package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-logr/logr"
	"github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	"github.com/tinkerbell/tinkerbell/cmd/tinkerbell/flag"
	"github.com/tinkerbell/tinkerbell/pkg/backend/kube"
	"github.com/tinkerbell/tinkerbell/pkg/wireguard"
	"github.com/tinkerbell/tinkerbell/tink/server/tunnel"
)

const (
	defaultWireGuardBootstrapPort = 51820
	defaultWireGuardSessionPort   = 51821
)

// newTunnel loads the WireGuard server keys, brings up the tunnel devices, and
// keeps the session index in line with the backend's Workflows.
func newTunnel(ctx context.Context, log logr.Logger, w *flag.WireGuardConfig, mode tunnel.Mode, b *kube.Backend, v4, v6 tunnel.Family) (*tunnel.Manager, error) {
	if err := generateMissing(log, w); err != nil {
		return nil, err
	}
	keys, err := tunnel.LoadKeys(tunnel.KeyFiles{
		Prefix:        w.Prefix,
		BootstrapFile: w.BootstrapKeyFile,
		SessionFile:   w.SessionKeyFile,
		PSKRootFile:   w.PSKRootFile,

		PreviousBootstrapFile: w.PreviousBootstrapKeyFile,
	})
	if err != nil {
		return nil, err
	}
	m, err := tunnel.New(tunnel.Config{
		Mode:                 mode,
		AuthenticationPolicy: tunnel.AuthenticationPolicy(w.AuthenticationPolicy),
		V4:                   v4,
		V6:                   v6,
		SourceCheck:          w.BindingSourceCheck,
		Backend:              b,
		Log:                  log,
	}, keys)
	if err != nil {
		return nil, err
	}
	if err := b.AddEventHandler(ctx, &tinkerbell.Workflow{}, m.WorkflowHandler()); err != nil {
		m.Close()
		return nil, fmt.Errorf("WireGuard: %w", err)
	}
	kv := []any{"mode", mode, "agentAuthenticationPolicy", w.AuthenticationPolicy, "bootstrapPublicKey", keys.Bootstrap.PublicKey().String(), "prefix", keys.Prefix}
	if bs, ok := m.Bootstrap(false); ok {
		kv = append(kv, "bootstrapEndpoint", bs.Endpoint, "bindAddr", v4.BindAddr)
	}
	if bs, ok := m.Bootstrap(true); ok {
		kv = append(kv, "bootstrapEndpointV6", bs.Endpoint, "bindAddrV6", v6.BindAddr)
	}
	log.Info("WireGuard Agent authentication enabled", kv...)
	return m, nil
}

// wireGuardKernelParams are the public values an Agent booted in one address
// family needs to reach the bootstrap device. None of them are secrets. They are
// tinkerbell_<flag> parameters, which CaptainOS passes to the Agent as the named
// flag. It returns nothing when that family isn't served.
func wireGuardKernelParams(m *tunnel.Manager, v6 bool) []string {
	bs, ok := m.Bootstrap(v6)
	if !ok {
		return nil
	}
	return []string{
		"tinkerbell_wireguard_server_public_key=" + bs.PublicKey.String(),
		"tinkerbell_wireguard_server_endpoint=" + bs.Endpoint.String(),
		"tinkerbell_wireguard_prefix=" + bs.Prefix.String(),
	}
}

// defaultWireGuardGeneratedDir is where unset keys and the prefix are generated.
func defaultWireGuardGeneratedDir() string {
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "tinkerbell", "wireguard")
	}
	return filepath.Join(os.TempDir(), "tinkerbell", "wireguard")
}

// generateMissing points every key file and the prefix that weren't set at a
// file in w.GeneratedDir, generating the file if it doesn't exist yet. Reusing
// the files keeps the keys stable across restarts, so Agents booted earlier can
// still reach the server.
func generateMissing(log logr.Logger, w *flag.WireGuardConfig) error {
	for _, f := range []struct {
		path *string
		name string
		gen  func() (string, error)
	}{
		{&w.BootstrapKeyFile, "bootstrap.key", generatePrivateKey},
		{&w.SessionKeyFile, "session.key", generatePrivateKey},
		{&w.PSKRootFile, "psk-root", generateRandomKey},
	} {
		if *f.path != "" {
			continue
		}
		p := filepath.Join(w.GeneratedDir, f.name)
		created, err := writeIfMissing(p, f.gen)
		if err != nil {
			return fmt.Errorf("WireGuard: generating %s: %w", p, err)
		}
		*f.path = p
		log.Info("WireGuard key not set, using a generated one", "file", p, "generatedNow", created)
	}
	if w.Prefix == "" {
		p := filepath.Join(w.GeneratedDir, "prefix")
		created, err := writeIfMissing(p, func() (string, error) {
			pr, err := wireguard.GeneratePrefix()
			return pr.String(), err
		})
		if err != nil {
			return fmt.Errorf("WireGuard: generating %s: %w", p, err)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return fmt.Errorf("WireGuard: reading %s: %w", p, err)
		}
		w.Prefix = strings.TrimSpace(string(b))
		log.Info("WireGuard prefix not set, using a generated one", "file", p, "prefix", w.Prefix, "generatedNow", created)
	}
	return nil
}

func generatePrivateKey() (string, error) {
	k, err := wireguard.GeneratePrivateKey()
	return k.String(), err
}

func generateRandomKey() (string, error) {
	k, err := wireguard.GenerateKey()
	return k.String(), err
}

// writeIfMissing writes gen's value to path with mode 0600 unless path exists.
// created reports whether it wrote the file.
func writeIfMissing(path string, gen func() (string, error)) (created bool, err error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, err
	}
	v, err := gen()
	if err != nil {
		return false, err
	}
	// O_EXCL so two processes starting together can't each write a different value.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	if _, err := f.WriteString(v + "\n"); err != nil {
		return false, err
	}
	return true, nil
}
