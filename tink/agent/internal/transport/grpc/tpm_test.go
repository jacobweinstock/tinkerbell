package grpc

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-tpm/tpm2"
	"github.com/google/go-tpm/tpm2/transport"
	"github.com/google/go-tpm/tpm2/transport/linuxudstpm"
	"github.com/tinkerbell/tinkerbell/pkg/proto"
	"github.com/tinkerbell/tinkerbell/pkg/wireguard"
	protobuf "google.golang.org/protobuf/proto"
)

// swtpm starts a software TPM and returns a function that connects to it. The
// test is skipped where swtpm isn't installed.
func swtpm(t *testing.T) func() (transport.TPMCloser, error) {
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
	return func() (transport.TPMCloser, error) { return linuxudstpm.Open(sock) }
}

// challenge makes a challenge for ev the way the server does.
func challenge(t *testing.T, ev *proto.Evidence, secret []byte) *proto.Challenge {
	t.Helper()
	var te proto.TPMEvidence
	if err := protobuf.Unmarshal(ev.GetPayload(), &te); err != nil {
		t.Fatal(err)
	}
	pub, err := x509.ParsePKIXPublicKey(te.GetEkPublic())
	if err != nil {
		t.Fatal(err)
	}
	ek := tpm2.RSAEKTemplate
	ek.Unique = tpm2.NewTPMUPublicID(tpm2.TPMAlgRSA, &tpm2.TPM2BPublicKeyRSA{Buffer: pub.(*rsa.PublicKey).N.FillBytes(make([]byte, 256))})
	key, err := tpm2.ImportEncapsulationKey(&ek)
	if err != nil {
		t.Fatal(err)
	}
	ak2b, err := tpm2.Unmarshal[tpm2.TPM2BPublic](te.GetAkPublic())
	if err != nil {
		t.Fatal(err)
	}
	ak, _ := ak2b.Contents()
	name, err := tpm2.ObjectName(ak)
	if err != nil {
		t.Fatal(err)
	}
	blob, enc, err := tpm2.CreateCredential(rand.Reader, key, name.Buffer, secret)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := protobuf.Marshal(&proto.TPMChallenge{CredentialBlob: blob, EncryptedSecret: enc})
	return &proto.Challenge{Payload: payload}
}

func TestTPMAttestor(t *testing.T) {
	a, err := newTPMAttestor(swtpm(t))
	if err != nil {
		t.Fatal(err)
	}
	var te proto.TPMEvidence
	if err := protobuf.Unmarshal(a.evidence.GetPayload(), &te); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(te.GetEkPublic())
	if a.evidence.GetType() != "tpm" || a.ekHash != hex.EncodeToString(sum[:]) {
		t.Errorf("evidence type %q, ekHash %s, want tpm and the hash of %x", a.evidence.GetType(), a.ekHash, te.GetEkPublic())
	}

	// answer recreates the keys, so this also checks they come back the same.
	secret := []byte("0123456789abcdef0123456789abcdef")
	got, err := a.answer(challenge(t, a.evidence, secret))
	if err != nil {
		t.Fatalf("answer: %v", err)
	}
	if !bytes.Equal(got, secret) {
		t.Errorf("answer = %x, want %x", got, secret)
	}
}

func TestInitTPM(t *testing.T) {
	p, _ := wireguard.GeneratePrefix()
	agent := func(open func() (transport.TPMCloser, error)) *WireGuard {
		return &WireGuard{Log: logr.Discard(), KeyFile: filepath.Join(t.TempDir(), "wireguard.key"), Prefix: p, TPM: true, openTPM: open}
	}
	for name, open := range map[string]func() (transport.TPMCloser, error){
		"no TPM":       func() (transport.TPMCloser, error) { return nil, fs.ErrNotExist },
		"unusable TPM": func() (transport.TPMCloser, error) { return nil, errors.New("device busy") },
	} {
		w := agent(open)
		if err := w.Init(); err != nil {
			t.Errorf("%s: Init: %v", name, err)
		}
		if w.attestor != nil {
			t.Errorf("%s: TPM evidence prepared", name)
		}
	}

	w := agent(swtpm(t))
	if err := w.Init(); err != nil {
		t.Fatal(err)
	}
	if w.attestor == nil || w.ekHash() == "" {
		t.Error("no TPM evidence with a TPM")
	}
}
