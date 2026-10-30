package wireguard

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr"
)

func TestPeerAddr(t *testing.T) {
	p, err := ParsePrefix("fd12:3456:789a::/48")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := BootstrapPrefix(p).String(), "fd12:3456:789a::/64"; got != want {
		t.Errorf("BootstrapPrefix = %s, want %s", got, want)
	}
	if got, want := SessionPrefix(p).String(), "fd12:3456:789a:1::/64"; got != want {
		t.Errorf("SessionPrefix = %s, want %s", got, want)
	}
	if got, want := ServerAddr(SessionPrefix(p)).String(), "fd12:3456:789a:1::1"; got != want {
		t.Errorf("ServerAddr = %s, want %s", got, want)
	}
	k, _ := GeneratePrivateKey()
	a := PeerAddr(BootstrapPrefix(p), k.PublicKey())
	if !BootstrapPrefix(p).Contains(a) {
		t.Errorf("PeerAddr %s not in %s", a, BootstrapPrefix(p))
	}
	if a != PeerAddr(BootstrapPrefix(p), k.PublicKey()) {
		t.Error("PeerAddr is not deterministic")
	}
}

func TestLoadOrCreatePrivateKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "wg.key")
	k1, created, err := LoadOrCreatePrivateKey(path)
	if err != nil || !created {
		t.Fatalf("first load: created=%v err=%v", created, err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
	k2, created, err := LoadOrCreatePrivateKey(path)
	if err != nil || created {
		t.Fatalf("second load: created=%v err=%v", created, err)
	}
	if !k1.Equal(k2) {
		t.Error("reloaded key differs")
	}
}

// TestTunnel brings up a server that admits peers just in time, as the
// bootstrap device does, and checks that only the admitted key's address can
// talk to it. It runs with the default bind and with one bound to an address.
func TestTunnel(t *testing.T) {
	t.Run("all interfaces", func(t *testing.T) { roundTrip(t, nil) })
	t.Run("bind address", func(t *testing.T) {
		roundTrip(t, []netip.AddrPort{netip.MustParseAddrPort("127.0.0.1:0")})
	})
}

// roundTrip runs the TestTunnel scenario with the server bound to bindAddrs.
func roundTrip(t *testing.T, bindAddrs []netip.AddrPort) {
	t.Helper()
	prefix, _ := GeneratePrefix()
	p64 := BootstrapPrefix(prefix)
	serverKey, _ := GeneratePrivateKey()
	clientKey, _ := GeneratePrivateKey()
	psk, _ := GenerateKey()

	var admitted atomic.Int32
	server, err := New(Config{
		PrivateKey: serverKey,
		BindAddrs:  bindAddrs,
		Addresses:  []netip.Addr{ServerAddr(p64)},
		Lookup: func(pub Key, _ netip.AddrPort) (PeerConfig, bool) {
			if !pub.Equal(clientKey.PublicKey()) {
				return PeerConfig{}, false
			}
			admitted.Add(1)
			return PeerConfig{
				AllowedIPs:   []netip.Prefix{netip.PrefixFrom(PeerAddr(p64, pub), 128)},
				PresharedKey: psk,
			}, true
		},
		Log: logr.Discard(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	ln, err := server.ListenTCP(4242)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	remotes := make(chan string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		remotes <- c.RemoteAddr().String()
		line, _ := bufio.NewReader(c).ReadString('\n')
		fmt.Fprint(c, "echo "+line)
	}()

	client, err := New(Config{
		PrivateKey: clientKey,
		Addresses:  []netip.Addr{PeerAddr(p64, clientKey.PublicKey())},
		Peers: []PeerConfig{{
			PublicKey:    serverKey.PublicKey(),
			AllowedIPs:   []netip.Prefix{p64},
			PresharedKey: psk,
			Endpoint:     netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), server.ListenPort()),
		}},
		Log: logr.Discard(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := client.DialContext(ctx, "tcp", net.JoinHostPort(ServerAddr(p64).String(), "4242"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	fmt.Fprint(c, "hello\n")
	resp, err := bufio.NewReader(c).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if resp != "echo hello\n" {
		t.Errorf("response = %q", resp)
	}
	remote := <-remotes
	ap, _ := netip.ParseAddrPort(remote)
	if ap.Addr() != PeerAddr(p64, clientKey.PublicKey()) {
		t.Errorf("server saw %s, want derived address %s", ap.Addr(), PeerAddr(p64, clientKey.PublicKey()))
	}
	if admitted.Load() == 0 {
		t.Error("lookup never admitted the client")
	}
	if ep, ok := server.PeerEndpoint(clientKey.PublicKey()); !ok || !ep.Addr().IsLoopback() {
		t.Errorf("PeerEndpoint = %v, %v", ep, ok)
	}
}
