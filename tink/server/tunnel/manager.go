// Package tunnel authenticates Tink Agents with WireGuard. It runs two tiers of
// userspace WireGuard devices: a bootstrap tier that admits any key just in
// time and reaches only Register, and a session tier that admits only session
// keys granted for one Agent in one Workflow. See
// docs/technical/designs/AGENT_WIREGUARD_AUTHN.md.
package tunnel

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	"github.com/tinkerbell/tinkerbell/pkg/data"
	"github.com/tinkerbell/tinkerbell/pkg/wireguard"
	"k8s.io/apimachinery/pkg/types"
)

// Mode is how WireGuard authentication is enforced.
type Mode string

const (
	// ModeDisabled serves only the plain listener, without authentication, as before.
	ModeDisabled Mode = "disabled"
	// ModeOptional serves the tunnels and the plain listener. The plain listener
	// refuses Agents that have a pinned credential or a binding.
	ModeOptional Mode = "optional"
	// ModeRequired serves gRPC only in the tunnels.
	ModeRequired Mode = "required"
)

// AuthenticationPolicy is which credential trust can get an Agent a session.
type AuthenticationPolicy string

const (
	// AuthenticationPolicyFirstUse accepts every credential: an Agent without a
	// pinned credential is bound to the first key that registers while a
	// Workflow has work for it.
	AuthenticationPolicyFirstUse AuthenticationPolicy = "first-use"
	// AuthenticationPolicyPinned accepts only pinned credentials, which an
	// operator sets.
	AuthenticationPolicyPinned AuthenticationPolicy = "pinned"
)

// ParseAuthenticationPolicy parses s as an AuthenticationPolicy.
func ParseAuthenticationPolicy(s string) (AuthenticationPolicy, error) {
	switch p := AuthenticationPolicy(s); p {
	case AuthenticationPolicyFirstUse, AuthenticationPolicyPinned:
		return p, nil
	case "":
		return AuthenticationPolicyFirstUse, nil
	default:
		return "", fmt.Errorf("unknown Agent authentication policy %q, must be one of [%s, %s]", s, AuthenticationPolicyFirstUse, AuthenticationPolicyPinned)
	}
}

// ParseMode parses s as a Mode.
func ParseMode(s string) (Mode, error) {
	switch m := Mode(s); m {
	case ModeDisabled, ModeOptional, ModeRequired:
		return m, nil
	case "":
		return ModeDisabled, nil
	default:
		return "", fmt.Errorf("unknown WireGuard mode %q, must be one of [%s, %s, %s]", s, ModeDisabled, ModeOptional, ModeRequired)
	}
}

const (
	// GRPCPort is the Tink Server's port inside both tunnels.
	GRPCPort = 42113

	// bootstrapMaxLifetime is how long a bootstrap peer may exist from admission,
	// whatever it does. It covers handshake retries and one bounded Register.
	bootstrapMaxLifetime = 60 * time.Second
	// bootstrapRegisterTimeout removes peers that never call Register. A real Agent calls it right after its handshake.
	bootstrapRegisterTimeout = 10 * time.Second
	// bootstrapRejectGrace lets a NotFound or PermissionDenied response leave the tunnel before the peer is removed.
	bootstrapRejectGrace = 2 * time.Second
	// sessionRevokeGrace keeps a revoked session's peer just long enough for its
	// Agent to get an Unauthenticated response instead of timing out, so it
	// returns to Register promptly. The interceptor refuses the key throughout.
	sessionRevokeGrace = 15 * time.Second
	// maxBootstrapPeers matches wireguard-go's MaxPeers.
	maxBootstrapPeers = 1 << 16

	janitorInterval = time.Second
)

// Backend is what the Manager reads and writes in the API.
type Backend interface {
	ReadWorkflow(ctx context.Context, name, namespace string) (*tinkerbell.Workflow, error)
	UpdateWorkflow(ctx context.Context, wf *tinkerbell.Workflow, opts data.UpdateOptions) error
}

