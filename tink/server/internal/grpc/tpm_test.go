package grpc

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-tpm/tpm2"
	"github.com/google/go-tpm/tpm2/transport"
	"github.com/google/go-tpm/tpm2/transport/linuxudstpm"
	"github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	"github.com/tinkerbell/tinkerbell/pkg/proto"
	"github.com/tinkerbell/tinkerbell/pkg/wireguard"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	protobuf "google.golang.org/protobuf/proto"
)

// testTPM is an Agent's TPM, backed by swtpm. It mirrors the Agent's TPM
// code, which the server can't import.
type testTPM struct {
	tpm      transport.TPMCloser
	ek, ak   *tpm2.CreatePrimaryResponse
	ekHash   string
	evidence *proto.Evidence
}

var testAKTemplate = tpm2.TPMTPublic{
	Type:    tpm2.TPMAlgECC,
	NameAlg: tpm2.TPMAlgSHA256,
	ObjectAttributes: tpm2.TPMAObject{
		FixedTPM: true, FixedParent: true, SensitiveDataOrigin: true, UserWithAuth: true,
		NoDA: true, Restricted: true, SignEncrypt: true,
	},
	Parameters: tpm2.NewTPMUPublicParms(tpm2.TPMAlgECC, &tpm2.TPMSECCParms{
		Scheme: tpm2.TPMTECCScheme{
			Scheme:  tpm2.TPMAlgECDSA,
			Details: tpm2.NewTPMUAsymScheme(tpm2.TPMAlgECDSA, &tpm2.TPMSSigSchemeECDSA{HashAlg: tpm2.TPMAlgSHA256}),
		},
		CurveID: tpm2.TPMECCNistP256,
	}),
}

// newTestTPM starts swtpm, skipping the test where it isn't installed.
func newTestTPM(t *testing.T) *testTPM {
	t.Helper()
	bin, err := exec.LookPath("swtpm")
	if err != nil {
		t.Skip("swtpm not installed")
	}
	dir := t.TempDir()
	sock := filepath.Join(dir, "tpm.sock")
	cmd := exec.Command(bin, "socket", "--tpm2", "--tpmstate", "dir="+dir,
		"--server", "type=unixio,path="+sock, "--flags", "not-need-init,startup-clear")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	for i := 0; ; i++ {
		if _, err := os.Stat(sock); err == nil {
			break
		}
		if i == 100 {
			t.Fatal("swtpm socket did not appear")
		}
		time.Sleep(20 * time.Millisecond)
	}
	tp, err := linuxudstpm.Open(sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tp.Close() })

	ek, err := tpm2.CreatePrimary{PrimaryHandle: tpm2.TPMRHEndorsement, InPublic: tpm2.New2B(tpm2.RSAEKTemplate)}.Execute(tp)
	if err != nil {
		t.Fatal(err)
	}
	ak, err := tpm2.CreatePrimary{PrimaryHandle: tpm2.TPMRHEndorsement, InPublic: tpm2.New2B(testAKTemplate)}.Execute(tp)
	if err != nil {
		t.Fatal(err)
	}
	ekPub, _ := ek.OutPublic.Contents()
	pub, err := tpm2.Pub(*ekPub)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKIXPublicKey(pub)
	ev := evidence(t, der, tpm2.Marshal(ak.OutPublic))
	id, err := parseTPMEvidence(ev)
	if err != nil {
		t.Fatal(err)
	}
	return &testTPM{tpm: tp, ek: ek, ak: ak, ekHash: id.ekHash, evidence: ev}
}

func evidence(t *testing.T, ekDER, akPublic []byte) *proto.Evidence {
	t.Helper()
	payload, err := protobuf.Marshal(&proto.TPMEvidence{EkPublic: ekDER, AkPublic: akPublic})
	if err != nil {
		t.Fatal(err)
	}
	return &proto.Evidence{Type: toPtr("tpm"), Payload: payload}
}

func (tt *testTPM) answer(t *testing.T, ch *proto.Challenge) []byte {
	t.Helper()
	var c proto.TPMChallenge
	if err := protobuf.Unmarshal(ch.GetPayload(), &c); err != nil {
		t.Fatal(err)
	}
	rsp, err := tpm2.ActivateCredential{
		ActivateHandle: tpm2.NamedHandle{Handle: tt.ak.ObjectHandle, Name: tt.ak.Name},
		KeyHandle: tpm2.AuthHandle{
			Handle: tt.ek.ObjectHandle,
			Name:   tt.ek.Name,
			Auth: tpm2.Policy(tpm2.TPMAlgSHA256, 16, func(tp transport.TPM, h tpm2.TPMISHPolicy, n tpm2.TPM2BNonce) error {
				_, err := tpm2.PolicySecret{AuthHandle: tpm2.TPMRHEndorsement, PolicySession: h, NonceTPM: n}.Execute(tp)
				return err
			}),
		},
		CredentialBlob: tpm2.TPM2BIDObject{Buffer: c.GetCredentialBlob()},
		Secret:         tpm2.TPM2BEncryptedSecret{Buffer: c.GetEncryptedSecret()},
	}.Execute(tt.tpm)
	if err != nil {
		t.Fatalf("ActivateCredential: %v", err)
	}
	return rsp.CertInfo.Buffer
}

