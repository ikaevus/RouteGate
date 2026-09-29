package configs

import (
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ikaevus/routegate/backend/internal/platform"
	wgcredentials "github.com/ikaevus/routegate/backend/internal/wireguard"
)

// clientSettingsFromRenderedJSON derives the client-facing node parameters
// from what a config version actually deploys. Deriving them from the stored
// rendered config (rather than from the saved server settings) means the
// snapshot can only describe the runtime that the version installs, and it
// works equally for versions rendered before snapshots existed.
//
// A protocol section that is present but cannot be parsed is an error: a
// partial snapshot could silently hand clients wrong parameters.
//
// primaries carries the primary protocol chosen per account at render time;
// it may be nil (backfill of versions rendered before it was recorded).
func clientSettingsFromRenderedJSON(payload []byte, primaries map[string]string) (platform.AppliedClientSettings, error) {
	var config RenderedConfig
	if err := json.Unmarshal(payload, &config); err != nil {
		return platform.AppliedClientSettings{}, fmt.Errorf("decode rendered config: %w", err)
	}
	settings := platform.AppliedClientSettings{VPNProtocol: config.Metadata.VPNCore.Protocol}
	settings.Accounts = renderedAccountDeployments(payload, config, primaries)

	for _, inbound := range config.SingBox.Inbounds {
		switch strings.ToLower(strings.TrimSpace(stringValue(inbound["type"]))) {
		case platform.VPNProtocolVLESS:
			if err := vlessClientSettings(inbound, &settings); err != nil {
				return platform.AppliedClientSettings{}, err
			}
		case platform.VPNProtocolShadowsocks:
			settings.ShadowsocksPort = intValue(inbound["listen_port"])
			settings.ShadowsocksMethod = stringValue(inbound["method"])
			settings.ShadowsocksServerKey = strings.TrimSpace(stringValue(inbound["password"]))
			if settings.ShadowsocksPort < 1 || settings.ShadowsocksMethod == "" || settings.ShadowsocksServerKey == "" {
				return platform.AppliedClientSettings{}, errors.New("rendered Shadowsocks inbound is incomplete")
			}
		}
	}

	if strings.TrimSpace(config.WireGuard) != "" {
		parsed, err := parseWireGuardServerConfig(config.WireGuard)
		if err != nil {
			return platform.AppliedClientSettings{}, err
		}
		publicKey, err := wgcredentials.PublicKeyFromPrivate(parsed.PrivateKey)
		if err != nil {
			return platform.AppliedClientSettings{}, fmt.Errorf("derive WireGuard public key: %w", err)
		}
		settings.WireGuardPort, settings.WireGuardAddress, settings.WireGuardPublicKey = parsed.Port, parsed.Address, publicKey
	}
	if strings.TrimSpace(config.Hysteria2) != "" {
		parsed, err := parseHysteria2ServerConfig(config.Hysteria2)
		if err != nil {
			return platform.AppliedClientSettings{}, err
		}
		settings.Hysteria2Port, _ = strconv.Atoi(strings.TrimPrefix(parsed.Listen, ":"))
		settings.Hysteria2Domain = parsed.ACME.Domains[0]
	}
	if strings.TrimSpace(config.MTProto) != "" {
		parsed, err := parseMTProtoServerConfig(config.MTProto)
		if err != nil {
			return platform.AppliedClientSettings{}, err
		}
		settings.MTProtoPort, settings.MTProtoSecret, settings.MTProtoFrontingDomain = parsed.Port, parsed.Secret, parsed.FrontingDomain
	}
	return settings, nil
}

func vlessClientSettings(inbound map[string]any, settings *platform.AppliedClientSettings) error {
	settings.VLESSPort = intValue(inbound["listen_port"])
	if settings.VLESSPort < 1 || settings.VLESSPort > 65535 {
		return errors.New("rendered VLESS inbound has no valid listen_port")
	}
	// The managed VLESS inbound never sets a transport, so sing-box serves raw
	// TCP regardless of the transport saved for the server.
	settings.VLESSNetwork = platform.VPNTransportTCP
	if users, ok := inbound["users"].([]any); ok && len(users) > 0 {
		if user, ok := users[0].(map[string]any); ok {
			settings.VLESSFlow = strings.TrimSpace(stringValue(user["flow"]))
		}
	}

	tls, ok := inbound["tls"].(map[string]any)
	if !ok {
		return nil
	}
	reality, ok := tls["reality"].(map[string]any)
	if !ok || !boolValue(reality["enabled"]) {
		return nil
	}
	settings.RealityServerName = strings.TrimSpace(stringValue(tls["server_name"]))
	if shortIDs, ok := reality["short_id"].([]any); ok && len(shortIDs) > 0 {
		settings.RealityShortID = strings.TrimSpace(stringValue(shortIDs[0]))
	}
	publicKey, err := realityPublicKeyFromPrivate(stringValue(reality["private_key"]))
	if err != nil {
		return err
	}
	settings.RealityPublicKey = publicKey
	return nil
}

func realityPublicKeyFromPrivate(value string) (string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return "", fmt.Errorf("decode Reality private key: %w", err)
	}
	privateKey, err := ecdh.X25519().NewPrivateKey(decoded)
	if err != nil {
		return "", fmt.Errorf("parse Reality private key: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(privateKey.PublicKey().Bytes()), nil
}

// renderedAccountDeployments lists what the version deploys per account. It
// returns nil (unknown) unless the rendered config carries a vpnAccounts list
// whose every entry names an account and at least one protocol, so a legacy
// or unexpected render never makes deployed accounts look undeployed.
func renderedAccountDeployments(payload []byte, config RenderedConfig, primaries map[string]string) map[string]platform.AppliedAccountProtocols {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil
	}
	if _, listed := envelope["vpnAccounts"]; !listed {
		return nil
	}
	mtprotoDeployed := strings.TrimSpace(config.MTProto) != ""
	accounts := make(map[string]platform.AppliedAccountProtocols, len(config.VPNAccounts))
	for _, account := range config.VPNAccounts {
		// Older renders did not record the protocol list; the credentials the
		// render issued identify the deployed protocols just as reliably.
		protocols := renderedAccountProtocols(account)
		if strings.TrimSpace(account.ID) == "" || len(protocols) == 0 {
			return nil
		}
		applied := platform.AppliedAccountProtocols{Protocols: protocols}
		if requested := primaries[account.ID]; requested != "" {
			primary := normalizeAccountProtocol(requested)
			if protocolListContains(protocols, primary) || (primary == platform.VPNProtocolMTProto && mtprotoDeployed) {
				applied.Primary = primary
			}
		}
		accounts[account.ID] = applied
	}
	return accounts
}
