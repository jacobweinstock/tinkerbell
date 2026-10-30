// Package wireguard runs userspace WireGuard devices whose network stack lives
// entirely inside the process (gVisor netstack), so neither side needs root, a
// kernel module, or host network changes.
//
// It requires Tailscale's [wireguard-go] fork for lazily created peers
// ([Config.Lookup]) and the hybrid post-quantum handshake, which every Device
// enables for every peer. Both sides of a tunnel must use this package, because
// the hybrid handshake has no fallback.
//
// [wireguard-go]: https://github.com/tailscale/wireguard-go
package wireguard

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/tailscale/wireguard-go/conn"
	"github.com/tailscale/wireguard-go/device"
	"github.com/tailscale/wireguard-go/tun"
	"github.com/tailscale/wireguard-go/tun/netstack"
)

// DefaultMTU is the tunnel MTU for a 1500 byte underlay.
const DefaultMTU = 1420

// PeerConfig configures one peer of a Device.
type PeerConfig struct {
	PublicKey Key
	// AllowedIPs are the tunnel addresses the peer may send from and that are
	// routed to it.
	AllowedIPs []netip.Prefix
	// PresharedKey is the optional pre-shared key. The zero Key disables it.
	PresharedKey Key
	// Endpoint is the peer's UDP address. The zero value leaves it to be
	// learned from the peer's first handshake.
	Endpoint netip.AddrPort
	// PersistentKeepalive keeps NAT mappings open. Zero disables it.
	PersistentKeepalive time.Duration
}

// LookupFunc decides whether to admit an unknown peer that has sent a valid
// handshake initiation to the device's key from the UDP address src. It runs
// synchronously in handshake processing, so it must only read memory. The
// returned PeerConfig's PublicKey, Endpoint, and PersistentKeepalive are
// ignored: the peer is always pinned to src.
type LookupFunc func(pub Key, src netip.AddrPort) (PeerConfig, bool)

// Config configures a Device.
type Config struct {
	PrivateKey Key
	// ListenPort is the UDP port to listen on, on every interface in both
	// families. Zero picks a random port. Ignored when BindAddrs is set.
	ListenPort uint16
	// BindAddrs, if set, are the only UDP addresses the device listens on, one
	// socket per address, each serving only its address's family.
	BindAddrs []netip.AddrPort
	// Addresses are this device's tunnel addresses.
	Addresses []netip.Addr
	// MTU defaults to DefaultMTU.
	MTU int
	// Lookup, if set, admits unknown peers just in time.
	Lookup LookupFunc
	// Peers are configured before the device comes up.
	Peers []PeerConfig
	// OnInitiation, if set, is called with the claimed key and UDP source of
	// every handshake initiation, before the initiation is verified, so it
	// must not be trusted as proof of the key. It runs in handshake processing
	// and must not call back into the Device.
	OnInitiation func(pub Key, src netip.AddrPort)
	Log          logr.Logger
}

// Device is a running userspace WireGuard device and its network stack.
type Device struct {
	dev       *device.Device
	net       *netstack.Net
	tun       tun.Device
	addresses []netip.Addr
	publicKey Key
}

// New creates a Device and brings it up.
func New(cfg Config) (*Device, error) {
	if cfg.PrivateKey.IsZero() {
		return nil, errors.New("wireguard: private key is required")
	}
	if len(cfg.Addresses) == 0 {
		return nil, errors.New("wireguard: at least one tunnel address is required")
	}
	mtu := cfg.MTU
	if mtu == 0 {
		mtu = DefaultMTU
	}
	tdev, tnet, err := netstack.CreateNetTUN(cfg.Addresses, nil, mtu)
	if err != nil {
		return nil, fmt.Errorf("wireguard: create netstack: %w", err)
	}
	inner := conn.NewDefaultBind()
	if len(cfg.BindAddrs) > 0 {
		inner = &addrBind{addrs: cfg.BindAddrs}
	}
	bind := newSourceBind(inner)
	bind.onInitiation = cfg.OnInitiation
	dev := device.NewDevice(tdev, bind, logger(cfg.Log))
	if cfg.Lookup != nil {
		lookup := cfg.Lookup
		dev.SetPeerLookupFunc(func(pk device.NoisePublicKey) (*device.NewPeerConfig, bool) {
			ep, ok := bind.take(Key(pk))
			if !ok {
				return nil, false
			}
			pc, ok := lookup(Key(pk), endpointAddrPort(ep))
			if !ok {
				return nil, false
			}
			return &device.NewPeerConfig{
				AllowedIPs:   pc.AllowedIPs,
				PresharedKey: device.NoisePresharedKey(pc.PresharedKey),
				Hybrid:       true,
				Endpoint:     ep,
			}, true
		})
	}

	var b strings.Builder
	fmt.Fprintf(&b, "private_key=%s\n", hex.EncodeToString(cfg.PrivateKey[:]))
	fmt.Fprintf(&b, "listen_port=%d\n", cfg.ListenPort)
	for _, p := range cfg.Peers {
		writePeer(&b, p)
	}
	if err := dev.IpcSet(b.String()); err != nil {
		dev.Close()
		return nil, fmt.Errorf("wireguard: configure device: %w", err)
	}
	if err := dev.Up(); err != nil {
		dev.Close()
		return nil, fmt.Errorf("wireguard: bring device up: %w", err)
	}

	return &Device{
		dev:       dev,
		net:       tnet,
		tun:       tdev,
		addresses: cfg.Addresses,
		publicKey: cfg.PrivateKey.PublicKey(),
	}, nil
}