// Family is how the devices are served in one address family.
type Family struct {
	Enabled bool
	// BindAddr is the address the devices listen on.
	BindAddr netip.Addr
	// PublicAddr is the address Agents reach the devices at. It is what boot
	// parameters and Register responses advertise.
	PublicAddr netip.Addr
	// BootstrapPort and SessionPort are the devices' UDP ports.
	BootstrapPort uint16
	SessionPort   uint16
	// PreviousBootstrapPort is the previous bootstrap key's port, used when
	// ServerKeys.PreviousBootstrap is set.
	PreviousBootstrapPort uint16
}

func (f Family) port(tier Tier, previous bool) uint16 {
	switch {
	case tier == TierSession:
		return f.SessionPort
	case previous:
		return f.PreviousBootstrapPort
	default:
		return f.BootstrapPort
	}
}

// Config configures a Manager.
type Config struct {
	Mode                 Mode
	AuthenticationPolicy AuthenticationPolicy
	// V4 and V6 configure each address family, like the other services'
	// per-family listeners.
	V4 Family
	V6 Family
	// SourceCheck allows binding a key only from an address in its Hardware's spec.
	SourceCheck bool
	Backend     Backend
	Log         logr.Logger
}

// Manager runs the tunnel devices and holds the in-memory state that admission
// callbacks read: bootstrap peer lifetimes and the session index.
type Manager struct {
	cfg  Config
	keys *ServerKeys
	log  logr.Logger
	now  func() time.Time

	bootstrap *device
	session   *device
	// previous is the previous bootstrap key's device, or nil.
	previous  *device
	work      chan func(context.Context)
	closeOnce sync.Once

	mu sync.Mutex
	// bpeers are admitted bootstrap peers by device and tunnel address.
	bpeers map[peerRef]*bootstrapPeer
	// sessions are session entries by public key; saddr maps a session address to its key.
	sessions map[wireguard.Key]*sessionEntry
	saddr    map[netip.Addr]wireguard.Key
	// wfKeys are the session keys indexed for each Workflow.
	wfKeys map[types.NamespacedName][]wireguard.Key
}

type device struct {
	tier     Tier
	previous bool
	dev      *wireguard.Device
	grpc     net.Listener
}

// peerRef identifies a bootstrap peer. The same Agent key can be a peer of
// both bootstrap devices, at the same address in each.
type peerRef struct {
	previous bool
	addr     netip.Addr
}

type bootstrapPeer struct {
	key        wireguard.Key
	source     netip.AddrPort
	deadline   time.Time
	registerBy time.Time
	registered bool
	removeAt   time.Time
}

type sessionEntry struct {
	Session
	bootstrapKey string
	addr         netip.Addr
	expiresAt    time.Time
	revokedAt    time.Time
	// reason says why the session was revoked, for the Agent's logs.
	reason string
}

// New brings up the bootstrap and session devices.
func New(cfg Config, keys *ServerKeys) (*Manager, error) {
	if cfg.Mode == ModeDisabled {
		return nil, errors.New("tunnel: WireGuard mode is disabled")
	}
	// The plain listener of optional mode would let Agents without a pinned
	// credential in.
	if cfg.AuthenticationPolicy == AuthenticationPolicyPinned && cfg.Mode != ModeRequired {
		return nil, fmt.Errorf("tunnel: Agent authentication policy %s requires mode %s", AuthenticationPolicyPinned, ModeRequired)
	}
	if !cfg.V4.Enabled && !cfg.V6.Enabled {
		return nil, errors.New("tunnel: no address family is served")
	}
	for _, f := range []struct {
		name string
		fam  Family
		is4  bool
	}{{"IPv4", cfg.V4, true}, {"IPv6", cfg.V6, false}} {
		if !f.fam.Enabled {
			continue
		}
		if !f.fam.BindAddr.IsValid() || f.fam.BindAddr.Unmap().Is4() != f.is4 {
			return nil, fmt.Errorf("tunnel: %s is served but has no %s bind address", f.name, f.name)
		}
		if !f.fam.PublicAddr.IsValid() || f.fam.PublicAddr.Unmap().Is4() != f.is4 {
			return nil, fmt.Errorf("tunnel: %s is served but has no %s public address to advertise", f.name, f.name)
		}
	}
	m := &Manager{
		cfg:      cfg,
		keys:     keys,
		log:      cfg.Log,
		now:      time.Now,
		work:     make(chan func(context.Context), 256),
		bpeers:   map[peerRef]*bootstrapPeer{},
		sessions: map[wireguard.Key]*sessionEntry{},
		saddr:    map[netip.Addr]wireguard.Key{},
		wfKeys:   map[types.NamespacedName][]wireguard.Key{},
	}
	var err error
	if m.bootstrap, err = m.newDevice(TierBootstrap, keys.Bootstrap, false); err != nil {
		return nil, err
	}
	if m.session, err = m.newDevice(TierSession, keys.Session, false); err != nil {
		m.Close()
		return nil, err
	}
	if !keys.PreviousBootstrap.IsZero() {
		if m.previous, err = m.newDevice(TierBootstrap, keys.PreviousBootstrap, true); err != nil {
			m.Close()
			return nil, err
		}
	}
	return m, nil
}

