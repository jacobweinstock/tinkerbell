package grpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-tpm/tpm2/transport"
	"github.com/tinkerbell/tinkerbell/pkg/data"
	"github.com/tinkerbell/tinkerbell/pkg/proto"
	"github.com/tinkerbell/tinkerbell/pkg/wireguard"
	"github.com/tinkerbell/tinkerbell/tink/agent/internal/spec"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

const (
	// tunnelGRPCPort is the Tink Server's port inside both tunnels.
	tunnelGRPCPort = 42113
	keepalive      = 25 * time.Second
	// sessionRequestTimeout bounds each call through the session tunnel. A
	// session dies silently when the server's session key or PSK root rotates:
	// the handshake goes to a key the server no longer has, and nothing answers.
	// WireGuard retries a handshake every 5 seconds, so this gives up after a
	// couple of retries and returns to Register, which re-issues the session.
	sessionRequestTimeout = 15 * time.Second
	// serverStateFile, next to the key file, holds the bootstrap server a
	// previous one redirected the Agent to, so an Agent restart doesn't go back
	// to a key that is being rotated out.
	serverStateFile = "wireguard-server.json"
)

// WireGuardServer is a bootstrap server an Agent registers with.
type WireGuardServer struct {
	PublicKey wireguard.Key
	Endpoint  netip.AddrPort
}

// WireGuard is a transport that authenticates the Agent with WireGuard. Idle,
// it polls Register through a fresh bootstrap tunnel. Once granted a session,
// it runs GetAction and ReportActionStatus through the session tunnel until
// requests on the session fail, then goes back to Register.
type WireGuard struct {
	Log        logr.Logger
	AgentID    string
	Attributes *data.AgentAttributes
	// KeyFile holds the bootstrap private key. It is generated if missing.
	KeyFile string
	// TPM sends evidence from the machine's TPM, when it has one, so an Agent
	// whose Hardware pins its TPM can authenticate with any key.
	TPM bool
	// Server is the bootstrap server from the boot parameters, replaced when
	// the server redirects the Agent to a new bootstrap key.
	Server WireGuardServer
	Prefix netip.Prefix

	key wireguard.Key

	mu      sync.Mutex
	session *session
	// sessionEnded is set when a session ends, so the next Register's answer
	// is logged once: it tells the operator why there is no more work.
	sessionEnded bool
	// boot is the bootstrap device. It is kept between polls so its UDP source
	// port stays the same: the server pins a peer to the source it first saw.
	boot *wireguard.Device

	openTPM func() (transport.TPMCloser, error)
	// attestor is nil when the Agent has no usable TPM.
	attestor *tpmAttestor
}

type session struct {
	workflow  string
	expiresAt time.Time
	dev       *wireguard.Device
	conn      *grpc.ClientConn
	rw        *Config
}

// Init loads or creates the bootstrap key and logs its public half, which is
// what an operator pins or rebinds to. With TPM set, it also logs the TPM's EK
// public key hash, which an operator pins instead.
func (w *WireGuard) Init() error {
	k, created, err := wireguard.LoadOrCreatePrivateKey(w.KeyFile)
	if err != nil {
		return err
	}
	w.key = k
	if w.TPM {
		w.initTPM()
	}
	if saved, ok := w.loadServer(); ok && saved != w.Server {
		w.Log.Info("using the bootstrap server this Agent was redirected to", "server", saved.Endpoint, "serverPublicKey", saved.PublicKey.String())
		w.Server = saved
	}
	srv := w.Server
	w.Log.Info("WireGuard bootstrap key loaded", "publicKey", k.PublicKey().String(), "keyFile", w.KeyFile, "generated", created,
		"server", srv.Endpoint, "serverPublicKey", srv.PublicKey.String(), "address", wireguard.PeerAddr(wireguard.BootstrapPrefix(w.Prefix), k.PublicKey()))
	return nil
}

// initTPM prepares TPM evidence. A machine without a usable TPM still
// authenticates by its WireGuard key, so a TPM problem doesn't stop the Agent.
func (w *WireGuard) initTPM() {
	open := w.openTPM
	if open == nil {
		open = openTPM
	}
	a, err := newTPMAttestor(open)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		w.Log.V(1).Info("no TPM, not sending TPM evidence", "device", tpmDevice)
	case err != nil:
		w.Log.Error(err, "TPM is present but unusable, not sending TPM evidence", "device", tpmDevice)
	default:
		w.attestor = a
		w.Log.Info("TPM found; to authenticate this Agent by it, set spec.agent.authentication.tpm.ekPublicKeyHash on its Hardware",
			"ekPublicKeyHash", a.ekHash)
	}
}

