package wireguard

import (
	"net/netip"
	"sync"
	"time"

	"github.com/tailscale/wireguard-go/conn"
)

// maxPendingSources bounds the initiation sources remembered for peers that
// were never looked up, such as initiations from peers that already exist.
const maxPendingSources = 4096

// sourceBind wraps a conn.Bind to learn the UDP source of each handshake
// initiation. The fork doesn't roam endpoints, and a lazily created peer only
// gets the endpoint its lookup returns, so the lookup needs the initiation's
// source to be able to reply at all.
type sourceBind struct {
	conn.Bind

	onInitiation func(Key, netip.AddrPort)

	mu      sync.Mutex
	pending map[Key]pendingSource
}

type pendingSource struct {
	ep   conn.Endpoint
	seen time.Time
}

func newSourceBind(b conn.Bind) *sourceBind {
	return &sourceBind{Bind: b, pending: map[Key]pendingSource{}}
}

func (b *sourceBind) Open(port uint16) ([]conn.ReceiveFunc, uint16, error) {
	fns, actual, err := b.Bind.Open(port)
	if err != nil {
		return nil, 0, err
	}
	wrapped := make([]conn.ReceiveFunc, len(fns))
	for i, fn := range fns {
		wrapped[i] = func(slab []byte, packets []conn.ReceivedPacket) (int, error) {
			n, err := fn(slab, packets)
			for j := range n {
				packets[j].Endpoint = &sourceEndpoint{Endpoint: packets[j].Endpoint, bind: b}
			}
			return n, err
		}
	}
	return wrapped, actual, nil
}

func (b *sourceBind) Send(bufs [][]byte, ep conn.Endpoint, offset int) error {
	if s, ok := ep.(*sourceEndpoint); ok {
		ep = s.Endpoint
	}
	return b.Bind.Send(bufs, ep, offset)
}

func (b *sourceBind) record(pub Key, ep conn.Endpoint) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	if len(b.pending) >= maxPendingSources {
		for k, v := range b.pending {
			if now.Sub(v.seen) > 10*time.Second {
				delete(b.pending, k)
			}
		}
		if len(b.pending) >= maxPendingSources {
			return
		}
	}
	b.pending[pub] = pendingSource{ep: ep, seen: now}
}

func (b *sourceBind) take(pub Key) (conn.Endpoint, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	p, ok := b.pending[pub]
	delete(b.pending, pub)
	return p.ep, ok
}

// sourceEndpoint is a received packet's endpoint that reports the initiator's
// key back to its bind.
type sourceEndpoint struct {
	conn.Endpoint
	bind *sourceBind
}

// InitiationMessagePublicKey implements conn.InitiationAwareEndpoint. The
// device calls it after decrypting an initiation's static key and right before
// looking the peer up.
func (e *sourceEndpoint) InitiationMessagePublicKey(pub [32]byte) {
	e.bind.record(Key(pub), e.Endpoint)
	if e.bind.onInitiation != nil {
		e.bind.onInitiation(Key(pub), endpointAddrPort(e.Endpoint))
	}
}

func endpointAddrPort(ep conn.Endpoint) netip.AddrPort {
	ap, err := netip.ParseAddrPort(ep.DstToString())
	if err != nil {
		return netip.AddrPort{}
	}
	return netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port())
}
