package wireguard

import (
	"errors"
	"net"
	"net/netip"
	"sync"

	"github.com/tailscale/wireguard-go/conn"
	"github.com/tinkerbell/tinkerbell/pkg/listener"
)

// addrBind is a conn.Bind that listens on specific addresses, one socket per
// address, each serving only its address's family. wireguard-go's default bind
// always listens on every interface in both families.
type addrBind struct {
	addrs []netip.AddrPort

	mu    sync.Mutex
	conns []*net.UDPConn
}

var _ conn.Bind = (*addrBind)(nil)

// Open listens on the bind's addresses. The port argument is ignored: each
// address carries its own port.
func (b *addrBind) Open(_ uint16) ([]conn.ReceiveFunc, uint16, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.conns) > 0 {
		return nil, 0, conn.ErrBindAlreadyOpen
	}
	if len(b.addrs) == 0 {
		return nil, 0, errors.New("wireguard: no address to listen on")
	}
	fns := make([]conn.ReceiveFunc, 0, len(b.addrs))
	for _, ap := range b.addrs {
		c, err := listener.UDP(ap)
		if err != nil {
			return nil, 0, errors.Join(err, b.closeLocked())
		}
		b.conns = append(b.conns, c)
		fns = append(fns, receive(c))
	}
	return fns, uint16(b.conns[0].LocalAddr().(*net.UDPAddr).Port), nil //nolint:forcetypeassert,gosec // a UDPConn's local address is a UDPAddr with a 16 bit port
}

func receive(c *net.UDPConn) conn.ReceiveFunc {
	return func(slab []byte, packets []conn.ReceivedPacket) (int, error) {
		n, ap, err := c.ReadFromUDPAddrPort(slab)
		if err != nil {
			return 0, err
		}
		packets[0] = conn.ReceivedPacket{Size: n, Endpoint: &addrEndpoint{dst: unmap(ap)}}
		return 1, nil
	}
}

func (b *addrBind) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closeLocked()
}

func (b *addrBind) closeLocked() error {
	var errs []error
	for _, c := range b.conns {
		errs = append(errs, c.Close())
	}
	b.conns = nil
	return errors.Join(errs...)
}

func (b *addrBind) SetMark(uint32) error { return nil }

// Send sends from the socket of ep's family.
func (b *addrBind) Send(bufs [][]byte, ep conn.Endpoint, offset int) error {
	e, ok := ep.(*addrEndpoint)
	if !ok {
		return conn.ErrWrongEndpointType
	}
	b.mu.Lock()
	var c *net.UDPConn
	for i, a := range b.addrs {
		if i < len(b.conns) && a.Addr().Is4() == e.dst.Addr().Is4() {
			c = b.conns[i]
			break
		}
	}
	b.mu.Unlock()
	if c == nil {
		return net.ErrClosed
	}
	for _, buf := range bufs {
		if _, err := c.WriteToUDPAddrPort(buf[offset:], e.dst); err != nil {
			return err
		}
	}
	return nil
}

func (b *addrBind) ParseEndpoint(s string) (conn.Endpoint, error) {
	ap, err := netip.ParseAddrPort(s)
	if err != nil {
		return nil, err
	}
	return &addrEndpoint{dst: unmap(ap)}, nil
}

func (b *addrBind) BatchSize() int { return 1 }

// addrEndpoint is a peer's UDP address.
type addrEndpoint struct {
	dst netip.AddrPort
}

func (e *addrEndpoint) ClearSrc()           {}
func (e *addrEndpoint) SrcToString() string { return "" }
func (e *addrEndpoint) DstToString() string { return e.dst.String() }
func (e *addrEndpoint) DstToBytes() []byte  { b, _ := e.dst.MarshalBinary(); return b }
func (e *addrEndpoint) DstIP() netip.Addr   { return e.dst.Addr() }
func (e *addrEndpoint) SrcIP() netip.Addr   { return netip.Addr{} }

func unmap(ap netip.AddrPort) netip.AddrPort {
	return netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port())
}