// Read gets the next Action through the session, registering for one first if needed.
func (w *WireGuard) Read(ctx context.Context) (spec.Action, error) {
	s, err := w.ensureSession(ctx)
	if err != nil {
		return spec.Action{}, err
	}
	a, err := s.rw.Read(ctx)
	if err != nil && sessionFailed(err) {
		w.endSession(s, err)
	}
	return a, err
}

// Write reports an Action's status through the session, registering for one first if needed.
func (w *WireGuard) Write(ctx context.Context, event spec.Event) error {
	s, err := w.ensureSession(ctx)
	if err != nil {
		var ce *classifiedError
		if (errors.As(err, &ce) && ce.NoAction()) || status.Code(err) == codes.PermissionDenied {
			// No Workflow has work for this Agent anymore, or its key was
			// rebound, so no session will ever accept this report.
			return &classifiedError{kind: kindPermanent, err: fmt.Errorf("report not sent: %s", reason(err))}
		}
		return err
	}
	err = s.rw.Write(ctx, event)
	if err != nil && sessionFailed(err) {
		w.endSession(s, err)
	}
	return err
}

func (w *WireGuard) ensureSession(ctx context.Context) (*session, error) {
	w.mu.Lock()
	s := w.session
	w.mu.Unlock()
	if s != nil {
		if time.Now().Before(s.expiresAt) {
			return s, nil
		}
		w.endSession(s, errors.New("session reached its expiry, the Workflow's execution deadline"))
	}
	return w.register(ctx)
}

// sessionFailed reports whether err means the session is gone: the server
// revoked or removed the key, or the tunnel broke. NotFound and the like just
// mean no Action is available now, and the session stays up.
func sessionFailed(err error) bool {
	switch status.Code(err) { //nolint:exhaustive // other codes are answers from a working session
	case codes.Unauthenticated, codes.PermissionDenied, codes.Unavailable, codes.DeadlineExceeded:
		return true
	}
	return false
}

func (w *WireGuard) endSession(s *session, cause error) {
	w.mu.Lock()
	if w.session != s {
		w.mu.Unlock()
		return
	}
	w.session = nil
	w.sessionEnded = true
	w.mu.Unlock()
	_ = s.conn.Close()
	s.dev.Close()
	w.Log.Info("WireGuard session ended, returning to Register", "workflow", s.workflow, "reason", reason(cause))
}

// reason returns the server's explanation in a gRPC error, or err's text.
func reason(err error) string {
	if st, ok := status.FromError(err); ok {
		return st.Message()
	}
	return err.Error()
}

// register calls Register through a fresh bootstrap tunnel. Each poll does its
// own handshake: the server keeps a bootstrap peer only for a short, fixed
// lifetime, so a long-lived bootstrap tunnel would go stale between polls.
func (w *WireGuard) register(ctx context.Context) (*session, error) {
	p64 := wireguard.BootstrapPrefix(w.Prefix)
	dev, err := w.bootstrapDevice()
	if err != nil {
		return nil, err
	}
	conn, err := tunnelConn(dev, wireguard.ServerAddr(p64))
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	rctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	client := proto.NewAgentServiceClient(conn)
	req := &proto.RegisterRequest{
		AgentId:         toPtr(w.AgentID),
		AgentAttributes: ToProto(w.Attributes),
	}
	if w.attestor != nil {
		req.Evidence = w.attestor.evidence
	}
	resp, err := client.Register(rctx, req)
	// A challenge is answered at once, in the same tunnel, which the answer
	// vouches for.
	if err == nil && resp.GetState() == proto.RegisterResponse_STATE_CHALLENGE {
		resp, err = w.answerChallenge(rctx, client, req, resp.GetChallenge())
	}
	if err != nil {
		switch status.Code(err) { //nolint:exhaustive // every other code is retried with backoff
		case codes.PermissionDenied:
			w.Log.Info("Register refused: this Agent isn't authenticated. If this is the right machine, "+
				"set its Hardware's spec.agent.authentication to this key or its TPM's EK public key hash, "+
				"or rebind it in the Workflow with the tinkerbell.org/agent-rebind annotation",
				"publicKey", w.key.PublicKey().String(), "ekPublicKeyHash", w.ekHash(), "reason", status.Convert(err).Message())
		case codes.NotFound:
			w.Log.V(1).Info("Register: unknown Agent", "reason", status.Convert(err).Message())
		}
		return nil, readError(err)
	}
	if mv := resp.GetMoveTo(); mv != nil {
		if w.moveTo(mv) {
			return nil, &classifiedError{kind: kindConflict, err: errors.New("bootstrap key rotated, moving to the current bootstrap server")}
		}
		// A redirect the Agent can't follow must not turn into a tight retry loop.
		return nil, &classifiedError{kind: kindNoAction, err: errors.New("ignored an unusable redirect from the bootstrap server")}
	}
	switch resp.GetState() {
	case proto.RegisterResponse_STATE_GRANTED:
		return w.startSession(resp.GetSession())
	case proto.RegisterResponse_STATE_CHALLENGE:
		// A second challenge means the answer came too late; try again next poll.
		return nil, &classifiedError{kind: kindNoAction, err: errors.New("TPM challenge expired before it was answered")}
	default:
		w.mu.Lock()
		after := w.sessionEnded
		w.sessionEnded = false
		w.mu.Unlock()
		why := resp.GetReason()
		if after && why != "" {
			w.Log.Info("no more work for this Agent", "reason", why)
		}
		if why == "" {
			why = "no Workflow has work for this Agent"
		}
		return nil, &classifiedError{kind: kindNoAction, err: errors.New(why)}
	}
}

