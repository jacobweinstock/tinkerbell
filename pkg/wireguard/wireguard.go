// Package wireguard implements running a userspace WireGuard device.
//
// This package requires Tailscale's [wireguard-go] fork and must use the tailscale branch.
//
// Generally, the order of operations are:
//  1. Create a network Tunnel.
//  2. Add a device to the Tunnel. Optionally, add peers to the device.
//  3. Bring up the Tunnel device
//  4. Start a client (net.Conn) or a server (net.Listener) using the Tunnel.
//  5. Bring down the Tunnel device.
//  6. Close the Tunnel device.
//
// Contrived example HTTP server:
//
//	 func main() {
//	     tunNet, err := wireguard.NewTunnel(ctx, log, topts...)
//	     if err != nil {
//	     	panic(err)
//	     }
//	     dev := wireguard.NewDevice(serverPrivateKey, devOpts...)
//	     tunDev, err := tunNet.AddDevice(ctx, log, *dev)
//	     if err := tunDev.Up(); err != nil {
//	     	panic(err)
//	     }
//	     listener, err := tunNet.ListenTCP(&net.TCPAddr{Port: 80})
//	     if err != nil {
//	     	panic(err)
//	     }
//	     go func() {
//	         <-ctx.Done()
//			    tunDev.Down()
//			    tunDev.Close()
//	     }()
//	     http.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
//	     	log.Printf("> %s - %s - %s", request.RemoteAddr, request.URL.String(), request.UserAgent())
//	     	io.WriteString(writer, "Hello from userspace TCP!")
//	     })
//	     if err := http.Serve(listener, nil); err != nil {
//	     	panic(err)
//	     }
//	     <-tunDev.Wait()
//	 }
//
// [wireguard-go]: https://github.com/tailscale/wireguard-go
package wireguard

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/netip"
	"strings"

	"github.com/go-logr/logr"
	"github.com/tailscale/wireguard-go/conn"
	"github.com/tailscale/wireguard-go/device"
	"github.com/tailscale/wireguard-go/tun"
	"github.com/tailscale/wireguard-go/tun/netstack"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
)

// NetworkTunnel
type NetworkTunnel struct {
	DNSAddrs       []netip.Addr
	LocalAddresses []netip.Addr
	MTU            int
	tunDevice      tun.Device

	*netstack.Net
}

type NetworkDevice struct {
	*device.Device
}

// NetworkTunnelOption
type NetworkTunnelOption func(*NetworkTunnel)

func WithTunnelDNSAddrs(d []netip.Addr) NetworkTunnelOption {
	return func(nt *NetworkTunnel) {
		nt.DNSAddrs = d
	}
}

func WithTunnelMTU(m int) NetworkTunnelOption {
	return func(nt *NetworkTunnel) {
		nt.MTU = m
	}
}

func WithTunnelLocalAddresses(a []netip.Addr) NetworkTunnelOption {
	return func(nt *NetworkTunnel) {
		nt.LocalAddresses = a
	}
}

// NewTunnel
func NewTunnel(ctx context.Context, log logr.Logger, opts ...NetworkTunnelOption) (*NetworkTunnel, error) {
	nt := &NetworkTunnel{
		DNSAddrs: []netip.Addr{},
		MTU:      device.DefaultMTU,
	}

	for _, opt := range opts {
		opt(nt)
	}

	if nt.MTU == 0 {
		nt.MTU = device.DefaultMTU
	}

	tdev, tnet, err := netstack.CreateNetTUN(nt.LocalAddresses, nt.DNSAddrs, nt.MTU)
	if err != nil {
		return nil, fmt.Errorf("failed to create WireGuard network tunnel: %w", err)
	}

	nt.tunDevice = tdev
	nt.Net = tnet

	return nt, nil
}

func (nt *NetworkTunnel) AddDevice(ctx context.Context, log logr.Logger, cfg Device) (*NetworkDevice, error) {
	quiet := logger(log)
	if nt == nil {
		return nil, errors.New("network tunnel is nil")
	}
	tunDev := device.NewDevice(nt.tunDevice, conn.NewDefaultBind(), quiet)

	if cfg.PeerLookupFunc != nil {
		tunDev.SetPeerLookupFunc(device.PeerLookupFunc(cfg.PeerLookupFunc))
	}

	if err := tunDev.IpcSet(configProtocol(cfg.Text())); err != nil {
		return nil, err
	}

	return &NetworkDevice{tunDev}, nil
}

func (nt *NetworkTunnel) TCPListener(port uint16) (*gonet.TCPListener, error) {
	return nt.ListenTCPAddrPort(netip.AddrPortFrom(nt.LocalAddresses[0], port))
}

func logger(l logr.Logger) *device.Logger {
	log := logr.Discard()

	if l.GetV() >= 1 {
		log = l
	}
	return &device.Logger{
		Verbosef: func(format string, args ...any) {
			log.Info(fmt.Sprintf(format, args...))
		},
		Errorf: func(format string, args ...any) {
			log.Info(fmt.Sprintf(format, args...))
		},
	}
}

func base64ToHex(base64Key string) string {
	decodedKey, err := base64.StdEncoding.DecodeString(base64Key)
	if err != nil {
		log.Panic("Failed to decode base64 key:", err)
	}

	hexKey := hex.EncodeToString(decodedKey)
	return hexKey
}

func configProtocol(lines []string) string {
	var ins strings.Builder
	for _, l := range lines {
		ins.WriteString(l)
		ins.WriteString("\n")
	}
	return ins.String()
}

func toPtr[T any](v T) *T {
	return &v
}
