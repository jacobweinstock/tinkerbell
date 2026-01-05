package wireguard

import (
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"
)

// A Peer is a WireGuard peer to a Device.
//
// Because the zero value of some Go types may be significant to WireGuard for
// Peer fields, pointer types are used for some of these fields. Only
// pointer fields which are not nil will be applied when configuring a peer.
type Peer struct {
	// PublicKey is the public key of a peer, computed from its private key.
	//
	// PublicKey is always present in a Peer.
	PublicKey Key

	// Remove specifies if the peer with this public key should be removed
	// from a device's peer list.
	Remove bool

	// UpdateOnly specifies that an operation will only occur on this peer
	// if the peer already exists as part of the interface.
	UpdateOnly bool

	// PresharedKey is an optional preshared key which may be used as an
	// additional layer of security for peer communications.
	//
	// A zero-value Key means no preshared key is configured.
	PresharedKey *Key

	// Endpoint is the most recent source address used for communication by
	// this Peer.
	Endpoint *net.UDPAddr

	// PersistentKeepaliveInterval specifies how often an "empty" packet is sent
	// to a peer to keep a connection alive.
	//
	// A value of 0 indicates that persistent keepalives are disabled.
	PersistentKeepaliveInterval time.Duration

	// LastHandshakeTime indicates the most recent time a handshake was performed
	// with this peer.
	//
	// A zero-value time.Time indicates that no handshake has taken place with
	// this peer.
	LastHandshakeTime time.Time

	// ReceiveBytes indicates the number of bytes received from this peer.
	ReceiveBytes int64

	// TransmitBytes indicates the number of bytes transmitted to this peer.
	TransmitBytes int64

	// ReplaceAllowedIPs specifies if the allowed IPs specified in this peer
	// configuration should replace any existing ones, instead of appending them
	// to the allowed IPs list.
	ReplaceAllowedIPs bool

	// AllowedIPs specifies which IPv4 and IPv6 addresses this peer is allowed
	// to communicate on.
	//
	// 0.0.0.0/0 indicates that all IPv4 addresses are allowed, and ::/0
	// indicates that all IPv6 addresses are allowed.
	AllowedIPs []netip.Prefix

	// ProtocolVersion specifies which version of the WireGuard protocol is used
	// for this Peer.
	//
	// A value of 0 indicates that the most recent protocol version will be used.
	ProtocolVersion int
}

type PeerOption func(*Peer)

func WithPeerEndpoint(e *net.UDPAddr) PeerOption {
	return func(p *Peer) {
		p.Endpoint = e
	}
}

func WithPeerAllowedIPs(i []netip.Prefix) PeerOption {
	return func(p *Peer) {
		p.AllowedIPs = i
	}
}

func NewPeer(publicKey Key, opts ...PeerOption) *Peer {
	p := &Peer{
		PublicKey:  publicKey,
		Endpoint:   &net.UDPAddr{},
		AllowedIPs: []netip.Prefix{},
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// The keys pertaining to a Peer for the configuration protocol are defined here:
// https://github.com/tailscale/wireguard-go/blob/tailscale/device/uapi.go:handlePeerLine
func (p *Peer) Text() []string {
	var lines []string
	if p == nil {
		return lines
	}

	// Handle Peer fields
	if len(p.PublicKey) > 0 {
		lines = append(lines, fmt.Sprintf("public_key=%s", hex.EncodeToString(p.PublicKey[:])))
	}

	if p.Remove {
		lines = append(lines, fmt.Sprintf("remove=%t", p.Remove))
	}

	if p.UpdateOnly {
		lines = append(lines, fmt.Sprintf("update_only=%t", p.UpdateOnly))
	}

	if p.PresharedKey != nil {
		lines = append(lines, fmt.Sprintf("preshared_key=%s", p.PresharedKey))
	}
	if p.Endpoint != nil && !p.Endpoint.AddrPort().Addr().IsUnspecified() && p.Endpoint.AddrPort().Addr().IsValid() {
		lines = append(lines, fmt.Sprintf("endpoint=%s", p.Endpoint))
	}

	lines = append(lines, fmt.Sprintf("persistent_keepalive_interval=%d", p.PersistentKeepaliveInterval))

	if p.ReplaceAllowedIPs {
		lines = append(lines, fmt.Sprintf("replace_allowed_ips=%t", p.ReplaceAllowedIPs))
	}

	var allowedIPs []string
	for _, i := range p.AllowedIPs {
		allowedIPs = append(allowedIPs, i.String())
	}
	if len(allowedIPs) > 0 {
		lines = append(lines, fmt.Sprintf("allowed_ip=%s", strings.Join(allowedIPs, ",")))
	}

	return lines
}