// answerChallenge answers a TPM challenge from Register and returns the
// response to the answer.
func (w *WireGuard) answerChallenge(ctx context.Context, client proto.AgentServiceClient, req *proto.RegisterRequest, ch *proto.Challenge) (*proto.RegisterResponse, error) {
	if w.attestor == nil {
		return nil, &classifiedError{kind: kindNoAction, err: errors.New("the server sent a TPM challenge, but this Agent has no TPM")}
	}
	secret, err := w.attestor.answer(ch)
	if err != nil {
		w.Log.Error(err, "unable to answer TPM challenge")
		return nil, &classifiedError{kind: kindNoAction, err: fmt.Errorf("answering TPM challenge: %w", err)}
	}
	req.ChallengeResponse = &proto.ChallengeResponse{Payload: secret, Ticket: ch.GetTicket()}
	return client.Register(ctx, req)
}

// ekHash returns the TPM's EK public key hash, or "" without a TPM.
func (w *WireGuard) ekHash() string {
	if w.attestor == nil {
		return ""
	}
	return w.attestor.ekHash
}

// bootstrapDevice returns the bootstrap device with the server peer freshly
// configured, so the next request starts with a new handshake. The server
// keeps a bootstrap peer only for a short, fixed lifetime, so a session from
// an earlier poll may no longer exist on its side.
func (w *WireGuard) bootstrapDevice() (*wireguard.Device, error) {
	srv := w.Server
	p64 := wireguard.BootstrapPrefix(w.Prefix)
	if w.boot == nil {
		dev, err := wireguard.New(wireguard.Config{
			PrivateKey: w.key,
			Addresses:  []netip.Addr{wireguard.PeerAddr(p64, w.key.PublicKey())},
			Log:        w.Log,
		})
		if err != nil {
			return nil, fmt.Errorf("bring up bootstrap tunnel: %w", err)
		}
		w.boot = dev
	}
	w.boot.RemovePeer(srv.PublicKey)
	if err := w.boot.AddPeer(wireguard.PeerConfig{
		PublicKey:  srv.PublicKey,
		AllowedIPs: []netip.Prefix{p64},
		Endpoint:   srv.Endpoint,
	}); err != nil {
		return nil, fmt.Errorf("configure bootstrap peer: %w", err)
	}
	return w.boot, nil
}