func (m *Manager) newDevice(tier Tier, priv wireguard.Key, previous bool) (*device, error) {
	var binds []netip.AddrPort
	for _, f := range []Family{m.cfg.V4, m.cfg.V6} {
		if f.Enabled {
			binds = append(binds, netip.AddrPortFrom(f.BindAddr, f.port(tier, previous)))
		}
	}
	p64 := wireguard.BootstrapPrefix(m.keys.Prefix)
	lookup := func(pub wireguard.Key, src netip.AddrPort) (wireguard.PeerConfig, bool) {
		return m.admitBootstrap(previous, pub, src)
	}
	onInit := func(pub wireguard.Key, src netip.AddrPort) { m.bootstrapInitiation(previous, pub, src) }
	if tier == TierSession {
		p64 = wireguard.SessionPrefix(m.keys.Prefix)
		lookup = func(pub wireguard.Key, _ netip.AddrPort) (wireguard.PeerConfig, bool) {
			return m.admitSession(pub)
		}
		onInit = nil
	}
	dev, err := wireguard.New(wireguard.Config{
		PrivateKey:   priv,
		BindAddrs:    binds,
		Addresses:    []netip.Addr{wireguard.ServerAddr(p64)},
		Lookup:       lookup,
		OnInitiation: onInit,
		Log:          m.log.WithValues("tier", tier, "previous", previous),
	})
	if err != nil {
		return nil, fmt.Errorf("tunnel: %s: %w", tier, err)
	}
	ln, err := dev.ListenTCP(GRPCPort)
	if err != nil {
		dev.Close()
		return nil, fmt.Errorf("tunnel: %s: listen gRPC: %w", tier, err)
	}
	m.log.V(1).Info("WireGuard device up", "tier", tier, "previous", previous, "listen", binds, "publicKey", priv.PublicKey().String(), "address", wireguard.ServerAddr(p64))
	return &device{tier: tier, previous: previous, dev: dev, grpc: &listener{Listener: ln, tier: tier, previous: previous}}, nil
}

// Mode returns the configured mode.
func (m *Manager) Mode() Mode { return m.cfg.Mode }

// PinnedOnly reports whether only pinned credentials can get sessions.
func (m *Manager) PinnedOnly() bool {
	return m.cfg.AuthenticationPolicy == AuthenticationPolicyPinned
}

// SourceCheck reports whether bindings require the Hardware's address.
func (m *Manager) SourceCheck() bool { return m.cfg.SourceCheck }

// Listeners returns the gRPC listeners of every device.
func (m *Manager) Listeners() []net.Listener {
	ls := []net.Listener{m.bootstrap.grpc, m.session.grpc}
	if m.previous != nil {
		ls = append(ls, m.previous.grpc)
	}
	return ls
}

// BootstrapServer describes a bootstrap device to an Agent.
type BootstrapServer struct {
	PublicKey wireguard.Key
	Endpoint  netip.AddrPort
	Prefix    netip.Prefix
}

// Bootstrap returns the bootstrap server Smee advertises to clients of one
// address family, and whether that family is served.
func (m *Manager) Bootstrap(v6 bool) (BootstrapServer, bool) {
	f := m.cfg.V4
	if v6 {
		f = m.cfg.V6
	}
	if !f.Enabled {
		return BootstrapServer{}, false
	}
	return BootstrapServer{
		PublicKey: m.keys.Bootstrap.PublicKey(),
		Endpoint:  netip.AddrPortFrom(f.PublicAddr, f.BootstrapPort),
		Prefix:    m.keys.Prefix,
	}, true
}

