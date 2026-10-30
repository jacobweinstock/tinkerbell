package flag

import (
	"fmt"
	"net/netip"
	"strings"

	"github.com/peterbourgon/ff/v4/ffval"
	"github.com/tinkerbell/tinkerbell/pkg/constant"
	ntip "github.com/tinkerbell/tinkerbell/pkg/flag/netip"
	"github.com/tinkerbell/tinkerbell/tink/server/tunnel"
)

// WireGuardConfig configures WireGuard authentication of Tink Agents.
type WireGuardConfig struct {
	Mode                 string
	AuthenticationPolicy string
	// BindAddr and BindAddrV6 are the per-family listen addresses. A family
	// with none falls back to the global bind address.
	BindAddr   netip.Addr
	BindAddrV6 netip.Addr
	// The per-family UDP ports of the bootstrap and session devices.
	BootstrapPort    uint16
	BootstrapPortV6  uint16
	SessionPort      uint16
	SessionPortV6    uint16
	Prefix           string
	BootstrapKeyFile string
	SessionKeyFile   string
	PSKRootFile      string
	// PreviousBootstrapKeyFile and the previous bootstrap ports, set together,
	// keep the bootstrap key being rotated out serving redirects to the current one.
	PreviousBootstrapKeyFile string
	PreviousBootstrapPort    uint16
	PreviousBootstrapPortV6  uint16
	BindingSourceCheck       bool
	LogLevel                 int
	// GeneratedDir is where keys and the prefix that weren't specified are
	// generated, and found again after a restart.
	GeneratedDir string
}

// Validate checks the WireGuard flags of the families being served. Keys and
// the prefix are optional: any that aren't set are generated in GeneratedDir.
// With WireGuard disabled nothing is checked.
func (w *WireGuardConfig) Validate(families constant.ListenerFamilies) error {
	mode, err := tunnel.ParseMode(w.Mode)
	if err != nil {
		return err
	}
	if mode == tunnel.ModeDisabled {
		return nil
	}
	policy, err := tunnel.ParseAuthenticationPolicy(w.AuthenticationPolicy)
	if err != nil {
		return err
	}
	if policy == tunnel.AuthenticationPolicyPinned && mode != tunnel.ModeRequired {
		// The plain listener of optional mode would let Agents without a pinned credential in.
		return fmt.Errorf("--%s=%s requires --%s=%s", AgentAuthenticationPolicy.Name, policy, WireGuardMode.Name, tunnel.ModeRequired)
	}
	for _, f := range []struct {
		served                       bool
		fam                          Family
		bootstrap, session, previous uint16
	}{
		{families.HasV4(), V4, w.BootstrapPort, w.SessionPort, w.PreviousBootstrapPort},
		{families.HasV6(), V6, w.BootstrapPortV6, w.SessionPortV6, w.PreviousBootstrapPortV6},
	} {
		if !f.served {
			continue
		}
		name := func(c Config) string { return "--" + c.Name + "-" + string(f.fam) }
		var missing []string
		if f.bootstrap == 0 {
			missing = append(missing, name(WireGuardBootstrapPort))
		}
		if f.session == 0 {
			missing = append(missing, name(WireGuardSessionPort))
		}
		if len(missing) > 0 {
			return fmt.Errorf("--%s=%s requires %s", WireGuardMode.Name, mode, strings.Join(missing, ", "))
		}
		if f.bootstrap == f.session {
			return fmt.Errorf("%s and %s must differ", name(WireGuardBootstrapPort), name(WireGuardSessionPort))
		}
		if (w.PreviousBootstrapKeyFile == "") != (f.previous == 0) {
			return fmt.Errorf("--%s and %s must be set together", WireGuardPreviousBootstrapKeyFile.Name, name(WireGuardPreviousBootstrapPort))
		}
		if f.previous != 0 && (f.previous == f.bootstrap || f.previous == f.session) {
			return fmt.Errorf("%s must differ from the bootstrap and session ports", name(WireGuardPreviousBootstrapPort))
		}
	}
	return nil
}

// Convert returns how each family is served. A family with no WireGuard
// specific address falls back to the global bind address, and devices are
// advertised at the global public address of their family.
func (w *WireGuardConfig) Convert(families constant.ListenerFamilies, bindAddr, bindAddrV6, publicIP, publicIPv6 netip.Addr) (v4, v6 tunnel.Family) {
	v4 = tunnel.Family{
		Enabled:               families.HasV4(),
		BindAddr:              firstValidAddr(w.BindAddr, bindAddr),
		PublicAddr:            publicIP,
		BootstrapPort:         w.BootstrapPort,
		SessionPort:           w.SessionPort,
		PreviousBootstrapPort: w.PreviousBootstrapPort,
	}
	v6 = tunnel.Family{
		Enabled:               families.HasV6(),
		BindAddr:              firstValidAddr(w.BindAddrV6, bindAddrV6),
		PublicAddr:            publicIPv6,
		BootstrapPort:         w.BootstrapPortV6,
		SessionPort:           w.SessionPortV6,
		PreviousBootstrapPort: w.PreviousBootstrapPortV6,
	}
	return v4, v6
}