func tpmHardware(ekHash string) *tinkerbell.Hardware {
	hw := hardware("")
	hw.Spec.Agent.Authentication = &tinkerbell.AgentAuthentication{TPM: &tinkerbell.TPMAuthentication{EKPublicKeyHash: ekHash}}
	return hw
}

// tpmRegister registers key for the test Agent with tt's evidence, answering
// a challenge if one comes back.
func tpmRegister(t *testing.T, b *registerBackend, tun *fakeTunnel, tt *testTPM, key wireguard.Key, now time.Time) (*proto.RegisterResponse, error) {
	t.Helper()
	h := &Handler{Backend: b}
	req := &proto.RegisterRequest{AgentId: toPtr(testAgent), Evidence: tt.evidence}
	resp, err := registerReq(t, h, tun, key, now, req)
	if err != nil || resp.GetState() != proto.RegisterResponse_STATE_CHALLENGE {
		return resp, err
	}
	req.ChallengeResponse = &proto.ChallengeResponse{Payload: tt.answer(t, resp.GetChallenge()), Ticket: resp.GetChallenge().GetTicket()}
	return registerReq(t, h, tun, key, now, req)
}

func TestRegisterTPM(t *testing.T) {
	tt := newTestTPM(t)
	now := time.Now()
	tun := &fakeTunnel{pinned: true}

	// An idle Agent isn't challenged.
	idle := &registerBackend{hw: tpmHardware(tt.ekHash)}
	if resp, err := registerReq(t, &Handler{Backend: idle}, tun, newKey(t), now, &proto.RegisterRequest{AgentId: toPtr(testAgent), Evidence: tt.evidence}); err != nil || resp.GetState() != proto.RegisterResponse_STATE_WAITING {
		t.Fatalf("idle Register = %v, %v; want WAITING", resp, err)
	}

	b := &registerBackend{hw: tpmHardware(tt.ekHash), wf: pendingWorkflow()}
	first := newKey(t)
	resp, err := tpmRegister(t, b, tun, tt, first, now)
	if err != nil || resp.GetState() != proto.RegisterResponse_STATE_GRANTED {
		t.Fatalf("Register = %v, %v; want GRANTED", resp, err)
	}
	rec := b.wf.Status.Agent(testAgent)
	if c := rec.Credential; c.Type != tinkerbell.CredentialTypeTPM || c.Trust != tinkerbell.CredentialTrustPinned ||
		c.WireGuard.PublicKey != first.String() || c.TPM == nil || c.TPM.EKPublicKeyHash != tt.ekHash {
		t.Fatalf("session record = %+v", rec)
	}
	firstSession := rec.Session.PublicKey

	// After a reboot, the same TPM vouches for a new key, which continues the Workflow.
	second := newKey(t)
	resp, err = tpmRegister(t, b, tun, tt, second, now.Add(time.Minute))
	if err != nil || resp.GetState() != proto.RegisterResponse_STATE_GRANTED {
		t.Fatalf("Register after reboot = %v, %v; want GRANTED", resp, err)
	}
	rec = b.wf.Status.Agent(testAgent)
	if rec.Credential.WireGuard.PublicKey != second.String() || rec.Session.PublicKey == firstSession {
		t.Errorf("session record after reboot = %+v; want it moved to the new key", rec)
	}
}

