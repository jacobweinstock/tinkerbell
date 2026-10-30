package grpc

import (
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/google/go-tpm/tpm2"
	"github.com/google/go-tpm/tpm2/transport"
	"github.com/google/go-tpm/tpm2/transport/linuxtpm"
	"github.com/tinkerbell/tinkerbell/pkg/proto"
	protobuf "google.golang.org/protobuf/proto"
)

// tpmDevice is the kernel's resource-managed TPM, which can be shared with
// other TPM users on the machine.
const tpmDevice = "/dev/tpmrm0"

// ekCertNVIndex holds the RSA 2048 EK certificate, per the TCG EK Credential Profile.
const ekCertNVIndex = 0x01c00002

// akTemplate is the attestation key: a restricted signing key that can't leave
// the TPM. As a primary key in the endorsement hierarchy it is the same every
// time it is created, so it needs no persistent handle.
var akTemplate = tpm2.TPMTPublic{
	Type:    tpm2.TPMAlgECC,
	NameAlg: tpm2.TPMAlgSHA256,
	ObjectAttributes: tpm2.TPMAObject{
		FixedTPM:            true,
		FixedParent:         true,
		SensitiveDataOrigin: true,
		UserWithAuth:        true,
		NoDA:                true,
		Restricted:          true,
		SignEncrypt:         true,
	},
	Parameters: tpm2.NewTPMUPublicParms(tpm2.TPMAlgECC, &tpm2.TPMSECCParms{
		Scheme: tpm2.TPMTECCScheme{
			Scheme:  tpm2.TPMAlgECDSA,
			Details: tpm2.NewTPMUAsymScheme(tpm2.TPMAlgECDSA, &tpm2.TPMSSigSchemeECDSA{HashAlg: tpm2.TPMAlgSHA256}),
		},
		CurveID: tpm2.TPMECCNistP256,
	}),
}

func openTPM() (transport.TPMCloser, error) {
	return linuxtpm.Open(tpmDevice)
}

// tpmAttestor proves to the server that this machine's TPM holds its
// endorsement key (EK), for spec.agent.authentication.tpm.
type tpmAttestor struct {
	open func() (transport.TPMCloser, error)
	// ekHash is the SHA-256 of the EK's PKIX DER encoding, lowercase hex: the
	// value an operator pins in spec.agent.authentication.tpm.ekPublicKeyHash.
	ekHash   string
	evidence *proto.Evidence
}

// tpmKeys are the EK and attestation key, loaded in the TPM until flushed.
type tpmKeys struct {
	ek, ak *tpm2.CreatePrimaryResponse
}

func createTPMKeys(t transport.TPM) (*tpmKeys, error) {
	ek, err := tpm2.CreatePrimary{PrimaryHandle: tpm2.TPMRHEndorsement, InPublic: tpm2.New2B(tpm2.RSAEKTemplate)}.Execute(t)
	if err != nil {
		return nil, fmt.Errorf("creating EK: %w", err)
	}
	ak, err := tpm2.CreatePrimary{PrimaryHandle: tpm2.TPMRHEndorsement, InPublic: tpm2.New2B(akTemplate)}.Execute(t)
	if err != nil {
		_, _ = tpm2.FlushContext{FlushHandle: ek.ObjectHandle}.Execute(t)
		return nil, fmt.Errorf("creating attestation key: %w", err)
	}
	return &tpmKeys{ek: ek, ak: ak}, nil
}

func (k *tpmKeys) flush(t transport.TPM) {
	_, _ = tpm2.FlushContext{FlushHandle: k.ek.ObjectHandle}.Execute(t)
	_, _ = tpm2.FlushContext{FlushHandle: k.ak.ObjectHandle}.Execute(t)
}

