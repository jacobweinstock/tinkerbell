package wireguard

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/curve25519"
)

// KeyLen is the length in bytes of a WireGuard key.
const KeyLen = 32

// Key is a WireGuard private, public, or pre-shared key.
type Key [KeyLen]byte

// GeneratePrivateKey returns a new random, clamped X25519 private key.
func GeneratePrivateKey() (Key, error) {
	var k Key
	if _, err := rand.Read(k[:]); err != nil {
		return Key{}, fmt.Errorf("generate private key: %w", err)
	}
	k[0] &= 248
	k[31] = (k[31] & 127) | 64
	return k, nil
}

// GenerateKey returns 32 random bytes, suitable for a pre-shared key or HMAC root.
func GenerateKey() (Key, error) {
	var k Key
	if _, err := rand.Read(k[:]); err != nil {
		return Key{}, fmt.Errorf("generate key: %w", err)
	}
	return k, nil
}

// ParseKey parses a base64 encoded key, the format the wg command uses.
func ParseKey(s string) (Key, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return Key{}, fmt.Errorf("decode key: %w", err)
	}
	if len(b) != KeyLen {
		return Key{}, fmt.Errorf("key is %d bytes, want %d", len(b), KeyLen)
	}
	return Key(b), nil
}

// KeyFromBytes returns the Key in b, which must be exactly KeyLen bytes.
func KeyFromBytes(b []byte) (Key, error) {
	if len(b) != KeyLen {
		return Key{}, fmt.Errorf("key is %d bytes, want %d", len(b), KeyLen)
	}
	return Key(b), nil
}

// String returns the base64 encoding of k.
func (k Key) String() string {
	return base64.StdEncoding.EncodeToString(k[:])
}

// PublicKey returns the public key for the private key k.
func (k Key) PublicKey() Key {
	var pub [KeyLen]byte
	priv := [KeyLen]byte(k)
	curve25519.ScalarBaseMult(&pub, &priv)
	return Key(pub)
}

// IsZero reports whether k is all zeros.
func (k Key) IsZero() bool {
	return k.Equal(Key{})
}

// Equal reports whether k and o are the same key, in constant time.
func (k Key) Equal(o Key) bool {
	return subtle.ConstantTimeCompare(k[:], o[:]) == 1
}

// LoadOrCreatePrivateKey reads a base64 encoded private key from path. If the
// file doesn't exist, it generates a key and writes it to path with mode 0600.
// created reports whether a new key was written.
func LoadOrCreatePrivateKey(path string) (k Key, created bool, err error) {
	b, err := os.ReadFile(path)
	if err == nil {
		k, err := ParseKey(string(b))
		if err != nil {
			return Key{}, false, fmt.Errorf("parse key file %s: %w", path, err)
		}
		return k, false, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return Key{}, false, fmt.Errorf("read key file %s: %w", path, err)
	}
	k, err = GeneratePrivateKey()
	if err != nil {
		return Key{}, false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return Key{}, false, fmt.Errorf("create key file directory: %w", err)
	}
	// O_EXCL so two Agent processes starting together can't each write a different key.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return LoadOrCreatePrivateKey(path)
		}
		return Key{}, false, fmt.Errorf("create key file %s: %w", path, err)
	}
	defer f.Close()
	if _, err := f.WriteString(k.String() + "\n"); err != nil {
		return Key{}, false, fmt.Errorf("write key file %s: %w", path, err)
	}
	return k, true, nil
}
