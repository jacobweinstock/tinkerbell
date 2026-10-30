package wireguard

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"net/netip"
)

// GeneratePrefix returns a random IPv6 unique local /48 (RFC 4193).
func GeneratePrefix() (netip.Prefix, error) {
	var a [16]byte
	a[0] = 0xfd
	if _, err := rand.Read(a[1:6]); err != nil {
		return netip.Prefix{}, fmt.Errorf("generate prefix: %w", err)
	}
	return netip.PrefixFrom(netip.AddrFrom16(a), 48), nil
}

// ParsePrefix parses an IPv6 /48, the tunnel prefix shared by both tiers.
func ParsePrefix(s string) (netip.Prefix, error) {
	p, err := netip.ParsePrefix(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	if !p.Addr().Is6() || p.Addr().Is4In6() || p.Bits() != 48 {
		return netip.Prefix{}, fmt.Errorf("prefix %s is not an IPv6 /48", s)
	}
	return p.Masked(), nil
}

// BootstrapPrefix returns the /64 of the bootstrap tunnel: the first /64 in p.
func BootstrapPrefix(p netip.Prefix) netip.Prefix {
	return subnet64(p, 0)
}

// SessionPrefix returns the /64 of the session tunnel: the second /64 in p.
func SessionPrefix(p netip.Prefix) netip.Prefix {
	return subnet64(p, 1)
}

func subnet64(p netip.Prefix, n byte) netip.Prefix {
	a := p.Masked().Addr().As16()
	a[7] = n
	return netip.PrefixFrom(netip.AddrFrom16(a), 64)
}

// ServerAddr returns the server's address in a tunnel /64: ::1.
func ServerAddr(p64 netip.Prefix) netip.Addr {
	a := p64.Masked().Addr().As16()
	a[15] = 1
	return netip.AddrFrom16(a)
}

// PeerAddr returns the address of the peer with public key pub in a tunnel
// /64: the prefix followed by the first 64 bits of SHA-256(pub). Both sides
// compute it, so there is no address allocation state.
func PeerAddr(p64 netip.Prefix, pub Key) netip.Addr {
	a := p64.Masked().Addr().As16()
	h := sha256.Sum256(pub[:])
	copy(a[8:], h[:8])
	return netip.AddrFrom16(a)
}