// newTPMAttestor reads the TPM's EK and attestation key into the evidence sent
// with every Register.
func newTPMAttestor(open func() (transport.TPMCloser, error)) (*tpmAttestor, error) {
	t, err := open()
	if err != nil {
		return nil, err
	}
	defer t.Close()
	keys, err := createTPMKeys(t)
	if err != nil {
		return nil, err
	}
	defer keys.flush(t)

	ekPublic, err := keys.ek.OutPublic.Contents()
	if err != nil {
		return nil, fmt.Errorf("reading EK: %w", err)
	}
	pub, err := tpm2.Pub(*ekPublic)
	if err != nil {
		return nil, fmt.Errorf("reading EK: %w", err)
	}
	if _, ok := pub.(*rsa.PublicKey); !ok {
		return nil, errors.New("EK is not an RSA key")
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, fmt.Errorf("encoding EK: %w", err)
	}
	sum := sha256.Sum256(der)
	payload, err := protobuf.Marshal(&proto.TPMEvidence{
		EkPublic:      der,
		EkCertificate: readNV(t, ekCertNVIndex),
		AkPublic:      tpm2.Marshal(keys.ak.OutPublic),
	})
	if err != nil {
		return nil, err
	}
	return &tpmAttestor{
		open:     open,
		ekHash:   hex.EncodeToString(sum[:]),
		evidence: &proto.Evidence{Type: toPtr("tpm"), Payload: payload},
	}, nil
}

// answer recovers the secret in a challenge, which only a TPM that holds the
// EK the challenge was made for can do.
func (a *tpmAttestor) answer(ch *proto.Challenge) ([]byte, error) {
	var c proto.TPMChallenge
	if err := protobuf.Unmarshal(ch.GetPayload(), &c); err != nil {
		return nil, fmt.Errorf("decoding TPM challenge: %w", err)
	}
	t, err := a.open()
	if err != nil {
		return nil, err
	}
	defer t.Close()
	keys, err := createTPMKeys(t)
	if err != nil {
		return nil, err
	}
	defer keys.flush(t)
	rsp, err := tpm2.ActivateCredential{
		ActivateHandle: tpm2.NamedHandle{Handle: keys.ak.ObjectHandle, Name: keys.ak.Name},
		KeyHandle: tpm2.AuthHandle{
			Handle: keys.ek.ObjectHandle,
			Name:   keys.ek.Name,
			Auth:   tpm2.Policy(tpm2.TPMAlgSHA256, 16, ekPolicy),
		},
		CredentialBlob: tpm2.TPM2BIDObject{Buffer: c.GetCredentialBlob()},
		Secret:         tpm2.TPM2BEncryptedSecret{Buffer: c.GetEncryptedSecret()},
	}.Execute(t)
	if err != nil {
		return nil, fmt.Errorf("activating credential: %w", err)
	}
	return rsp.CertInfo.Buffer, nil
}

// ekPolicy satisfies the TCG default EK's policy: PolicySecret(TPM_RH_ENDORSEMENT).
func ekPolicy(t transport.TPM, handle tpm2.TPMISHPolicy, nonceTPM tpm2.TPM2BNonce) error {
	_, err := tpm2.PolicySecret{
		AuthHandle:    tpm2.TPMRHEndorsement,
		PolicySession: handle,
		NonceTPM:      nonceTPM,
	}.Execute(t)
	return err
}

// readNV returns the contents of NV index idx, or nil if it can't be read. It
// reads in chunks, because an index can be larger than one TPM2_NV_Read.
func readNV(t transport.TPM, idx uint32) []byte {
	pub, err := tpm2.NVReadPublic{NVIndex: tpm2.TPMHandle(idx)}.Execute(t)
	if err != nil {
		return nil
	}
	contents, err := pub.NVPublic.Contents()
	if err != nil {
		return nil
	}
	const chunk = 512
	out := make([]byte, 0, contents.DataSize)
	for off := uint16(0); off < contents.DataSize; {
		n := min(chunk, contents.DataSize-off)
		rsp, err := tpm2.NVRead{
			AuthHandle: tpm2.AuthHandle{Handle: tpm2.TPMHandle(idx), Name: pub.NVName, Auth: tpm2.PasswordAuth(nil)},
			NVIndex:    tpm2.NamedHandle{Handle: tpm2.TPMHandle(idx), Name: pub.NVName},
			Size:       n,
			Offset:     off,
		}.Execute(t)
		if err != nil {
			return nil
		}
		out = append(out, rsp.Data.Buffer...)
		off += n
	}
	return out
}
