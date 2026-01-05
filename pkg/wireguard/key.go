package wireguard

import (
	"crypto/subtle"
	"encoding/base64"
	"fmt"

	"golang.org/x/crypto/curve25519"
)

// KeyLen is the expected key length for a WireGuard key.
const KeyLen = 32

// A Key is a public, private, or pre-shared secret key
// that is base64 encoded.
// The Key constructor functions in this package can be
// used to create Keys suitable for each of these applications.
type Key [KeyLen]byte

// String returns the base64-encoded string representation of a Key.
//
// ParseKey can be used to produce a new Key from this string.
func (k Key) String() string {
	return base64.StdEncoding.EncodeToString(k[:])
}

// PublicKey computes a public key from the private key k.
//
// PublicKey should only be called when k is a private key.
func (k Key) PublicKey() Key {
	var (
		pub  [KeyLen]byte
		priv = [KeyLen]byte(k)
	)

	// ScalarBaseMult uses the correct base value per https://cr.yp.to/ecdh.html,
	// so no need to specify it.
	curve25519.ScalarBaseMult(&pub, &priv)

	return Key(pub)
}

func (k Key) IsZero() bool {
	return k.Equals(Key{})
}

func (k Key) Equals(in Key) bool {
	return subtle.ConstantTimeCompare(k[:], in[:]) == 1
}

// NewKey takes a base64 encoded key that the wg command produces and returns a Key.
func NewKey(base64encoded []byte) (Key, error) {
	k, err := base64.StdEncoding.DecodeString(string(base64encoded))
	if err != nil {
		return Key{}, err
	}

	if len(k) == 32 {
		return Key(k), nil
	}

	return Key{}, fmt.Errorf("key is not the correct length, want: 32, got: %v", len(k))
}