// endpoint returns where an Agent reaching the server from src reaches tier's
// device: in src's family when it is served, otherwise in the other one.
func (m *Manager) endpoint(src netip.AddrPort, tier Tier) netip.AddrPort {
	f := m.cfg.V4
	if (src.Addr().Unmap().Is6() && m.cfg.V6.Enabled) || !m.cfg.V4.Enabled {
		f = m.cfg.V6
	}
	return netip.AddrPortFrom(f.PublicAddr, f.port(tier, false))
}

// MoveTo returns the current bootstrap server for a caller that reached the
// previous bootstrap key's device. That device only redirects.
func (m *Manager) MoveTo(c *Caller) (BootstrapServer, bool) {
	if !c.Previous {
		return BootstrapServer{}, false
	}
	return BootstrapServer{
		PublicKey: m.keys.Bootstrap.PublicKey(),
		Endpoint:  m.endpoint(c.Source, TierBootstrap),
		Prefix:    m.keys.Prefix,
	}, true
}

// Run removes expired peers until ctx is done. It leaves the devices up: the
// gRPC servers inside them must stop first, so their owner calls Close.
func (m *Manager) Run(ctx context.Context) error {
	t := time.NewTicker(janitorInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			m.sweep()
		case fn := <-m.work:
			fn(ctx)
		}
	}
}

// Close closes every device. It is safe to call more than once.
func (m *Manager) Close() {
	m.closeOnce.Do(func() {
		for _, d := range []*device{m.bootstrap, m.session, m.previous} {
			if d != nil {
				_ = d.grpc.Close()
				d.dev.Close()
			}
		}
	})
}

func (m *Manager) enqueue(fn func(context.Context)) {
	select {
	case m.work <- fn:
	default:
		m.log.V(1).Info("dropping tunnel work item, queue is full")
	}
}

// admitBootstrap admits every key: anyone can trigger it with the public server
// key, and keys are free. What bounds the cost is each peer's fixed lifetime.
// Even with only pinned credentials allowed, a TPM Agent's key is new every boot,
// so the keys to expect can't be known here; Register authenticates them.
func (m *Manager) admitBootstrap(previous bool, pub wireguard.Key, src netip.AddrPort) (wireguard.PeerConfig, bool) {
	addr := wireguard.PeerAddr(wireguard.BootstrapPrefix(m.keys.Prefix), pub)
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.bpeers) >= maxBootstrapPeers {
		return wireguard.PeerConfig{}, false
	}
	m.bpeers[peerRef{previous: previous, addr: addr}] = &bootstrapPeer{
		key:        pub,
		source:     src,
		deadline:   now.Add(bootstrapMaxLifetime),
		registerBy: now.Add(bootstrapRegisterTimeout),
	}
	m.log.V(1).Info("bootstrap peer admitted", "previous", previous, "key", pub.String(), "source", src, "address", addr)
	return wireguard.PeerConfig{AllowedIPs: []netip.Prefix{netip.PrefixFrom(addr, 128)}}, true
}

// bootstrapInitiation drops a bootstrap peer that a handshake initiation
// claims from a new UDP source. The fork pins a peer to the source it was
// admitted from and doesn't roam, so after an Agent restart or a NAT rebinding
// the peer would keep answering the old source. Once removed, the Agent's next
// handshake retry is admitted at its new source. The initiation isn't verified
// yet, so a replayed one can at worst cost the real Agent one retry, which
// bootstrap peers' fixed lifetimes already allow for.
func (m *Manager) bootstrapInitiation(previous bool, pub wireguard.Key, src netip.AddrPort) {
	addr := wireguard.PeerAddr(wireguard.BootstrapPrefix(m.keys.Prefix), pub)
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.bpeers[peerRef{previous: previous, addr: addr}]; ok && p.source != src && p.removeAt.IsZero() {
		p.removeAt = m.now()
		m.log.V(1).Info("bootstrap peer moved source, removing it so it can be admitted again", "key", pub.String(), "oldSource", p.source, "source", src)
	}
}

