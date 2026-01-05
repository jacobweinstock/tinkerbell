package wireguard

import (
	"fmt"
	"net/netip"

	"github.com/tailscale/wireguard-go/device"
)

const (
	DefaultListenPort int = 51820
)

// Type aliasing here strengthens the abstraction by not leaking the github.com/tailscale/wireguard-go/device package to consumers of this package.
type PeerLookupFunc func(NoisePublicKey) (allowedIPs []netip.Prefix)
type NoisePublicKey = device.NoisePublicKey

// A Device is a WireGuard device.
//
// Because the zero value of some Go types may be significant to WireGuard for
// Device fields, pointer types are used for some of these fields. Only
// pointer fields which are not nil will be applied when configuring a device.
type Device struct {
	// Name is the name of the device.
	Name string

	// Address of the Wireguard device.
	Address netip.Addr

	// Type specifies the underlying implementation of the device.
	Type DeviceType

	// PrivateKey is the device's private key.
	PrivateKey *Key

	// PublicKey is the device's public key, computed from its PrivateKey.
	PublicKey Key

	// ListenPort is the device's network listening port.
	ListenPort *int

	// FirewallMark is the device's current firewall mark.
	//
	// The firewall mark can be used in conjunction with firewall software to
	// take action on outgoing WireGuard packets.
	FirewallMark *int

	// Peers is the list of network peers associated with this device.
	Peers []Peer

	// ReplacePeers specifies if the Peers in this configuration should replace
	// the existing peer list, instead of appending them to the existing list.
	ReplacePeers bool

	// PeerLookupFunc is the function that does lookups of Peers. This allows the device to be
	// configured when a Peer initiates a connection. This allows a Device to start up without any
	// Peers and removes Peers after an idle connection.
	PeerLookupFunc PeerLookupFunc
}

// A DeviceType specifies the underlying implementation of a WireGuard device.
type DeviceType int

// Possible DeviceType values.
const (
	Unknown DeviceType = iota
	LinuxKernel
	OpenBSDKernel
	FreeBSDKernel
	WindowsKernel
	Userspace
)

// String returns the string representation of a DeviceType.
func (dt DeviceType) String() string {
	switch dt {
	case LinuxKernel:
		return "Linux kernel"
	case OpenBSDKernel:
		return "OpenBSD kernel"
	case FreeBSDKernel:
		return "FreeBSD kernel"
	case WindowsKernel:
		return "Windows kernel"
	case Userspace:
		return "userspace"
	default:
		return "unknown"
	}
}

type DeviceOption func(*Device)

func WithDeviceListenPort(p int) DeviceOption {
	return func(d *Device) {
		d.ListenPort = toPtr(p)
	}
}

func WithDeviceAddress(a netip.Addr) DeviceOption {
	return func(d *Device) {
		d.Address = a
	}
}

func WithDevicePeerLookupFunc(fn PeerLookupFunc) DeviceOption {
	return func(d *Device) {
		d.PeerLookupFunc = fn
	}
}

func WithDevicePeers(p []Peer) DeviceOption {
	return func(d *Device) {
		d.Peers = p
	}
}

func NewDevice(privateKey Key, opts ...DeviceOption) *Device {
	dc := &Device{
		PrivateKey: &privateKey,
		PublicKey:  privateKey.PublicKey(),
		ListenPort: toPtr(DefaultListenPort),
		Type:       Userspace,
	}

	for _, opt := range opts {
		opt(dc)
	}

	return dc
}

func (d *Device) AddPeers(peers ...Peer) {
	if d != nil {
		d.Peers = append(d.Peers, peers...)
	}
}

// The keys for the Wireguard configuration protocol are found here:
// https://github.com/tailscale/wireguard-go/blob/tailscale/device/uapi.go:handleDeviceLine
func (d *Device) Text() []string {
	var lines []string
	if d == nil {
		return lines
	}

	// Handle Device fields
	if d.PrivateKey != nil {
		lines = append(lines, fmt.Sprintf("private_key=%s", base64ToHex(d.PrivateKey.String())))
	}
	if d.ListenPort != nil {
		lines = append(lines, fmt.Sprintf("listen_port=%d", *d.ListenPort))
	}
	if d.FirewallMark != nil {
		lines = append(lines, fmt.Sprintf("fwmark=%d", *d.FirewallMark))
	}
	if d.ReplacePeers {
		lines = append(lines, fmt.Sprintf("replace_peers=%t", d.ReplacePeers))
	}

	for _, p := range d.Peers {
		lines = append(lines, p.Text()...)
	}

	return lines
}
