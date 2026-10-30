package tunnel

import (
	"bytes"
	"context"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	"github.com/tinkerbell/tinkerbell/pkg/data"
	"github.com/tinkerbell/tinkerbell/pkg/proto"
	"github.com/tinkerbell/tinkerbell/pkg/wireguard"
	"google.golang.org/grpc"
	"google.golang.org/grpc/peer"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestLoadKeys(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, k wireguard.Key) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(k.String()+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	boot, _ := wireguard.GeneratePrivateKey()
	sess, _ := wireguard.GeneratePrivateKey()
	root, _ := wireguard.GenerateKey()
	f := KeyFiles{
		Prefix:        "fd12:3456:789a::/48",
		BootstrapFile: write("bootstrap", boot),
		SessionFile:   write("session", sess),
		PSKRootFile:   write("psk-root", root),
	}
	k, err := LoadKeys(f)
	if err != nil {
		t.Fatal(err)
	}
	if !k.Bootstrap.Equal(boot) || !k.Session.Equal(sess) || !k.PSKRoot.Equal(root) || k.Prefix.String() != f.Prefix {
		t.Errorf("LoadKeys = %+v", k)
	}

	same := f
	same.SessionFile = f.BootstrapFile
	if _, err := LoadKeys(same); err == nil {
		t.Error("identical bootstrap and session keys accepted")
	}
	missing := f
	missing.PSKRootFile = filepath.Join(dir, "nope")
	if _, err := LoadKeys(missing); err == nil {
		t.Error("missing PSK root file accepted")
	}
	bad := f
	bad.Prefix = "fd12:3456:789a::/64"
	if _, err := LoadKeys(bad); err == nil {
		t.Error("non-/48 prefix accepted")
	}

	prev, _ := wireguard.GeneratePrivateKey()
	withPrev := f
	withPrev.PreviousBootstrapFile = write("previous", prev)
	if k, err := LoadKeys(withPrev); err != nil || !k.PreviousBootstrap.Equal(prev) {
		t.Errorf("previous bootstrap key: %v, %v", k, err)
	}
	withPrev.PreviousBootstrapFile = f.BootstrapFile
	if _, err := LoadKeys(withPrev); err == nil {
		t.Error("previous bootstrap key equal to the current one accepted")
	}
}

func testManager(t *testing.T, now *time.Time) *Manager {
	t.Helper()
	p, _ := wireguard.GeneratePrefix()
	root, _ := wireguard.GenerateKey()
	return &Manager{
		keys:     &ServerKeys{Prefix: p, PSKRoot: root},
		log:      logr.Discard(),
		now:      func() time.Time { return *now },
		bpeers:   map[peerRef]*bootstrapPeer{},
		sessions: map[wireguard.Key]*sessionEntry{},
		saddr:    map[netip.Addr]wireguard.Key{},
		wfKeys:   map[types.NamespacedName][]wireguard.Key{},
	}
}

func runningWorkflow(sessionKey wireguard.Key, expires time.Time) *tinkerbell.Workflow {
	return &tinkerbell.Workflow{
		ObjectMeta: metav1.ObjectMeta{Name: "wf", Namespace: "default", UID: "uid"},
		Status: tinkerbell.WorkflowStatus{
			State: tinkerbell.WorkflowStateRunning,
			Tasks: []tinkerbell.Task{{AgentID: "agent", Actions: []tinkerbell.Action{{ID: "a", State: tinkerbell.WorkflowStateRunning}}}},
			Agents: []tinkerbell.WorkflowAgentStatus{{
				AgentID: "agent",
				Session: tinkerbell.AgentSession{
					PublicKey: sessionKey.String(),
					Address:   "fd00:0:0:1::2",
					ExpiresAt: &metav1.Time{Time: expires},
				},
			}},
		},
	}
}

func TestSessionIndex(t *testing.T) {
	now := time.Now()
	m := testManager(t, &now)
	priv, _ := wireguard.GeneratePrivateKey()
	pub := priv.PublicKey()
	wf := runningWorkflow(pub, now.Add(time.Hour))

	m.IndexWorkflow(wf)
	pc, ok := m.admitSession(pub)
	if !ok {
		t.Fatal("active session not admitted")
	}
	if pc.PresharedKey != m.psk("uid", pub) {
		t.Error("admission used the wrong pre-shared key")
	}
	other, _ := wireguard.GeneratePrivateKey()
	if _, ok := m.admitSession(other.PublicKey()); ok {
		t.Error("unknown key admitted")
	}

	// The Agent's last Task finishing revokes the session at once.
	wf.Status.Tasks[0].Actions[0].State = tinkerbell.WorkflowStateSuccess
	wf.Status.State = tinkerbell.WorkflowStatePost
	m.IndexWorkflow(wf)
	if _, ok := m.admitSession(pub); ok {
		t.Error("revoked session admitted")
	}
	if _, ok := m.sessions[pub]; !ok {
		t.Fatal("revoked session dropped before its grace period")
	}
	now = now.Add(sessionRevokeGrace)
	m.sweep()
	if _, ok := m.sessions[pub]; ok {
		t.Error("revoked session kept after its grace period")
	}
}

func TestSessionExpiry(t *testing.T) {
	now := time.Now()
	m := testManager(t, &now)
	priv, _ := wireguard.GeneratePrivateKey()
	m.IndexWorkflow(runningWorkflow(priv.PublicKey(), now.Add(time.Minute)))
	now = now.Add(time.Minute)
	if _, ok := m.admitSession(priv.PublicKey()); ok {
		t.Error("session admitted at its expiry")
	}
}

func TestBootstrapPeerLifetimes(t *testing.T) {
	now := time.Now()
	m := testManager(t, &now)
	k1, _ := wireguard.GeneratePrivateKey()
	k2, _ := wireguard.GeneratePrivateKey()
	src := netip.MustParseAddrPort("192.0.2.10:41641")
	ref := func(k wireguard.Key) peerRef {
		return peerRef{addr: wireguard.PeerAddr(wireguard.BootstrapPrefix(m.keys.Prefix), k.PublicKey())}
	}

	m.admitBootstrap(false, k1.PublicKey(), src)
	m.admitBootstrap(false, k2.PublicKey(), src)
	m.Registered(&Caller{Key: k1.PublicKey()})

	now = now.Add(bootstrapRegisterTimeout)
	m.sweep()
	if _, ok := m.bpeers[ref(k2)]; ok {
		t.Error("peer that never registered was kept")
	}
	if _, ok := m.bpeers[ref(k1)]; !ok {
		t.Fatal("registered peer removed early")
	}

	m.Rejected(&Caller{Key: k1.PublicKey()})
	if _, ok := m.bootstrapCaller(&Addr{AddrPort: netip.AddrPortFrom(ref(k1).addr, 1)}); ok {
		t.Error("rejected peer still identified as a caller")
	}
	now = now.Add(bootstrapRejectGrace)
	m.sweep()
	if _, ok := m.bpeers[ref(k1)]; ok {
		t.Error("rejected peer kept past its grace period")
	}

	// Registering can't extend the fixed lifetime.
	m.admitBootstrap(false, k1.PublicKey(), src)
	for range 10 {
		m.Registered(&Caller{Key: k1.PublicKey()})
		now = now.Add(bootstrapMaxLifetime / 10)
	}
	m.sweep()
	if _, ok := m.bpeers[ref(k1)]; ok {
		t.Error("peer kept past its maximum lifetime")
	}
}

func TestBootstrapPeerMovedSource(t *testing.T) {
	now := time.Now()
	m := testManager(t, &now)
	k, _ := wireguard.GeneratePrivateKey()
	m.admitBootstrap(false, k.PublicKey(), netip.MustParseAddrPort("192.0.2.10:1000"))
	m.bootstrapInitiation(false, k.PublicKey(), netip.MustParseAddrPort("192.0.2.10:1000"))
	m.sweep()
	if len(m.bpeers) != 1 {
		t.Fatal("peer removed for an initiation from its own source")
	}
	m.bootstrapInitiation(false, k.PublicKey(), netip.MustParseAddrPort("192.0.2.10:2000"))
	m.sweep()
	if len(m.bpeers) != 0 {
		t.Error("peer kept after its Agent moved source")
	}
}

func TestSessionEndedReason(t *testing.T) {
	now := time.Now()
	m := testManager(t, &now)
	priv, _ := wireguard.GeneratePrivateKey()
	pub := priv.PublicKey()
	wf := runningWorkflow(pub, now.Add(time.Hour))
	wf.Status.Agents[0].Credential.WireGuard.PublicKey = "boot"
	addr := &Addr{AddrPort: netip.MustParseAddrPort("[fd00:0:0:1::2]:1")}

	m.IndexWorkflow(wf)
	if r := m.endedReason(addr); r != "" {
		t.Errorf("active session has an end reason %q", r)
	}
	wf.Status.State = tinkerbell.WorkflowStateTimeout
	m.IndexWorkflow(wf)
	if got, want := m.endedReason(addr), "Workflow default/wf is TIMEOUT"; got != want {
		t.Errorf("reason = %q, want %q", got, want)
	}

	priv2, _ := wireguard.GeneratePrivateKey()
	wf = runningWorkflow(priv2.PublicKey(), now.Add(time.Hour))
	wf.Name = "wf2"
	wf.Status.Agents[0].Credential.WireGuard.PublicKey = "boot"
	wf.Status.Agents[0].Session.Address = "fd00:0:0:1::3"
	m.IndexWorkflow(wf)
	wf.Status.Agents[0].Credential.WireGuard.PublicKey = "other"
	wf.Status.Agents[0].Session.PublicKey = ""
	m.IndexWorkflow(wf)
	addr2 := &Addr{AddrPort: netip.MustParseAddrPort("[fd00:0:0:1::3]:1")}
	if got, want := m.endedReason(addr2), "Agent agent was rebound to another key in Workflow default/wf2"; got != want {
		t.Errorf("reason = %q, want %q", got, want)
	}
}

func TestParseRebind(t *testing.T) {
	k, _ := wireguard.GeneratePrivateKey()
	key := k.PublicKey().String()
	for _, tt := range []struct {
		in      string
		agentID string
		key     string
		wantErr bool
	}{
		{in: "aa:bb:cc:dd:ee:01=" + key, agentID: "aa:bb:cc:dd:ee:01", key: key},
		{in: "aa:bb:cc:dd:ee:01=", agentID: "aa:bb:cc:dd:ee:01"},
		{in: "=" + key, wantErr: true},
		{in: key[:10], wantErr: true},
		{in: "agent=notakey", wantErr: true},
	} {
		agentID, gotKey, err := ParseRebind(tt.in)
		if (err != nil) != tt.wantErr || agentID != tt.agentID || gotKey != tt.key {
			t.Errorf("ParseRebind(%q) = %q, %q, %v", tt.in, agentID, gotKey, err)
		}
	}
}

// wfBackend is an in-memory Backend holding one Workflow.
type wfBackend struct{ wf *tinkerbell.Workflow }

func (b *wfBackend) ReadWorkflow(context.Context, string, string) (*tinkerbell.Workflow, error) {
	return b.wf.DeepCopy(), nil
}

func (b *wfBackend) UpdateWorkflow(_ context.Context, wf *tinkerbell.Workflow, _ data.UpdateOptions) error {
	b.wf = wf.DeepCopy()
	return nil
}

func TestRebind(t *testing.T) {
	now := time.Now()
	m := testManager(t, &now)
	sess, _ := wireguard.GeneratePrivateKey()
	newBoot, _ := wireguard.GeneratePrivateKey()
	wf := runningWorkflow(sess.PublicKey(), now.Add(time.Hour))
	wf.Status.Agents[0].Credential.WireGuard.PublicKey = "old"
	wf.Annotations = map[string]string{tinkerbell.AgentRebindAnnotation: "agent=" + newBoot.PublicKey().String()}
	b := &wfBackend{wf: wf}
	m.cfg.Backend = b
	m.IndexWorkflow(wf)

	m.rebind(context.Background(), types.NamespacedName{Namespace: "default", Name: "wf"})

	rec := b.wf.Status.Agent("agent")
	if rec.Credential.WireGuard.PublicKey != newBoot.PublicKey().String() || rec.Session.PublicKey != "" || rec.Session.RevokedAt == nil {
		t.Errorf("record after rebind = %+v", rec)
	}
	if _, ok := b.wf.Annotations[tinkerbell.AgentRebindAnnotation]; ok {
		t.Error("rebind annotation not removed")
	}
	if _, ok := m.admitSession(sess.PublicKey()); ok {
		t.Error("old session key still admitted after rebind")
	}
}

// TestPreviousBootstrapDevice checks that peers of the previous bootstrap key's
// device are tracked apart from the current device's, and that its callers are
// redirected to the current bootstrap server.
func TestPreviousBootstrapDevice(t *testing.T) {
	now := time.Now()
	m := testManager(t, &now)
	cur, _ := wireguard.GeneratePrivateKey()
	m.keys.Bootstrap = cur
	m.cfg.V4 = Family{Enabled: true, PublicAddr: netip.MustParseAddr("192.0.2.1"), BootstrapPort: 51822}
	agent, _ := wireguard.GeneratePrivateKey()
	src := netip.MustParseAddrPort("192.0.2.10:1000")
	addr := wireguard.PeerAddr(wireguard.BootstrapPrefix(m.keys.Prefix), agent.PublicKey())

	m.admitBootstrap(true, agent.PublicKey(), src)
	if _, ok := m.bootstrapCaller(&Addr{AddrPort: netip.AddrPortFrom(addr, 1)}); ok {
		t.Error("a peer of the previous device was identified on the current device")
	}
	c, ok := m.bootstrapCaller(&Addr{Previous: true, AddrPort: netip.AddrPortFrom(addr, 1)})
	if !ok || !c.Previous {
		t.Fatalf("previous device caller = %+v, %v", c, ok)
	}
	mv, ok := m.MoveTo(c)
	if !ok || !mv.PublicKey.Equal(cur.PublicKey()) || mv.Endpoint.String() != "192.0.2.1:51822" {
		t.Errorf("MoveTo = %+v, %v", mv, ok)
	}
	if _, ok := m.MoveTo(&Caller{}); ok {
		t.Error("a caller on the current device was redirected")
	}
}

func TestFamilies(t *testing.T) {
	now := time.Now()
	m := testManager(t, &now)
	m.cfg.V4 = Family{Enabled: true, PublicAddr: netip.MustParseAddr("192.0.2.1"), BootstrapPort: 51820, SessionPort: 51821}

	if _, ok := m.Bootstrap(true); ok {
		t.Error("IPv6 bootstrap advertised while IPv6 isn't served")
	}
	if bs, ok := m.Bootstrap(false); !ok || bs.Endpoint.String() != "192.0.2.1:51820" {
		t.Errorf("IPv4 bootstrap = %+v, %v", bs, ok)
	}
	// An Agent that somehow reached the server over IPv6 is still sent to IPv4,
	// the only family served.
	if ep := m.endpoint(netip.MustParseAddrPort("[2001:db8::10]:1000"), TierSession); ep.String() != "192.0.2.1:51821" {
		t.Errorf("session endpoint = %s", ep)
	}

	m.cfg.V6 = Family{Enabled: true, PublicAddr: netip.MustParseAddr("2001:db8::1"), BootstrapPort: 51830, SessionPort: 51831}
	if ep := m.endpoint(netip.MustParseAddrPort("[2001:db8::10]:1000"), TierSession); ep.String() != "[2001:db8::1]:51831" {
		t.Errorf("IPv6 Agent's session endpoint = %s", ep)
	}
	if ep := m.endpoint(netip.MustParseAddrPort("[::ffff:192.0.2.10]:1000"), TierSession); ep.String() != "192.0.2.1:51821" {
		t.Errorf("IPv4-mapped Agent's session endpoint = %s", ep)
	}
}

func TestNewRequiresFamilyAddresses(t *testing.T) {
	keys := &ServerKeys{}
	for name, cfg := range map[string]Config{
		"no family":         {Mode: ModeOptional},
		"no bind address":   {Mode: ModeOptional, V4: Family{Enabled: true, PublicAddr: netip.MustParseAddr("192.0.2.1")}},
		"wrong family bind": {Mode: ModeOptional, V4: Family{Enabled: true, BindAddr: netip.MustParseAddr("::1"), PublicAddr: netip.MustParseAddr("192.0.2.1")}},
		"no public address": {Mode: ModeOptional, V6: Family{Enabled: true, BindAddr: netip.MustParseAddr("::1")}},
	} {
		if _, err := New(cfg, keys); err == nil {
			t.Errorf("%s: New succeeded", name)
		}
	}
}

func TestListenerTunnel(t *testing.T) {
	for _, tt := range []struct {
		l    *listener
		want string
	}{
		{&listener{tier: TierBootstrap}, "bootstrap"},
		{&listener{tier: TierSession}, "session"},
		{&listener{tier: TierBootstrap, previous: true}, "previous-bootstrap"},
	} {
		if got := tt.l.Tunnel(); got != tt.want {
			t.Errorf("Tunnel() = %q, want %q", got, tt.want)
		}
	}
}

// TestRebindIgnoredForPinnedMethods checks that the rebind annotation, which is
// for first use, only moves first-use bindings.
func TestRebindIgnoredForPinnedMethods(t *testing.T) {
	for name, tc := range map[string]struct {
		policy AuthenticationPolicy
		trust  tinkerbell.CredentialTrust
	}{
		"pinned policy":     {policy: AuthenticationPolicyPinned, trust: tinkerbell.CredentialTrustFirstUse},
		"pinned credential": {trust: tinkerbell.CredentialTrustPinned},
	} {
		t.Run(name, func(t *testing.T) {
			now := time.Now()
			m := testManager(t, &now)
			m.cfg.AuthenticationPolicy = tc.policy
			sess, _ := wireguard.GeneratePrivateKey()
			newBoot, _ := wireguard.GeneratePrivateKey()
			wf := runningWorkflow(sess.PublicKey(), now.Add(time.Hour))
			wf.Status.Agents[0].Credential.WireGuard.PublicKey = "old"
			wf.Status.Agents[0].Credential.Trust = tc.trust
			wf.Annotations = map[string]string{tinkerbell.AgentRebindAnnotation: "agent=" + newBoot.PublicKey().String()}
			b := &wfBackend{wf: wf}
			m.cfg.Backend = b
			m.IndexWorkflow(wf)

			m.rebind(context.Background(), types.NamespacedName{Namespace: "default", Name: "wf"})

			if rec := b.wf.Status.Agent("agent"); rec.Credential.WireGuard.PublicKey != "old" || rec.Session.PublicKey == "" {
				t.Errorf("record changed: %+v", rec)
			}
			if _, ok := b.wf.Annotations[tinkerbell.AgentRebindAnnotation]; ok {
				t.Error("rebind annotation not removed")
			}
		})
	}
}

func TestNewPinnedPolicyRequiresRequiredMode(t *testing.T) {
	v4 := Family{Enabled: true, BindAddr: netip.MustParseAddr("127.0.0.1"), PublicAddr: netip.MustParseAddr("192.0.2.1")}
	if _, err := New(Config{Mode: ModeOptional, AuthenticationPolicy: AuthenticationPolicyPinned, V4: v4}, &ServerKeys{}); err == nil {
		t.Error("New accepted the pinned policy in optional mode")
	}
}

func TestParseAuthenticationPolicy(t *testing.T) {
	for in, want := range map[string]AuthenticationPolicy{"": AuthenticationPolicyFirstUse, "first-use": AuthenticationPolicyFirstUse, "pinned": AuthenticationPolicyPinned} {
		if got, err := ParseAuthenticationPolicy(in); err != nil || got != want {
			t.Errorf("ParseAuthenticationPolicy(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := ParseAuthenticationPolicy("persistent"); err == nil {
		t.Error("ParseAuthenticationPolicy(persistent) succeeded")
	}
}

func TestTicket(t *testing.T) {
	now := time.Now()
	m := testManager(t, &now)
	want := Ticket{AgentID: "agent", BootstrapKey: "key", EKHash: "hash", AKName: []byte{1, 2}, Secret: []byte{3, 4}, Expires: now.Add(time.Minute).UTC()}
	b, err := m.SealTicket(want)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, want.Secret) || bytes.Contains(b, []byte(want.AgentID)) {
		t.Error("ticket isn't encrypted")
	}
	got, err := m.OpenTicket(b)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("OpenTicket (-want +got):\n%s", diff)
	}

	b[len(b)-1] ^= 1
	if _, err := m.OpenTicket(b); err == nil {
		t.Error("a tampered ticket opened")
	}
	b[len(b)-1] ^= 1
	other := testManager(t, &now)
	if _, err := other.OpenTicket(b); err == nil {
		t.Error("a ticket opened under another PSK root")
	}
	if _, err := m.OpenTicket(b[:5]); err == nil {
		t.Error("a truncated ticket opened")
	}
}

// TestInterceptorCountsDenials checks that session requests the session
// doesn't authorize are counted as authorization denials.
func TestInterceptorCountsDenials(t *testing.T) {
	now := time.Now()
	m := testManager(t, &now)
	intercept := m.UnaryInterceptor(nil)
	ctx := peer.NewContext(context.Background(), &peer.Peer{
		Addr: &Addr{Tier: TierSession, AddrPort: netip.MustParseAddrPort("[fd00:0:0:1::2]:1")},
	})
	handler := func(context.Context, any) (any, error) { return nil, nil }
	for method, reason := range map[string]string{
		"/proto.WorkflowService/GetAction": DeniedNoSession,
		"/proto.AgentService/Register":     DeniedWrongService,
	} {
		before := testutil.ToFloat64(AuthorizationDenials.WithLabelValues(reason))
		if _, err := intercept(ctx, &proto.ActionRequest{}, &grpc.UnaryServerInfo{FullMethod: method}, handler); err == nil {
			t.Errorf("%s: allowed", method)
		}
		if testutil.ToFloat64(AuthorizationDenials.WithLabelValues(reason)) != before+1 {
			t.Errorf("%s: denial %s not counted", method, reason)
		}
	}
}
