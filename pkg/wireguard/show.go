package wireguard

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/tailscale/wireguard-go/device"
)

// Show device and peer connection info.
func Show(ctx context.Context, d *device.Device) (string, error) {
	cfg, err := d.IpcGet()
	if err == nil {
		iface, err := parseConfig(cfg)
		if err != nil {
			return "", fmt.Errorf("error parsing config: %w", err)
		}

		return output(iface), nil
	}

	return "", err
}

func output(devBlock *Device) string {
	if devBlock == nil {
		return ""
	}
	var buf bytes.Buffer

	// Interface section
	buf.WriteString(fmt.Sprintf("interface: %s\n", devBlock.Name))
	buf.WriteString(fmt.Sprintf("  public key: %s\n", devBlock.PublicKey.String()))
	buf.WriteString(fmt.Sprintf("  listening port: %d\n", *devBlock.ListenPort))

	// Peers section
	for _, peer := range devBlock.Peers {
		buf.WriteString(fmt.Sprintf("\npeer: %s\n", peer.PublicKey.String()))
		buf.WriteString(fmt.Sprintf("  endpoint: %s\n", peer.Endpoint.String()))

		a := func() string {
			converted := make([]string, len(peer.AllowedIPs))
			for idx, elem := range peer.AllowedIPs {
				converted[idx] = elem.String()
			}
			return strings.Join(converted, ", ")
		}()

		buf.WriteString(fmt.Sprintf("  allowed ips: %s\n", a))

		duration := time.Since(peer.LastHandshakeTime)
		buf.WriteString(fmt.Sprintf("  latest handshake: %s\n", formatDuration(duration)))
		buf.WriteString(fmt.Sprintf("  transfer: %s received, %s sent\n", formatBytes(peer.ReceiveBytes), formatBytes(peer.TransmitBytes)))
		buf.WriteString("  persistent keepalive: off\n")
	}

	return buf.String()
}

func parseConfig(input string) (*Device, error) {
	lines := strings.Split(input, "\n")

	iface := &Device{
		Name:  "wg0",
		Peers: []Peer{},
	} // Initialize device block with default name
	currentPeer := Peer{}
	isParsingPeer := false

	for _, line := range lines {
		if len(line) == 0 {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid config line: %s", line)
		}

		key, value := parts[0], parts[1]

		switch key {
		case "private_key":
			// private_key is used to calculate the public key.
			// It is not stored nor displayed in show output.
			privKey, err := hex.DecodeString(value)
			if err != nil {
				return nil, fmt.Errorf("failed to decode hex key: %w", err)
			}
			pk := toPtr(Key(privKey))
			iface.PublicKey = pk.PublicKey()
		case "listen_port":
			port, err := strconv.Atoi(value)
			if err != nil {
				return nil, fmt.Errorf("invalid listen port: %v", err)
			}
			iface.ListenPort = toPtr(port)
		case "endpoint":
			if isParsingPeer {
				v, err := netip.ParseAddrPort(value)
				if err != nil {
					return nil, fmt.Errorf("failed to parse Addr Port: %w", err)
				}
				currentPeer.Endpoint = net.UDPAddrFromAddrPort(v)
			}
		case "last_handshake_time_sec":
			sec, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid last handshake time: %v", err)
			}
			if isParsingPeer {
				currentPeer.LastHandshakeTime = time.Unix(sec, 0)
			}
		case "tx_bytes":
			tx, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid tx bytes: %v", err)
			}
			if isParsingPeer {
				currentPeer.TransmitBytes = tx
			}
		case "rx_bytes":
			rx, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid rx bytes: %v", err)
			}
			if isParsingPeer {
				currentPeer.ReceiveBytes = rx
			}
		case "allowed_ip":
			if isParsingPeer {
				v, err := netip.ParsePrefix(value)
				if err != nil {
					return nil, fmt.Errorf("failed parse prefix: %w", err)
				}
				currentPeer.AllowedIPs = append(currentPeer.AllowedIPs, v)
			}
		case "public_key":
			isParsingPeer = true
			if !currentPeer.PublicKey.IsZero() {
				// Save the current peer before starting a new one
				iface.Peers = append(iface.Peers, currentPeer)
				currentPeer = Peer{}
			}
			pubKey, err := hex.DecodeString(value)
			if err != nil {
				return nil, fmt.Errorf("failed to decode hex key: %w", err)
			}
			currentPeer.PublicKey = Key(pubKey)
		}
	}

	// Add the last peer if present
	if !currentPeer.PublicKey.IsZero() {
		iface.Peers = append(iface.Peers, currentPeer)
	}

	return iface, nil
}

func formatDuration(d time.Duration) string {
	mins := int(d.Minutes())
	secs := int(d.Seconds()) % 60
	if mins > 0 {
		return fmt.Sprintf("%d minute%s, %d second%s ago",
			mins, plural(mins),
			secs, plural(secs))
	}
	return fmt.Sprintf("%d second%s ago", secs, plural(secs))
}

func plural(n int) string {
	if n != 1 {
		return "s"
	}
	return ""
}

func formatBytes(bytes int64) string {
	const KB = 1024.0
	const MB = KB * 1024.0

	if bytes >= MB {
		return fmt.Sprintf("%.2f MiB", float64(bytes)/MB)
	}
	return fmt.Sprintf("%.2f KiB", float64(bytes)/KB)
}
