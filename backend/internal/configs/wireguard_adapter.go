package configs

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"github.com/ikaevus/routegate/backend/internal/platform"
	wgcredentials "github.com/ikaevus/routegate/backend/internal/wireguard"
)

type wireGuardAdapter struct{}

var _ vpnCoreAdapter = wireGuardAdapter{}

func (wireGuardAdapter) Descriptor() platform.VPNCoreAdapterDescriptor {
	return platform.VPNCoreAdapterDescriptor{
		Core:          platform.VPNCoreWireGuard,
		Protocol:      platform.VPNProtocolWireGuard,
		Transports:    []string{platform.VPNTransportUDP},
		SecurityModes: []string{platform.VPNSecurityWireGuard},
	}
}

func (wireGuardAdapter) Render(config *RenderedConfig, info ServerConfigInfo) {
	address := strings.TrimSpace(info.WireGuardAddress)
	if address == "" {
		address = wgcredentials.DefaultServerAddress
	}
	port := info.WireGuardPort
	if port == 0 {
		port = wgcredentials.DefaultListenPort
	}
	prefix, _ := netip.ParsePrefix(address)
	network := prefix.Masked().String()

	var rendered strings.Builder
	rendered.WriteString("[Interface]\n")
	fmt.Fprintf(&rendered, "PrivateKey = %s\n", strings.TrimSpace(info.WireGuardPrivateKey))
	fmt.Fprintf(&rendered, "Address = %s\n", address)
	fmt.Fprintf(&rendered, "ListenPort = %d\n", port)
	rendered.WriteString("SaveConfig = false\n")
	fmt.Fprintf(&rendered, "PostUp = %s\n", wireGuardPostUp(network))
	fmt.Fprintf(&rendered, "PostDown = %s\n", wireGuardPostDown(network))

	for _, account := range info.VPNAccounts {
		if !isWireGuardAccountRenderable(account) {
			continue
		}
		peerAddress := wireGuardPeerAddress(account.WireGuardAddress)
		config.VPNAccounts = append(config.VPNAccounts, ConfigVPNAccount{
			ID:                 account.ID,
			DisplayName:        accountDisplayName(account),
			Status:             account.Status,
			WireGuardPublicKey: strings.TrimSpace(account.WireGuardPublicKey),
			WireGuardAddress:   peerAddress,
		})
		rendered.WriteString("\n[Peer]\n")
		fmt.Fprintf(&rendered, "# %s\n", safeWireGuardComment(accountDisplayName(account)))
		fmt.Fprintf(&rendered, "PublicKey = %s\n", strings.TrimSpace(account.WireGuardPublicKey))
		fmt.Fprintf(&rendered, "AllowedIPs = %s/32\n", peerAddress)
	}
	config.WireGuard = rendered.String()
}

func (wireGuardAdapter) Validate(config RenderedConfig, result *ValidationResult) {
	parsed, err := parseWireGuardServerConfig(config.WireGuard)
	if err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, err.Error())
		return
	}
	if len(parsed.Peers) == 0 {
		result.Warnings = append(result.Warnings, "No active WireGuard accounts are available; this config has no peers and cannot be applied.")
	}
}

func (wireGuardAdapter) Ready(config RenderedConfig) bool {
	parsed, err := parseWireGuardServerConfig(config.WireGuard)
	return err == nil && len(parsed.Peers) > 0
}

func isWireGuardAccountRenderable(account VPNAccountConfigInfo) bool {
	if account.Status != "active" || wireGuardPeerAddress(account.WireGuardAddress) == "" {
		return false
	}
	if err := wgcredentials.ValidateKey(account.WireGuardPublicKey); err != nil {
		return false
	}
	return account.TrafficEnforcementStatus != "over_limit"
}

func wireGuardPeerAddress(value string) string {
	value = strings.TrimSpace(value)
	if address, err := netip.ParseAddr(value); err == nil && address.Is4() { return address.String() }
	if prefix, err := netip.ParsePrefix(value); err == nil && prefix.Addr().Is4() { return prefix.Addr().String() }
	return ""
}

