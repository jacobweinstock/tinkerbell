package grpc

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/go-tpm/tpm2"
	"github.com/tinkerbell/tinkerbell/pkg/proto"
	"github.com/tinkerbell/tinkerbell/tink/server/tunnel"
	protobuf "google.golang.org/protobuf/proto"
)

const (
	// challengeTTL bounds how long a TPM challenge can be answered. The Agent
	// answers at once, so it only needs to cover a TPM operation and a round trip.
	challengeTTL = 30 * time.Second
	// tpmSecretLen is the size of a challenge's secret. ActivateCredential
	// returns it as a TPM2B_DIGEST, so it must fit the EK's name hash, SHA-256.
	tpmSecretLen = 32
	// rsaEKBits is the key size of the TCG default RSA EK template.
	rsaEKBits = 2048
)

// evidenceTypeTPM is the Register evidence type a TPM's Agent sends.
const evidenceTypeTPM = "tpm"

var errNoTPMEvidence = errors.New("no TPM evidence")

// tpmIdentity is a caller's TPM, as its evidence describes it.
type tpmIdentity struct {
	ek *rsa.PublicKey
	// ekHash is the SHA-256 of the EK's PKIX DER encoding, lowercase hex: the
	// value Hardware pins as spec.agent.authentication.tpm.ekPublicKeyHash.
	ekHash string
	// akName is the attestation key's TPM name, which the challenge is bound to.
	akName []byte
}

// parseTPMEvidence returns the TPM ev describes. It checks that the
// attestation key can't leave the TPM; that the TPM holds the EK is only proved
// by answering a challenge.
func parseTPMEvidence(ev *proto.Evidence) (*tpmIdentity, error) {
	if ev == nil || ev.GetType() != evidenceTypeTPM {
		return nil, errNoTPMEvidence
	}
	var te proto.TPMEvidence
	if err := protobuf.Unmarshal(ev.GetPayload(), &te); err != nil {
		return nil, fmt.Errorf("decoding TPM evidence: %w", err)
	}
	pub, err := x509.ParsePKIXPublicKey(te.GetEkPublic())
	if err != nil {
		return nil, fmt.Errorf("parsing EK public key: %w", err)
	}
	ek, ok := pub.(*rsa.PublicKey)
	if !ok || ek.N.BitLen() != rsaEKBits || ek.E != 65537 {
		return nil, errors.New("EK is not an RSA 2048 key from the TCG default template")
	}
	// Hash a canonical encoding, so the hash doesn't depend on how the Agent encoded it.
	der, err := x509.MarshalPKIXPublicKey(ek)
	if err != nil {
		return nil, fmt.Errorf("encoding EK public key: %w", err)
	}
	sum := sha256.Sum256(der)

	ak2b, err := tpm2.Unmarshal[tpm2.TPM2BPublic](te.GetAkPublic())
	if err != nil {
		return nil, fmt.Errorf("decoding attestation key: %w", err)
	}
	ak, err := ak2b.Contents()
	if err != nil {
		return nil, fmt.Errorf("decoding attestation key: %w", err)
	}
	a := ak.ObjectAttributes
	if !a.FixedTPM || !a.FixedParent || !a.SensitiveDataOrigin || !a.Restricted || !a.SignEncrypt || a.Decrypt {
		return nil, errors.New("attestation key is not a restricted signing key that can't leave its TPM")
	}
	name, err := tpm2.ObjectName(ak)
	if err != nil {
		return nil, fmt.Errorf("computing attestation key name: %w", err)
	}
	return &tpmIdentity{ek: ek, ekHash: hex.EncodeToString(sum[:]), akName: name.Buffer}, nil
}

// makeCredential runs TPM2_MakeCredential in software: only a TPM that holds
// id's EK, with an object of id's attestation key name loaded, can recover secret.
func makeCredential(id *tpmIdentity, secret []byte) (*proto.TPMChallenge, error) {
	ek := tpm2.RSAEKTemplate
	ek.Unique = tpm2.NewTPMUPublicID(tpm2.TPMAlgRSA, &tpm2.TPM2BPublicKeyRSA{Buffer: id.ek.N.FillBytes(make([]byte, rsaEKBits/8))})
	key, err := tpm2.ImportEncapsulationKey(&ek)
	if err != nil {
		return nil, fmt.Errorf("importing EK: %w", err)
	}
	blob, enc, err := tpm2.CreateCredential(rand.Reader, key, id.akName, secret)
	if err != nil {
		return nil, fmt.Errorf("creating credential: %w", err)
	}
	return &proto.TPMChallenge{CredentialBlob: blob, EncryptedSecret: enc}, nil
}

// TicketSealer seals and opens challenge tickets.
type TicketSealer interface {
	SealTicket(t tunnel.Ticket) ([]byte, error)
	OpenTicket(b []byte) (tunnel.Ticket, error)
}

// tpmChallenge returns a challenge for agentID's caller, whose bootstrap key is key.
func tpmChallenge(ts TicketSealer, id *tpmIdentity, agentID, key string, now time.Time) (*proto.Challenge, error) {
	secret := make([]byte, tpmSecretLen)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	c, err := makeCredential(id, secret)
	if err != nil {
		return nil, err
	}
	payload, err := protobuf.Marshal(c)
	if err != nil {
		return nil, err
	}
	ticket, err := ts.SealTicket(tunnel.Ticket{
		AgentID:      agentID,
		BootstrapKey: key,
		EKHash:       id.ekHash,
		AKName:       id.akName,
		Secret:       secret,
		Expires:      now.Add(challengeTTL),
	})
	if err != nil {
		return nil, fmt.Errorf("sealing ticket: %w", err)
	}
	return &proto.Challenge{Payload: payload, Ticket: ticket}, nil
}

// errTicketExpired means the answer came too late, not that it was wrong.
var errTicketExpired = errors.New("challenge expired")

// checkTPMAnswer checks an answer to a challenge from tpmChallenge. The ticket
// must be for the same Agent, bootstrap key, EK, and attestation key.
func checkTPMAnswer(ts TicketSealer, r *proto.ChallengeResponse, id *tpmIdentity, agentID, key string, now time.Time) error {
	t, err := ts.OpenTicket(r.GetTicket())
	if err != nil {
		return err
	}
	if !now.Before(t.Expires) {
		return errTicketExpired
	}
	if t.AgentID != agentID || t.BootstrapKey != key || t.EKHash != id.ekHash || !bytes.Equal(t.AKName, id.akName) {
		return errors.New("challenge was issued to another caller")
	}
	if subtle.ConstantTimeCompare(t.Secret, r.GetPayload()) != 1 {
		return errors.New("wrong answer")
	}
	return nil
}