func (m *Manager) admitSession(pub wireguard.Key) (wireguard.PeerConfig, bool) {
	now := m.now()
	m.mu.Lock()
	e, ok := m.sessions[pub]
	m.mu.Unlock()
	if !ok || !e.revokedAt.IsZero() || !now.Before(e.expiresAt) {
		m.log.V(1).Info("session peer refused", "key", pub.String())
		return wireguard.PeerConfig{}, false
	}
	m.log.V(1).Info("session peer admitted", "key", pub.String(), "workflow", e.Workflow, "agentID", e.AgentID)
	return wireguard.PeerConfig{
		AllowedIPs:   []netip.Prefix{netip.PrefixFrom(e.addr, 128)},
		PresharedKey: m.psk(e.UID, pub),
	}, true
}

// psk derives a session's pre-shared key, so it is unique to one session and
// every replica can compute it without it being stored.
func (m *Manager) psk(uid types.UID, pub wireguard.Key) wireguard.Key {
	h := hmac.New(sha256.New, m.keys.PSKRoot[:])
	h.Write([]byte(uid))
	h.Write(pub[:])
	return wireguard.Key(h.Sum(nil))
}

// bootstrapCaller returns the identity of a bootstrap tunnel connection.
func (m *Manager) bootstrapCaller(a *Addr) (*Caller, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.bpeers[peerRef{previous: a.Previous, addr: a.Addr()}]
	if !ok || !p.removeAt.IsZero() {
		return nil, false
	}
	return &Caller{Tier: TierBootstrap, Previous: a.Previous, Key: p.key, Source: p.source}, true
}

// sessionCaller returns the identity of a session tunnel connection, if its session is active.
func (m *Manager) sessionCaller(a *Addr) (*Caller, bool) {
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	k, ok := m.saddr[a.Addr()]
	if !ok {
		return nil, false
	}
	e := m.sessions[k]
	if e == nil || !e.revokedAt.IsZero() || !now.Before(e.expiresAt) {
		return nil, false
	}
	s := e.Session
	src, _ := m.session.dev.PeerEndpoint(k)
	return &Caller{Tier: TierSession, Key: k, Source: src, Session: &s}, true
}

// Registered records that a bootstrap caller called Register, so it isn't
// removed early for never doing so. It doesn't extend the peer's lifetime.
func (m *Manager) Registered(c *Caller) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.bpeers[m.bootstrapRef(c)]; ok {
		p.registered = true
	}
}

// Rejected schedules removal of a bootstrap caller whose registration was
// refused, after a grace period for the response to leave the tunnel.
func (m *Manager) Rejected(c *Caller) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.bpeers[m.bootstrapRef(c)]; ok && p.removeAt.IsZero() {
		p.removeAt = minTime(m.now().Add(bootstrapRejectGrace), p.deadline)
	}
}

func (m *Manager) bootstrapRef(c *Caller) peerRef {
	return peerRef{previous: c.Previous, addr: wireguard.PeerAddr(wireguard.BootstrapPrefix(m.keys.Prefix), c.Key)}
}

func (m *Manager) sweep() {
	now := m.now()
	type removal struct {
		d   *device
		key wireguard.Key
	}
	var rm []removal
	m.mu.Lock()
	for ref, p := range m.bpeers {
		reason := ""
		switch {
		case !now.Before(p.deadline):
			reason = "maximum lifetime"
		case !p.registered && !now.Before(p.registerBy):
			reason = "no Register"
		case !p.removeAt.IsZero() && !now.Before(p.removeAt):
			reason = "rejected registration"
		}
		if reason == "" {
			continue
		}
		delete(m.bpeers, ref)
		d := m.bootstrap
		if ref.previous {
			d = m.previous
		}
		rm = append(rm, removal{d: d, key: p.key})
		m.log.V(1).Info("bootstrap peer removed", "previous", ref.previous, "key", p.key.String(), "reason", reason)
	}
	for k, e := range m.sessions {
		if e.revokedAt.IsZero() && !now.Before(e.expiresAt) {
			e.revokedAt = now
			e.reason = fmt.Sprintf("Workflow %s passed its execution deadline", e.Workflow)
			m.log.V(1).Info("session expired", "workflow", e.Workflow, "agentID", e.AgentID, "key", k.String())
		}
		if !e.revokedAt.IsZero() && !now.Before(e.revokedAt.Add(sessionRevokeGrace)) {
			delete(m.sessions, k)
			delete(m.saddr, e.addr)
			rm = append(rm, removal{d: m.session, key: k})
			m.log.V(1).Info("session peer removed", "workflow", e.Workflow, "agentID", e.AgentID, "key", k.String())
		}
	}
	m.mu.Unlock()
	for _, r := range rm {
		if r.d != nil {
			r.d.dev.RemovePeer(r.key)
		}
	}
}

