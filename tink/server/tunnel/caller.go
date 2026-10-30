package tunnel

import (
	"context"
	"net"
	"net/netip"

	"github.com/tinkerbell/tinkerbell/pkg/wireguard"
	"k8s.io/apimachinery/pkg/types"
)

// Tier is one of the two layers of WireGuard identity.
type Tier string

const (
	// TierBootstrap admits any key, just in time, and reaches only Register.
	TierBootstrap Tier = "bootstrap"
	// TierSession admits only keys with an active session, and reaches the WorkflowService.
	TierSession Tier = "session"
)

// Addr is the remote address of a connection accepted inside a tunnel. Because
// each peer is admitted with only its own /128, the address identifies the
// peer's key.
type Addr struct {
	Tier Tier
	// Previous is set for a connection through the previous bootstrap key's device.
	Previous bool
	netip.AddrPort
}

// Network implements net.Addr.
func (a *Addr) Network() string { return "wireguard-" + string(a.Tier) }

// String implements net.Addr.
func (a *Addr) String() string { return a.AddrPort.String() }

// listener wraps a tunnel listener so gRPC's peer address tells the interceptor
// which tier a request came through.
type listener struct {
	net.Listener
	tier     Tier
	previous bool
}

// Tunnel names the tunnel the listener is in: "bootstrap", "session", or
// "previous-bootstrap" during a bootstrap key rotation. Its address exists only
// inside that tunnel, not on the host.
func (l *listener) Tunnel() string {
	if l.previous {
		return "previous-bootstrap"
	}
	return string(l.tier)
}

func (l *listener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	ap, _ := netip.ParseAddrPort(c.RemoteAddr().String())
	return &conn{Conn: c, remote: &Addr{Tier: l.tier, Previous: l.previous, AddrPort: ap}}, nil
}

type conn struct {
	net.Conn
	remote *Addr
}

func (c *conn) RemoteAddr() net.Addr { return c.remote }

// Caller is the authenticated identity of a request that arrived through a tunnel.
type Caller struct {
	Tier Tier
	// Previous is set when the caller reached the previous bootstrap key's device.
	Previous bool
	// Key is the caller's bootstrap key on the bootstrap tier, or its session key on the session tier.
	Key wireguard.Key
	// Source is the UDP address the caller's WireGuard packets come from.
	Source netip.AddrPort
	// Session is set on the session tier.
	Session *Session
}

// Session is what a session key authorizes: one Agent in one Workflow.
type Session struct {
	Workflow types.NamespacedName
	UID      types.UID
	AgentID  string
}

type callerKey struct{}

// WithCaller returns ctx carrying c.
func WithCaller(ctx context.Context, c *Caller) context.Context {
	return context.WithValue(ctx, callerKey{}, c)
}

// CallerFromContext returns the tunnel caller of a request, or nil for a request
// on the plain listener.
func CallerFromContext(ctx context.Context) *Caller {
	c, _ := ctx.Value(callerKey{}).(*Caller)
	return c
}