func TestRegisterTPMRefusals(t *testing.T) {
	tt := newTestTPM(t)
	now := time.Now()
	start := func(t *testing.T, tun *fakeTunnel, pinned string, key wireguard.Key) (*registerBackend, *proto.RegisterRequest, *proto.Challenge) {
		t.Helper()
		b := &registerBackend{hw: tpmHardware(pinned), wf: pendingWorkflow()}
		req := &proto.RegisterRequest{AgentId: toPtr(testAgent), Evidence: tt.evidence}
		resp, err := registerReq(t, &Handler{Backend: b}, tun, key, now, req)
		if err != nil || resp.GetState() != proto.RegisterResponse_STATE_CHALLENGE {
			t.Fatalf("Register = %v, %v; want CHALLENGE", resp, err)
		}
		return b, req, resp.GetChallenge()
	}
	refusedAs := func(t *testing.T, reason string, call func() error) {
		t.Helper()
		before := failures(tinkerbell.CredentialTypeTPM, tinkerbell.CredentialTrustPinned, reason)
		if err := call(); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("Register: %v, want PermissionDenied", err)
		}
		if failures(tinkerbell.CredentialTypeTPM, tinkerbell.CredentialTrustPinned, reason) != before+1 {
			t.Errorf("refusal %s not counted", reason)
		}
	}

	t.Run("no evidence", func(t *testing.T) {
		b := &registerBackend{hw: tpmHardware(tt.ekHash), wf: pendingWorkflow()}
		refusedAs(t, failedTPMEK, func() error {
			_, err := registerWith(t, &Handler{Backend: b}, &fakeTunnel{}, newKey(t), now)
			return err
		})
	})
	t.Run("another EK", func(t *testing.T) {
		b := &registerBackend{hw: tpmHardware(strings.Repeat("0", 64)), wf: pendingWorkflow()}
		refusedAs(t, failedTPMEK, func() error {
			_, err := registerReq(t, &Handler{Backend: b}, &fakeTunnel{}, newKey(t), now, &proto.RegisterRequest{AgentId: toPtr(testAgent), Evidence: tt.evidence})
			return err
		})
	})
	t.Run("wrong answer", func(t *testing.T) {
		tun, key := &fakeTunnel{}, newKey(t)
		b, req, ch := start(t, tun, tt.ekHash, key)
		req.ChallengeResponse = &proto.ChallengeResponse{Payload: make([]byte, tpmSecretLen), Ticket: ch.GetTicket()}
		refusedAs(t, failedTPMChallenge, func() error {
			_, err := registerReq(t, &Handler{Backend: b}, tun, key, now, req)
			return err
		})
	})
	t.Run("answer through another key", func(t *testing.T) {
		tun := &fakeTunnel{}
		b, req, ch := start(t, tun, tt.ekHash, newKey(t))
		req.ChallengeResponse = &proto.ChallengeResponse{Payload: tt.answer(t, ch), Ticket: ch.GetTicket()}
		refusedAs(t, failedTPMChallenge, func() error {
			_, err := registerReq(t, &Handler{Backend: b}, tun, newKey(t), now, req)
			return err
		})
		if b.wf.Status.Agent(testAgent) != nil {
			t.Error("a session was granted")
		}
	})
	t.Run("late answer gets a new challenge", func(t *testing.T) {
		tun, key := &fakeTunnel{}, newKey(t)
		b, req, ch := start(t, tun, tt.ekHash, key)
		req.ChallengeResponse = &proto.ChallengeResponse{Payload: tt.answer(t, ch), Ticket: ch.GetTicket()}
		resp, err := registerReq(t, &Handler{Backend: b}, tun, key, now.Add(challengeTTL), req)
		if err != nil || resp.GetState() != proto.RegisterResponse_STATE_CHALLENGE {
			t.Fatalf("Register = %v, %v; want another CHALLENGE", resp, err)
		}
	})
}

func TestParseTPMEvidence(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	ekDER, _ := x509.MarshalPKIXPublicKey(&rsaKey.PublicKey)
	ecKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	ecDER, _ := x509.MarshalPKIXPublicKey(&ecKey.PublicKey)
	ak := testAKTemplate
	ak.Unique = tpm2.NewTPMUPublicID(tpm2.TPMAlgECC, &tpm2.TPMSECCPoint{
		X: tpm2.TPM2BECCParameter{Buffer: ecKey.X.FillBytes(make([]byte, 32))},
		Y: tpm2.TPM2BECCParameter{Buffer: ecKey.Y.FillBytes(make([]byte, 32))},
	})
	akPublic := tpm2.Marshal(tpm2.New2B(ak))
	exportable := ak
	exportable.ObjectAttributes.FixedTPM = false
	exportablePublic := tpm2.Marshal(tpm2.New2B(exportable))

	if id, err := parseTPMEvidence(evidence(t, ekDER, akPublic)); err != nil || len(id.ekHash) != 64 {
		t.Fatalf("valid evidence: %+v, %v", id, err)
	}
	for name, ev := range map[string]*proto.Evidence{
		"none":            nil,
		"other type":      {Type: toPtr("spiffe")},
		"ECC EK":          evidence(t, ecDER, akPublic),
		"exportable AK":   evidence(t, ekDER, exportablePublic),
		"garbage payload": {Type: toPtr("tpm"), Payload: []byte{0xff}},
	} {
		if _, err := parseTPMEvidence(ev); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}
