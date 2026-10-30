package tunnel

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ticketLabel separates the ticket key from the pre-shared keys, which are
// derived from the same root.
const ticketLabel = "tinkerbell register challenge ticket v1"

// Ticket is the state of a Register challenge, which the server keeps nowhere:
// the Agent carries it from the challenge to its answer, so the answer can
// reach any replica and nothing is stored or cleaned up.
//
// A challenge response carries the same secret twice, locked to two parties:
//
//   - The challenge is encrypted to the Agent's TPM, with TPM2_MakeCredential.
//     Only the TPM holding the pinned endorsement key can recover the secret,
//     with TPM2_ActivateCredential; recovering it is the proof.
//   - The ticket is sealed to the server, under a key every replica derives
//     from the PSK root. The Agent can neither read nor change it, so it can't
//     take the secret from it instead of from its TPM. That is why the ticket
//     is encrypted, not only authenticated.
//
// The Agent's next Register sends the secret its TPM recovered with the ticket
// unchanged. The server opens the ticket and accepts the answer if it matches
// the secret, before Expires, for the same Agent, keys, and the bootstrap key
// the answer arrives through. That last check binds the TPM to the tunnel's
// WireGuard key, and makes a ticket useless to any other tunnel. Used tickets
// aren't tracked: answering again from the same tunnel before Expires only
// repeats a result that tunnel already earned.
type Ticket struct {
	AgentID      string    `json:"agentID"`
	BootstrapKey string    `json:"bootstrapKey"`
	EKHash       string    `json:"ekHash"`
	AKName       []byte    `json:"akName"`
	Secret       []byte    `json:"secret"`
	Expires      time.Time `json:"expires"`
}

func (m *Manager) ticketAEAD() (cipher.AEAD, error) {
	key, err := hkdf.Key(sha256.New, m.keys.PSKRoot[:], nil, ticketLabel, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// SealTicket encrypts and authenticates t. It must be encrypted, not only
// authenticated, because it carries the answer to the challenge.
func (m *Manager) SealTicket(t Ticket) ([]byte, error) {
	aead, err := m.ticketAEAD()
	if err != nil {
		return nil, err
	}
	plain, err := json.Marshal(t) //nolint:gosec // G117: the secret is encrypted before the ticket leaves the server.
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize(), aead.NonceSize()+len(plain)+aead.Overhead())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, plain, []byte(ticketLabel)), nil
}

// OpenTicket returns the ticket sealed in b. It doesn't check the expiry.
func (m *Manager) OpenTicket(b []byte) (Ticket, error) {
	aead, err := m.ticketAEAD()
	if err != nil {
		return Ticket{}, err
	}
	if len(b) < aead.NonceSize() {
		return Ticket{}, errors.New("ticket too short")
	}
	plain, err := aead.Open(nil, b[:aead.NonceSize()], b[aead.NonceSize():], []byte(ticketLabel))
	if err != nil {
		return Ticket{}, fmt.Errorf("opening ticket: %w", err)
	}
	var t Ticket
	if err := json.Unmarshal(plain, &t); err != nil {
		return Ticket{}, fmt.Errorf("decoding ticket: %w", err)
	}
	return t, nil
}
