package grpc

import (
	"net/netip"
	"path/filepath"
	"testing"

	"github.com/go-logr/logr"
	"github.com/tinkerbell/tinkerbell/pkg/proto"
	"github.com/tinkerbell/tinkerbell/pkg/wireguard"
)

func TestMoveToSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	oldKey, _ := wireguard.GeneratePrivateKey()
	newKey, _ := wireguard.GeneratePrivateKey()
	boot := WireGuardServer{PublicKey: oldKey.PublicKey(), Endpoint: netip.MustParseAddrPort("192.0.2.1:51820")}
	prefix, _ := wireguard.ParsePrefix("fd12:3456:789a::/48")
	newAgent := func() *WireGuard {
		w := &WireGuard{Log: logr.Discard(), KeyFile: filepath.Join(dir, "wireguard.key"), Server: boot, Prefix: prefix}
		if err := w.Init(); err != nil {
			t.Fatal(err)
		}
		return w
	}

	w := newAgent()
	pub := newKey.PublicKey()
	if !w.moveTo(&proto.BootstrapServer{PublicKey: pub[:], Endpoint: toPtr("192.0.2.1:51822")}) {
		t.Fatal("valid redirect not followed")
	}
	want := WireGuardServer{PublicKey: pub, Endpoint: netip.MustParseAddrPort("192.0.2.1:51822")}
	if w.Server != want {
		t.Fatalf("after move_to, server = %+v", w.Server)
	}

	// A restarted Agent, given the same boot parameters, keeps the new server.
	if got := newAgent().Server; got != want {
		t.Errorf("after restart, server = %+v, want %+v", got, want)
	}

	// A malformed or empty redirect is ignored, which is what an Agent sees when
	// it reads a field the server meant as something else.
	if w.moveTo(&proto.BootstrapServer{PublicKey: []byte("short"), Endpoint: toPtr("192.0.2.1:1")}) {
		t.Error("malformed redirect followed")
	}
	if w.moveTo(&proto.BootstrapServer{}) {
		t.Error("empty redirect followed")
	}
	if w.Server != want {
		t.Errorf("malformed redirect changed the server to %+v", w.Server)
	}
}