// RegisterWireGuardFlags registers the WireGuard flags.
func RegisterWireGuardFlags(fs *Set, w *WireGuardConfig) {
	fs.Register(WireGuardMode, ffval.NewValueDefault(&w.Mode, w.Mode))
	fs.Register(AgentAuthenticationPolicy, ffval.NewValueDefault(&w.AuthenticationPolicy, w.AuthenticationPolicy))
	fs.RegisterFamily(WireGuardBindAddr, V4, &ntip.Addr{Addr: &w.BindAddr})
	fs.RegisterFamily(WireGuardBindAddr, V6, &ntip.Addr{Addr: &w.BindAddrV6})
	fs.RegisterFamily(WireGuardBootstrapPort, V4, ffval.NewValueDefault(&w.BootstrapPort, w.BootstrapPort))
	fs.RegisterFamily(WireGuardBootstrapPort, V6, ffval.NewValueDefault(&w.BootstrapPortV6, w.BootstrapPortV6))
	fs.RegisterFamily(WireGuardSessionPort, V4, ffval.NewValueDefault(&w.SessionPort, w.SessionPort))
	fs.RegisterFamily(WireGuardSessionPort, V6, ffval.NewValueDefault(&w.SessionPortV6, w.SessionPortV6))
	fs.Register(WireGuardPrefix, ffval.NewValueDefault(&w.Prefix, w.Prefix))
	fs.Register(WireGuardBootstrapKeyFile, ffval.NewValueDefault(&w.BootstrapKeyFile, w.BootstrapKeyFile))
	fs.Register(WireGuardSessionKeyFile, ffval.NewValueDefault(&w.SessionKeyFile, w.SessionKeyFile))
	fs.Register(WireGuardPSKRootFile, ffval.NewValueDefault(&w.PSKRootFile, w.PSKRootFile))
	fs.Register(WireGuardPreviousBootstrapKeyFile, ffval.NewValueDefault(&w.PreviousBootstrapKeyFile, w.PreviousBootstrapKeyFile))
	fs.RegisterFamily(WireGuardPreviousBootstrapPort, V4, ffval.NewValueDefault(&w.PreviousBootstrapPort, w.PreviousBootstrapPort))
	fs.RegisterFamily(WireGuardPreviousBootstrapPort, V6, ffval.NewValueDefault(&w.PreviousBootstrapPortV6, w.PreviousBootstrapPortV6))
	fs.Register(WireGuardGeneratedDir, ffval.NewValueDefault(&w.GeneratedDir, w.GeneratedDir))
	fs.Register(WireGuardBindingSourceCheck, ffval.NewValueDefault(&w.BindingSourceCheck, w.BindingSourceCheck))
	fs.Register(WireGuardLogLevel, ffval.NewValueDefault(&w.LogLevel, w.LogLevel))
}

var AgentAuthenticationPolicy = Config{
	Name: "agent-authentication-policy",
	Usage: fmt.Sprintf("Agent credential trust accepted, one of [%s, %s]; %s requires --wireguard-mode=%s",
		tunnel.AuthenticationPolicyFirstUse, tunnel.AuthenticationPolicyPinned, tunnel.AuthenticationPolicyPinned, tunnel.ModeRequired),
}

var WireGuardBindAddr = Config{
	Name:  "wireguard-bind-addr",
	Usage: "address on which the WireGuard devices listen; defaults to the global bind address",
}

var WireGuardMode = Config{
	Name:  "wireguard-mode",
	Usage: fmt.Sprintf("WireGuard authentication of Tink Agents, one of [%s, %s, %s]", tunnel.ModeDisabled, tunnel.ModeOptional, tunnel.ModeRequired),
}

var WireGuardBootstrapPort = Config{
	Name:  "wireguard-bootstrap-port",
	Usage: "UDP port of the bootstrap WireGuard device",
}

var WireGuardSessionPort = Config{
	Name:  "wireguard-session-port",
	Usage: "UDP port of the session WireGuard device",
}

var WireGuardPrefix = Config{
	Name:  "wireguard-prefix",
	Usage: "IPv6 /48 used inside the WireGuard tunnels only, e.g. a random fdXX:XXXX:XXXX::/48; all replicas must use the same one; generated if unset",
}

var WireGuardBootstrapKeyFile = Config{
	Name:  "wireguard-bootstrap-key-file",
	Usage: "file holding the bootstrap device's base64 private key (wg genkey); generated if unset",
}

var WireGuardSessionKeyFile = Config{
	Name:  "wireguard-session-key-file",
	Usage: "file holding the session device's base64 private key (wg genkey); must differ from the bootstrap key; generated if unset",
}

var WireGuardPSKRootFile = Config{
	Name:  "wireguard-psk-root-file",
	Usage: "file holding 32 random bytes, base64 (openssl rand -base64 32), from which session pre-shared keys are derived; generated if unset",
}

var WireGuardGeneratedDir = Config{
	Name:  "wireguard-generated-dir",
	Usage: "directory where keys and the prefix that aren't set are generated, and reused after a restart; replicas that generate their own won't agree, so set the keys explicitly for more than one replica",
}

var WireGuardPreviousBootstrapKeyFile = Config{
	Name:  "wireguard-previous-bootstrap-key-file",
	Usage: "during a bootstrap key rotation, the old bootstrap private key; its device only redirects Agents booted with it to the current key",
}

var WireGuardPreviousBootstrapPort = Config{
	Name:  "wireguard-previous-bootstrap-port",
	Usage: "during a bootstrap key rotation, the UDP port Agents booted with the old key use: the bootstrap port before the rotation; set together with the previous bootstrap key file",
}

var WireGuardBindingSourceCheck = Config{
	Name:  "wireguard-binding-source-check",
	Usage: "bind an Agent's key only when its UDP source IP is one of its Hardware's DHCP addresses",
}

var WireGuardLogLevel = Config{
	Name:  "wireguard-log-level",
	Usage: logLevelUsage,
}