func writePeer(b *strings.Builder, p PeerConfig) {
	fmt.Fprintf(b, "public_key=%s\n", hex.EncodeToString(p.PublicKey[:]))
	b.WriteString("hybrid=true\n")
	if !p.PresharedKey.IsZero() {
		fmt.Fprintf(b, "preshared_key=%s\n", hex.EncodeToString(p.PresharedKey[:]))
	}
	if p.Endpoint.IsValid() {
		fmt.Fprintf(b, "endpoint=%s\n", p.Endpoint)
	}
	if p.PersistentKeepalive > 0 {
		fmt.Fprintf(b, "persistent_keepalive_interval=%d\n", int(p.PersistentKeepalive.Seconds()))
	}
	b.WriteString("replace_allowed_ips=true\n")
	for _, a := range p.AllowedIPs {
		fmt.Fprintf(b, "allowed_ip=%s\n", a)
	}
}

// PublicKey returns the device's public key.
func (d *Device) PublicKey() Key {
	return d.publicKey
}

// AddPeer adds p, or replaces the configuration of an existing peer with p's key.
func (d *Device) AddPeer(p PeerConfig) error {
	var b strings.Builder
	writePeer(&b, p)
	return d.dev.IpcSet(b.String())
}

// RemovePeer removes the peer with public key pub, if present.
func (d *Device) RemovePeer(pub Key) {
	d.dev.RemovePeer(device.NoisePublicKey(pub))
}

// HasPeer reports whether the device currently has a peer with public key pub.
func (d *Device) HasPeer(pub Key) bool {
	_, ok := d.dev.LookupActivePeer(device.NoisePublicKey(pub))
	return ok
}

// PeerEndpoint returns the UDP address the peer with public key pub was last
// seen at.
func (d *Device) PeerEndpoint(pub Key) (netip.AddrPort, bool) {
	cfg, err := d.dev.IpcGet()
	if err != nil {
		return netip.AddrPort{}, false
	}
	want := hex.EncodeToString(pub[:])
	var inPeer bool
	s := bufio.NewScanner(strings.NewReader(cfg))
	for s.Scan() {
		k, v, ok := strings.Cut(s.Text(), "=")
		if !ok {
			continue
		}
		switch k {
		case "public_key":
			inPeer = v == want
		case "endpoint":
			if inPeer {
				ap, err := netip.ParseAddrPort(v)
				if err != nil {
					return netip.AddrPort{}, false
				}
				return netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port()), true
			}
		}
	}
	return netip.AddrPort{}, false
}

// ListenPort returns the UDP port the device listens on.
func (d *Device) ListenPort() uint16 {
	cfg, err := d.dev.IpcGet()
	if err != nil {
		return 0
	}
	s := bufio.NewScanner(strings.NewReader(cfg))
	for s.Scan() {
		if v, ok := strings.CutPrefix(s.Text(), "listen_port="); ok {
			p, _ := strconv.ParseUint(v, 10, 16)
			return uint16(p)
		}
	}
	return 0
}

// ListenTCP listens on port at the device's first tunnel address.
func (d *Device) ListenTCP(port uint16) (net.Listener, error) {
	return d.net.ListenTCPAddrPort(netip.AddrPortFrom(d.addresses[0], port))
}

// ListenUDP listens on port at the device's first tunnel address.
func (d *Device) ListenUDP(port uint16) (net.PacketConn, error) {
	return d.net.ListenUDPAddrPort(netip.AddrPortFrom(d.addresses[0], port))
}

// DialContext connects to address through the tunnel. network is "tcp" or "udp".
func (d *Device) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return d.net.DialContext(ctx, network, address)
}

// Close tears the device and its network stack down.
func (d *Device) Close() {
	d.dev.Close()
}

func logger(l logr.Logger) *device.Logger {
	l = l.WithName("wireguard")
	return &device.Logger{
		Verbosef: func(format string, args ...any) {
			l.V(5).Info(fmt.Sprintf(format, args...))
		},
		Errorf: func(format string, args ...any) {
			l.V(2).Info(fmt.Sprintf(format, args...))
		},
	}
}
