package server

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/tinkerbell/tinkerbell/pkg/wireguard"
	"github.com/tinkerbell/tinkerbell/tink/server/tunnel"
)

// TestStartStopsCleanlyInTunnels checks that cancelling the context stops a
// Tink Server serving only inside the WireGuard tunnels without an error, while
// the tunnel manager is running too.
func TestStartStopsCleanlyInTunnels(t *testing.T) {
	boot, _ := wireguard.GeneratePrivateKey()
	sess, _ := wireguard.GeneratePrivateKey()
	root, _ := wireguard.GenerateKey()
	prefix, _ := wireguard.GeneratePrefix()
	lo := netip.MustParseAddr("127.0.0.1")
	m, err := tunnel.New(tunnel.Config{
		Mode: tunnel.ModeRequired,
		V4:   tunnel.Family{Enabled: true, BindAddr: lo, PublicAddr: lo},
		Log:  logr.Discard(),
	}, &tunnel.ServerKeys{Prefix: prefix, PSKRoot: root, Bootstrap: boot, Session: sess})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- m.Run(ctx) }()
	startDone := make(chan error, 1)
	go func() { startDone <- (&Config{Tunnel: m}).Start(ctx, logr.Discard()) }()

	time.Sleep(200 * time.Millisecond)
	cancel()
	select {
	case err := <-startDone:
		if err != nil {
			t.Errorf("Start returned %v after the context was cancelled, want nil", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Start did not return after the context was cancelled")
	}
	if err := <-runDone; err != nil {
		t.Errorf("Run returned %v", err)
	}
}
