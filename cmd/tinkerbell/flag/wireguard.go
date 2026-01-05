package flag

import (
	"context"
	"net"
	"net/netip"
	"os"

	"github.com/go-logr/logr"
	"github.com/peterbourgon/ff/v4/ffval"
	"github.com/tinkerbell/tinkerbell/pkg/backend/kube"
	"github.com/tinkerbell/tinkerbell/pkg/wireguard"
)

type WireguardConfig struct {
	LogLevel int // The log level for the WireGuard tunnel logging.
	BindPort int // The port to use for binding to the WireGuard tunnel endpoint.
	// PrivateKeyPath is a file path to a base64 encode WireGuard private key.
	// The wg command can be used to generate one.
	PrivateKeyPath string
}

var KubeIndexesWireguard = map[kube.IndexType]kube.Index{}

func (w WireguardConfig) NewTunnelListener(ctx context.Context, log logr.Logger) (net.Listener, error) {
	wgDeviceAddr := ""
	topts := []wireguard.NetworkTunnelOption{
		wireguard.WithTunnelLocalAddresses([]netip.Addr{netip.MustParseAddr(wgDeviceAddr)}),
		wireguard.WithTunnelDNSAddrs([]netip.Addr{netip.MustParseAddr("1.1.1.1")}),
	}
	tunNet, err := wireguard.NewTunnel(ctx, log, topts...)
	if err != nil {
		panic(err)
	}

	var fn wireguard.PeerLookupFunc
	devOpts := []wireguard.DeviceOption{
		wireguard.WithDeviceListenPort(51820),
		wireguard.WithDeviceAddress(netip.MustParseAddr(wgDeviceAddr)),
		wireguard.WithDevicePeerLookupFunc(fn),
	}
	pk, err := os.ReadFile(w.PrivateKeyPath)
	if err != nil {
		return nil, err
	}
	serverPrivateKey, err := wireguard.NewKey(pk)
	if err != nil {
		panic(err)
	}
	dev := wireguard.NewDevice(serverPrivateKey, devOpts...)

	tunDev, err := tunNet.AddDevice(ctx, log, *dev)
	if err != nil {
		panic(err)
	}

	// Bring the tunnel device up
	if err := tunDev.Up(); err != nil {
		panic(err)
	}
	return nil, nil
}

func RegisterWireguardFlags(fs *Set, w *WireguardConfig) {
	fs.Register(WireguardLogLevel, ffval.NewValueDefault(&w.LogLevel, w.LogLevel))
	fs.Register(WireguardBindPort, ffval.NewValueDefault(&w.BindPort, w.BindPort))
	fs.Register(WireguardPrivateKeyPath, ffval.NewValueDefault(&w.PrivateKeyPath, w.PrivateKeyPath))
}

var WireguardLogLevel = Config{
	Name:  "wireguard-log-level",
	Usage: "the higher the number the more verbose, level 0 inherits the global log level",
}

var WireguardBindPort = Config{
	Name:  "wireguard-bind-port",
	Usage: "port on which the WireGuard endpoint will listen",
}

var WireguardPrivateKeyPath = Config{
	Name:  "wireguard-private-key-path",
	Usage: "path to the WireGuard private key",
}
