package tunnel

import (
	"errors"
	"fmt"
	"net/netip"
	"os"

	"github.com/tinkerbell/tinkerbell/pkg/wireguard"
)

// ServerKeys is everything every replica must share: the tunnel prefix, the
// PSK root, and the private key of each tier's device.
type ServerKeys struct {
	Prefix    netip.Prefix
	PSKRoot   wireguard.Key
	Bootstrap wireguard.Key
	Session   wireguard.Key
	// PreviousBootstrap, if set, is the bootstrap key being rotated out. Its
	// device only redirects Agents to the current one.
	PreviousBootstrap wireguard.Key
}

// KeyFiles names where the server keys come from. Each file holds one
// base64 encoded 32 byte key, as `wg genkey` or `openssl rand -base64 32` print.
type KeyFiles struct {
	Prefix        string
	BootstrapFile string
	SessionFile   string
	PSKRootFile   string
	// PreviousBootstrapFile is optional: the bootstrap key being rotated out.
	PreviousBootstrapFile string
}

// LoadKeys reads the server keys. Operators provide them, typically from a
// Secret mounted as files, so that every replica uses the same keys and the
// server needs no access to Secrets.
func LoadKeys(f KeyFiles) (*ServerKeys, error) {
	p, err := wireguard.ParsePrefix(f.Prefix)
	if err != nil {
		return nil, fmt.Errorf("WireGuard prefix: %w", err)
	}
	k := &ServerKeys{Prefix: p}
	for _, kf := range []struct {
		name string
		path string
		key  *wireguard.Key
	}{
		{"bootstrap key", f.BootstrapFile, &k.Bootstrap},
		{"session key", f.SessionFile, &k.Session},
		{"PSK root", f.PSKRootFile, &k.PSKRoot},
	} {
		b, err := os.ReadFile(kf.path)
		if err != nil {
			return nil, fmt.Errorf("WireGuard %s: %w", kf.name, err)
		}
		if *kf.key, err = wireguard.ParseKey(string(b)); err != nil {
			return nil, fmt.Errorf("WireGuard %s %s: %w", kf.name, kf.path, err)
		}
	}
	if k.Bootstrap.Equal(k.Session) {
		return nil, errors.New("WireGuard bootstrap and session keys must differ")
	}
	if f.PreviousBootstrapFile != "" {
		b, err := os.ReadFile(f.PreviousBootstrapFile)
		if err != nil {
			return nil, fmt.Errorf("WireGuard previous bootstrap key: %w", err)
		}
		if k.PreviousBootstrap, err = wireguard.ParseKey(string(b)); err != nil {
			return nil, fmt.Errorf("WireGuard previous bootstrap key %s: %w", f.PreviousBootstrapFile, err)
		}
		if k.PreviousBootstrap.Equal(k.Bootstrap) || k.PreviousBootstrap.Equal(k.Session) {
			return nil, errors.New("WireGuard previous bootstrap key must differ from the bootstrap and session keys")
		}
	}
	return k, nil
}