// Grant is a newly issued session.
type Grant struct {
	PrivateKey      wireguard.Key
	PublicKey       wireguard.Key
	PresharedKey    wireguard.Key
	Address         netip.Addr
	ServerPublicKey wireguard.Key
	ServerEndpoint  netip.AddrPort
	ServerAddress   netip.Addr
}

// NewSession generates a session key pair for one Agent in the Workflow with
// UID uid, reached from src. Nothing is recorded until the caller writes the
// session to the Workflow and calls IndexWorkflow.
func (m *Manager) NewSession(uid types.UID, src netip.AddrPort) (*Grant, error) {
	priv, err := wireguard.GeneratePrivateKey()
	if err != nil {
		return nil, err
	}
	pub := priv.PublicKey()
	p64 := wireguard.SessionPrefix(m.keys.Prefix)
	return &Grant{
		PrivateKey:      priv,
		PublicKey:       pub,
		PresharedKey:    m.psk(uid, pub),
		Address:         wireguard.PeerAddr(p64, pub),
		ServerPublicKey: m.keys.Session.PublicKey(),
		ServerEndpoint:  m.endpoint(src, TierSession),
		ServerAddress:   wireguard.ServerAddr(p64),
	}, nil
}

// IndexWorkflow brings the session index in line with wf: sessions that are
// active become admissible, and keys that are no longer active are revoked.
func (m *Manager) IndexWorkflow(wf *tinkerbell.Workflow) {
	nn := types.NamespacedName{Namespace: wf.Namespace, Name: wf.Name}
	now := m.now()
	want := map[wireguard.Key]*sessionEntry{}
	for _, s := range wf.Status.Agents {
		if s.Session.PublicKey == "" || !SessionActive(wf, &s, now) {
			continue
		}
		pub, err := wireguard.ParseKey(s.Session.PublicKey)
		if err != nil {
			continue
		}
		addr, err := netip.ParseAddr(s.Session.Address)
		if err != nil {
			continue
		}
		want[pub] = &sessionEntry{
			Session:      Session{Workflow: nn, UID: wf.UID, AgentID: s.AgentID},
			bootstrapKey: s.Credential.WireGuard.PublicKey,
			addr:         addr,
			expiresAt:    s.Session.ExpiresAt.Time,
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range m.wfKeys[nn] {
		if _, ok := want[k]; !ok {
			m.revokeLocked(k, now, m.revokeReason(wf, k, now))
		}
	}
	keys := make([]wireguard.Key, 0, len(want))
	for k, e := range want {
		keys = append(keys, k)
		if cur, ok := m.sessions[k]; ok {
			if cur.revokedAt.IsZero() {
				cur.expiresAt = e.expiresAt
			}
			continue
		}
		m.sessions[k] = e
		m.saddr[e.addr] = k
		m.log.V(1).Info("session indexed", "workflow", nn, "agentID", e.AgentID, "key", k.String(), "address", e.addr, "expiresAt", e.expiresAt)
	}
	if len(keys) == 0 {
		delete(m.wfKeys, nn)
	} else {
		m.wfKeys[nn] = keys
	}
}

// ForgetWorkflow revokes every session of a deleted Workflow.
func (m *Manager) ForgetWorkflow(nn types.NamespacedName) {
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range m.wfKeys[nn] {
		m.revokeLocked(k, now, fmt.Sprintf("Workflow %s was deleted", nn))
	}
	delete(m.wfKeys, nn)
}

func (m *Manager) revokeLocked(k wireguard.Key, now time.Time, reason string) {
	if e, ok := m.sessions[k]; ok && e.revokedAt.IsZero() {
		e.revokedAt = now
		e.reason = reason
		m.log.V(1).Info("session revoked", "workflow", e.Workflow, "agentID", e.AgentID, "key", k.String(), "reason", reason)
	}
}

// revokeReason explains why wf no longer has an active session for key k.
// Called with m.mu held.
func (m *Manager) revokeReason(wf *tinkerbell.Workflow, k wireguard.Key, now time.Time) string {
	e, ok := m.sessions[k]
	if !ok {
		return ""
	}
	rec := wf.Status.Agent(e.AgentID)
	switch {
	case rec == nil:
		return fmt.Sprintf("Agent %s's session record was removed from Workflow %s", e.AgentID, e.Workflow)
	case rec.Credential.WireGuard.PublicKey != e.bootstrapKey:
		return fmt.Sprintf("Agent %s was rebound to another key in Workflow %s", e.AgentID, e.Workflow)
	case rec.Session.PublicKey != k.String():
		return fmt.Sprintf("a newer session replaced this one in Workflow %s", e.Workflow)
	}
	if r := SessionEndReason(wf, rec, now); r != "" {
		return r
	}
	return fmt.Sprintf("session for Workflow %s ended", e.Workflow)
}

// endedReason returns why the session behind a tunnel address ended, while
// its revoked peer is still kept to deliver that answer.
func (m *Manager) endedReason(a *Addr) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.sessions[m.saddr[a.Addr()]]
	if e == nil {
		return ""
	}
	if e.reason == "" && !m.now().Before(e.expiresAt) {
		return fmt.Sprintf("Workflow %s passed its execution deadline", e.Workflow)
	}
	return e.reason
}

// SessionEndReason explains why the session s in wf is no longer active at now,
// in terms an operator reading the Agent's logs recognizes. It returns "" for
// an active session.
func SessionEndReason(wf *tinkerbell.Workflow, s *tinkerbell.WorkflowAgentStatus, now time.Time) string {
	name := wf.Namespace + "/" + wf.Name
	switch {
	case wf.DeletionTimestamp != nil:
		return fmt.Sprintf("Workflow %s is being deleted", name)
	case wf.Status.State != tinkerbell.WorkflowStatePending && wf.Status.State != tinkerbell.WorkflowStateRunning:
		return fmt.Sprintf("Workflow %s is %s", name, wf.Status.State)
	case s.Session.ExpiresAt == nil || !now.Before(s.Session.ExpiresAt.Time):
		return fmt.Sprintf("Workflow %s passed its execution deadline", name)
	case !AgentHasWork(wf, s.AgentID):
		return fmt.Sprintf("Agent %s has no Tasks left in Workflow %s", s.AgentID, name)
	case s.Session.RevokedAt != nil:
		return fmt.Sprintf("session for Workflow %s ended", name)
	}
	return ""
}

// SessionActive reports whether s may be used at now: not ended or expired,
// its Workflow still running, and its Agent with a Task left to finish.
func SessionActive(wf *tinkerbell.Workflow, s *tinkerbell.WorkflowAgentStatus, now time.Time) bool {
	if s.Session.RevokedAt != nil || s.Session.ExpiresAt == nil || !now.Before(s.Session.ExpiresAt.Time) {
		return false
	}
	if wf.DeletionTimestamp != nil {
		return false
	}
	if wf.Status.State != tinkerbell.WorkflowStatePending && wf.Status.State != tinkerbell.WorkflowStateRunning {
		return false
	}
	return AgentHasWork(wf, s.AgentID)
}

// AgentHasWork reports whether agentID has a Task in wf that hasn't finished.
func AgentHasWork(wf *tinkerbell.Workflow, agentID string) bool {
	for _, t := range wf.Status.Tasks {
		if t.AgentID != agentID {
			continue
		}
		if len(t.Actions) == 0 {
			continue
		}
		if t.Actions[len(t.Actions)-1].State != tinkerbell.WorkflowStateSuccess {
			return true
		}
	}
	return false
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
