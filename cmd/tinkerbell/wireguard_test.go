package main

import (
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"github.com/tinkerbell/tinkerbell/pkg/constant"

	"github.com/go-logr/logr"
	"github.com/tinkerbell/tinkerbell/cmd/tinkerbell/flag"
	"github.com/tinkerbell/tinkerbell/tink/server/tunnel"
)

func TestGenerateMissing(t *testing.T) {
	dir := t.TempDir()
	w := &flag.WireGuardConfig{GeneratedDir: dir}
	if err := generateMissing(logr.Discard(), w); err != nil {
		t.Fatal(err)
	}
	k1, err := tunnel.LoadKeys(tunnel.KeyFiles{Prefix: w.Prefix, BootstrapFile: w.BootstrapKeyFile, SessionFile: w.SessionKeyFile, PSKRootFile: w.PSKRootFile})
	if err != nil {
		t.Fatalf("generated keys don't load: %v", err)
	}
	for _, f := range []string{"bootstrap.key", "session.key", "psk-root", "prefix"} {
		fi, err := os.Stat(filepath.Join(dir, f))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %v, want 0600", f, fi.Mode().Perm())
		}
	}

	// A restart finds the same values.
	w2 := &flag.WireGuardConfig{GeneratedDir: dir}
	if err := generateMissing(logr.Discard(), w2); err != nil {
		t.Fatal(err)
	}
	k2, err := tunnel.LoadKeys(tunnel.KeyFiles{Prefix: w2.Prefix, BootstrapFile: w2.BootstrapKeyFile, SessionFile: w2.SessionKeyFile, PSKRootFile: w2.PSKRootFile})
	if err != nil {
		t.Fatal(err)
	}
	if !k1.Bootstrap.Equal(k2.Bootstrap) || !k1.Session.Equal(k2.Session) || !k1.PSKRoot.Equal(k2.PSKRoot) || k1.Prefix != k2.Prefix {
		t.Error("keys changed across restarts")
	}

	// Values that are set are left alone.
	w3 := &flag.WireGuardConfig{GeneratedDir: t.TempDir(), Prefix: "fd12:3456:789a::/48", BootstrapKeyFile: w.BootstrapKeyFile}
	if err := generateMissing(logr.Discard(), w3); err != nil {
		t.Fatal(err)
	}
	if w3.Prefix != "fd12:3456:789a::/48" || w3.BootstrapKeyFile != w.BootstrapKeyFile {
		t.Errorf("set values changed: %+v", w3)
	}
	if w3.SessionKeyFile == w.SessionKeyFile {
		t.Error("unset session key not generated in its own directory")
	}
}

func TestWireGuardFamilies(t *testing.T) {
	w := &flag.WireGuardConfig{Mode: "optional", BootstrapPort: 51820, SessionPort: 51821, BindAddrV6: netip.MustParseAddr("2001:db8::5")}
	if err := w.Validate(constant.ListenerFamiliesIPv4); err != nil {
		t.Errorf("IPv4 only, IPv6 ports unset: %v", err)
	}
	if err := w.Validate(constant.ListenerFamiliesDual); err == nil {
		t.Error("dual stack without IPv6 ports accepted")
	}

	v4, v6 := w.Convert(constant.ListenerFamiliesIPv4,
		netip.MustParseAddr("192.0.2.50"), netip.IPv6Unspecified(), netip.MustParseAddr("192.0.2.50"), netip.Addr{})
	if !v4.Enabled || v4.BindAddr.String() != "192.0.2.50" || v4.PublicAddr.String() != "192.0.2.50" {
		t.Errorf("IPv4 = %+v, want the global bind and public address", v4)
	}
	if v6.Enabled {
		t.Error("IPv6 enabled with listener families ipv4")
	}
	if v6.BindAddr.String() != "2001:db8::5" {
		t.Errorf("IPv6 bind address = %s, want the WireGuard specific one", v6.BindAddr)
	}

	w.PreviousBootstrapKeyFile = "/old.key"
	if err := w.Validate(constant.ListenerFamiliesIPv4); err == nil {
		t.Error("previous bootstrap key without a previous port accepted")
	}
	w.PreviousBootstrapPort = 51821
	if err := w.Validate(constant.ListenerFamiliesIPv4); err == nil {
		t.Error("previous bootstrap port equal to the session port accepted")
	}
}

func TestAgentAuthenticationPolicy(t *testing.T) {
	w := &flag.WireGuardConfig{Mode: "optional", AuthenticationPolicy: "pinned", BootstrapPort: 51820, SessionPort: 51821}
	if err := w.Validate(constant.ListenerFamiliesIPv4); err == nil {
		t.Error("pinned policy accepted in optional mode")
	}
	w.Mode = "required"
	if err := w.Validate(constant.ListenerFamiliesIPv4); err != nil {
		t.Errorf("pinned policy in required mode: %v", err)
	}
	w.AuthenticationPolicy = "strict"
	if err := w.Validate(constant.ListenerFamiliesIPv4); err == nil {
		t.Error("unknown policy accepted")
	}
	// Disabled WireGuard ignores the other WireGuard flags.
	w.Mode = "disabled"
	if err := w.Validate(constant.ListenerFamiliesIPv4); err != nil {
		t.Errorf("disabled: %v", err)
	}
}
