package flag

import (
	"net/netip"
	neturl "net/url"
	"testing"

	"github.com/tinkerbell/tinkerbell/smee"
)

// TestBootstrapURLDefaults is the regression test for the URL having been
// mandatory: --bootstrap-enabled alone must yield a usable URL, because an
// empty one silently omits the extra initrd from the boot script.
func TestBootstrapURLDefaults(t *testing.T) {
	tests := map[string]struct {
		publicIP   netip.Addr
		publicIPv6 netip.Addr
		builder    URLBuilder
		builderV6  URLBuilder
		scheme     string
		want       string
		wantV6     string
	}{
		"derived from public addresses": {
			publicIP:   netip.MustParseAddr("10.0.2.15"),
			publicIPv6: netip.MustParseAddr("2001:db8::15"),
			want:       "http://10.0.2.15:7080/bootstrap/",
			wantV6:     "http://[2001:db8::15]:7080/bootstrap/",
		},
		"ipv4 only leaves ipv6 empty": {
			publicIP: netip.MustParseAddr("10.0.2.15"),
			want:     "http://10.0.2.15:7080/bootstrap/",
			wantV6:   "",
		},
		"host override wins over the public address": {
			publicIP: netip.MustParseAddr("10.0.2.15"),
			builder:  URLBuilder{Host: "boot.example.com"},
			want:     "http://boot.example.com:7080/bootstrap/",
		},
		"port override wins over the http port": {
			publicIP: netip.MustParseAddr("10.0.2.15"),
			builder:  URLBuilder{Port: 8443},
			want:     "http://10.0.2.15:8443/bootstrap/",
		},
		"host and port override": {
			publicIP: netip.MustParseAddr("10.0.2.15"),
			builder:  URLBuilder{Host: "boot.example.com", Port: 8443},
			want:     "http://boot.example.com:8443/bootstrap/",
		},
		"scheme override for TLS termination": {
			publicIP: netip.MustParseAddr("10.0.2.15"),
			builder:  URLBuilder{Host: "boot.example.com", Port: 7443},
			scheme:   "https",
			want:     "https://boot.example.com:7443/bootstrap/",
		},
		"no advertisable address yields nothing": {
			want:   "",
			wantV6: "",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			cfg := &SmeeConfig{Config: smee.NewConfig(smee.Config{})}
			cfg.Config.Bootstrap.Enabled = true
			cfg.Bootstrap = tt.builder
			cfg.BootstrapV6 = tt.builderV6
			if tt.scheme != "" {
				cfg.Config.Bootstrap.BaseURL.Scheme = tt.scheme
				cfg.Config.Bootstrap.BaseURLV6.Scheme = tt.scheme
			}

			cfg.Convert(tt.publicIP, tt.publicIPv6, netip.Addr{}, netip.Addr{}, 7080, 7080)

			if got := bootstrapURLString(cfg.Config.Bootstrap.BaseURL); got != tt.want {
				t.Errorf("BaseURL = %q, want %q", got, tt.want)
			}
			if got := bootstrapURLString(cfg.Config.Bootstrap.BaseURLV6); got != tt.wantV6 {
				t.Errorf("BaseURLV6 = %q, want %q", got, tt.wantV6)
			}
		})
	}
}

// TestBootstrapPathIsNotConfigurable pins the path to the route Smee serves.
func TestBootstrapPathIsNotConfigurable(t *testing.T) {
	cfg := &SmeeConfig{Config: smee.NewConfig(smee.Config{})}
	cfg.Config.Bootstrap.Enabled = true
	cfg.Convert(netip.MustParseAddr("10.0.2.15"), netip.Addr{}, netip.Addr{}, netip.Addr{}, 7080, 7080)

	if got, want := cfg.Config.Bootstrap.BaseURL.Path, smee.BootstrapURI; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
}

func bootstrapURLString(u *neturl.URL) string {
	if u == nil || u.Host == "" {
		return ""
	}
	return u.String()
}