func (w *WireGuard) startSession(ps *proto.Session) (*session, error) {
	priv, err := wireguard.KeyFromBytes(ps.GetPrivateKey())
	if err != nil {
		return nil, fmt.Errorf("session private key: %w", err)
	}
	psk, err := wireguard.KeyFromBytes(ps.GetPresharedKey())
	if err != nil {
		return nil, fmt.Errorf("session pre-shared key: %w", err)
	}
	spub, err := wireguard.KeyFromBytes(ps.GetServerPublicKey())
	if err != nil {
		return nil, fmt.Errorf("session server public key: %w", err)
	}
	addr, err := netip.ParseAddr(ps.GetAddress())
	if err != nil {
		return nil, fmt.Errorf("session address: %w", err)
	}
	saddr, err := netip.ParseAddr(ps.GetServerAddress())
	if err != nil {
		return nil, fmt.Errorf("session server address: %w", err)
	}
	ep, err := netip.ParseAddrPort(ps.GetServerEndpoint())
	if err != nil {
		return nil, fmt.Errorf("session server endpoint: %w", err)
	}
	dev, err := wireguard.New(wireguard.Config{
		PrivateKey: priv,
		Addresses:  []netip.Addr{addr},
		Peers: []wireguard.PeerConfig{{
			PublicKey:           spub,
			PresharedKey:        psk,
			AllowedIPs:          []netip.Prefix{netip.PrefixFrom(saddr, 128)},
			Endpoint:            ep,
			PersistentKeepalive: keepalive,
		}},
		Log: w.Log,
	})
	if err != nil {
		return nil, fmt.Errorf("bring up session tunnel: %w", err)
	}
	conn, err := tunnelConn(dev, saddr)
	if err != nil {
		dev.Close()
		return nil, err
	}
	s := &session{
		workflow:  ps.GetWorkflow(),
		expiresAt: ps.GetExpiresAt().AsTime(),
		dev:       dev,
		conn:      conn,
		rw: &Config{
			Log:              w.Log,
			TinkServerClient: proto.NewWorkflowServiceClient(conn),
			AgentID:          w.AgentID,
			Attributes:       w.Attributes,
			Timeout:          sessionRequestTimeout,
		},
	}
	w.mu.Lock()
	w.session = s
	w.mu.Unlock()
	w.Log.Info("WireGuard session granted", "workflow", s.workflow, "address", addr, "server", ep, "expiresAt", s.expiresAt)
	return s, nil
}

func tunnelConn(dev *wireguard.Device, server netip.Addr) (*grpc.ClientConn, error) {
	target := net.JoinHostPort(server.String(), strconv.Itoa(tunnelGRPCPort))
	conn, err := grpc.NewClient("passthrough:///"+target,
		// WireGuard authenticates and encrypts; there is no TLS inside the tunnel.
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return dev.DialContext(ctx, "tcp", addr)
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("dial Tink Server through the tunnel: %w", err)
	}
	return conn, nil
}

type savedServer struct {
	PublicKey string `json:"publicKey"`
	Endpoint  string `json:"endpoint"`
}

func (w *WireGuard) stateFile() string {
	return filepath.Join(filepath.Dir(w.KeyFile), serverStateFile)
}

func (w *WireGuard) loadServer() (WireGuardServer, bool) {
	b, err := os.ReadFile(w.stateFile())
	if err != nil {
		return WireGuardServer{}, false
	}
	var s savedServer
	if err := json.Unmarshal(b, &s); err != nil {
		return WireGuardServer{}, false
	}
	k, err := wireguard.ParseKey(s.PublicKey)
	if err != nil {
		return WireGuardServer{}, false
	}
	ep, err := netip.ParseAddrPort(s.Endpoint)
	if err != nil {
		return WireGuardServer{}, false
	}
	return WireGuardServer{PublicKey: k, Endpoint: ep}, true
}

// moveTo switches to the bootstrap server the previous one redirected the
// Agent to, and reports whether it did. The redirect arrived through the old
// tunnel, which is authenticated, so it is trusted. The Agent keeps its own key.
func (w *WireGuard) moveTo(mv *proto.BootstrapServer) bool {
	k, err := wireguard.KeyFromBytes(mv.GetPublicKey())
	if err != nil {
		w.Log.Info("ignoring a redirect with an invalid bootstrap key; is the Agent built from the same version as the server?", "error", err)
		return false
	}
	ep, err := netip.ParseAddrPort(mv.GetEndpoint())
	if err != nil {
		w.Log.Info("ignoring a redirect with an invalid bootstrap endpoint; is the Agent built from the same version as the server?", "error", err)
		return false
	}
	next := WireGuardServer{PublicKey: k, Endpoint: ep}
	if next == w.Server {
		return false
	}
	w.Server = next
	if w.boot != nil {
		w.boot.Close()
		w.boot = nil
	}
	if err := w.saveServer(next); err != nil {
		w.Log.Info("unable to save the new bootstrap server; an Agent restart will use the boot parameters", "error", err)
	}
	w.Log.Info("bootstrap key rotated, moved to the current bootstrap server", "server", ep, "serverPublicKey", k.String())
	return true
}

func (w *WireGuard) saveServer(s WireGuardServer) error {
	b, err := json.Marshal(savedServer{PublicKey: s.PublicKey.String(), Endpoint: s.Endpoint.String()})
	if err != nil {
		return err
	}
	tmp := w.stateFile() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, w.stateFile())
}

// Close ends any session and closes the bootstrap device.
func (w *WireGuard) Close() {
	w.mu.Lock()
	s := w.session
	w.mu.Unlock()
	if s != nil {
		w.endSession(s, errors.New("agent stopping"))
	}
	if w.boot != nil {
		w.boot.Close()
	}
}