func safeWireGuardComment(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return strings.TrimSpace(value)
}

func wireGuardPostUp(network string) string {
	return "iptables -A FORWARD -i %i -j ACCEPT; iptables -A FORWARD -o %i -j ACCEPT; iptables -t nat -A POSTROUTING -s " + network + " -j MASQUERADE"
}

func wireGuardPostDown(network string) string {
	return "iptables -D FORWARD -i %i -j ACCEPT; iptables -D FORWARD -o %i -j ACCEPT; iptables -t nat -D POSTROUTING -s " + network + " -j MASQUERADE"
}

type parsedWireGuardConfig struct {
	Address string
	Port    int
	Peers   []string
}

func parseWireGuardServerConfig(payload string) (parsedWireGuardConfig, error) {
	lines := strings.Split(strings.TrimSpace(payload), "\n")
	section := ""
	interfaceValues := map[string]string{}
	peerValues := map[string]string{}
	parsed := parsedWireGuardConfig{}
	flushPeer := func() error {
		if len(peerValues) == 0 {
			return nil
		}
		if err := wgcredentials.ValidateKey(peerValues["PublicKey"]); err != nil {
			return fmt.Errorf("WireGuard peer public key is invalid: %w", err)
		}
		address, err := netip.ParsePrefix(peerValues["AllowedIPs"])
		if err != nil || !address.Addr().Is4() || address.Bits() != 32 {
			return fmt.Errorf("WireGuard peer AllowedIPs must contain one IPv4 /32 address")
		}
		parsed.Peers = append(parsed.Peers, address.String())
		peerValues = map[string]string{}
		return nil
	}

	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == "[Interface]" || line == "[Peer]" {
			if line == "[Peer]" {
				if err := flushPeer(); err != nil {
					return parsedWireGuardConfig{}, err
				}
			}
			section = strings.Trim(line, "[]")
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || section == "" {
			return parsedWireGuardConfig{}, fmt.Errorf("WireGuard config contains an invalid line")
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if section == "Interface" {
			if _, allowed := map[string]bool{"PrivateKey": true, "Address": true, "ListenPort": true, "SaveConfig": true, "PostUp": true, "PostDown": true}[key]; !allowed {
				return parsedWireGuardConfig{}, fmt.Errorf("WireGuard Interface field %q is not allowed", key)
			}
			interfaceValues[key] = value
		} else {
			if key != "PublicKey" && key != "AllowedIPs" {
				return parsedWireGuardConfig{}, fmt.Errorf("WireGuard Peer field %q is not allowed", key)
			}
			peerValues[key] = value
		}
	}
	if err := flushPeer(); err != nil {
		return parsedWireGuardConfig{}, err
	}
	if err := wgcredentials.ValidateKey(interfaceValues["PrivateKey"]); err != nil {
		return parsedWireGuardConfig{}, fmt.Errorf("WireGuard server private key is invalid: %w", err)
	}
	prefix, err := netip.ParsePrefix(interfaceValues["Address"])
	if err != nil || !prefix.Addr().Is4() || prefix.Bits() > 30 {
		return parsedWireGuardConfig{}, fmt.Errorf("WireGuard Interface Address must be a usable IPv4 prefix")
	}
	port, err := strconv.Atoi(interfaceValues["ListenPort"])
	if err != nil || port < 1 || port > 65535 {
		return parsedWireGuardConfig{}, fmt.Errorf("WireGuard ListenPort must be between 1 and 65535")
	}
	if interfaceValues["SaveConfig"] != "false" {
		return parsedWireGuardConfig{}, fmt.Errorf("WireGuard SaveConfig must be false")
	}
	network := prefix.Masked().String()
	if interfaceValues["PostUp"] != wireGuardPostUp(network) || interfaceValues["PostDown"] != wireGuardPostDown(network) {
		return parsedWireGuardConfig{}, fmt.Errorf("WireGuard hook commands must match the fixed RouteGate forwarding policy")
	}
	parsed.Address = prefix.String()
	parsed.Port = port
	return parsed, nil
}
