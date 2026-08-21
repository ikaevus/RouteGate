package configs

import (
	"context"
	"strings"

	"github.com/ikaevus/routegate/backend/internal/platform"
	wgcredentials "github.com/ikaevus/routegate/backend/internal/wireguard"
)

type accountProtocolResolver interface {
	ResolveServerAccountProtocols(context.Context, string) (map[string]string, error)
}

// ResolveServerAccountProtocols returns the effective protocol for every
// account assigned to a node. "auto" inherits the node default. WireGuard peer
// material is created before ServerConfigInfo is loaded so the same render can
// immediately include newly-selected WireGuard accounts.
func (r *Repository) ResolveServerAccountProtocols(ctx context.Context, serverID string) (map[string]string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			a.id::text,
			COALESCE(NULLIF(cp.protocol, 'auto'), s.vpn_protocol, 'vless')
		FROM vpn_accounts a
		JOIN servers s ON s.id = a.server_id
		LEFT JOIN vpn_client_profiles cp ON cp.vpn_account_id = a.id
		WHERE a.server_id = $1::uuid
		ORDER BY a.created_at ASC, a.id ASC
	`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	resolved := map[string]string{}
	wireGuardRequired := false
	for rows.Next() {
		var accountID, protocol string
		if err := rows.Scan(&accountID, &protocol); err != nil {
			return nil, err
		}
		protocol = strings.ToLower(strings.TrimSpace(protocol))
		if protocol == "" {
			protocol = platform.VPNProtocolVLESS
		}
		resolved[accountID] = protocol
		wireGuardRequired = wireGuardRequired || protocol == platform.VPNProtocolWireGuard
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if wireGuardRequired {
		if err := wgcredentials.EnsureServerPeerCredentials(ctx, r.pool, serverID); err != nil {
			return nil, err
		}
	}
	return resolved, nil
}

func resolveServerAccountProtocols(ctx context.Context, repository configRepository, serverID string) (map[string]string, error) {
	resolver, ok := repository.(accountProtocolResolver)
	if !ok {
		return map[string]string{}, nil
	}
	return resolver.ResolveServerAccountProtocols(ctx, serverID)
}

func applyResolvedAccountProtocols(info *ServerConfigInfo, resolved map[string]string) {
	defaultProtocol := strings.ToLower(strings.TrimSpace(info.VPNProtocol))
	if defaultProtocol == "" {
		defaultProtocol = platform.VPNProtocolVLESS
	}
	for index := range info.VPNAccounts {
		protocol := strings.ToLower(strings.TrimSpace(resolved[info.VPNAccounts[index].ID]))
		if protocol == "" || protocol == "auto" {
			protocol = defaultProtocol
		}
		info.VPNAccounts[index].VPNProtocol = protocol
	}
}

func selectedVPNCoreAdapters(info ServerConfigInfo) []vpnCoreAdapter {
	wanted := map[string]bool{}
	for _, account := range info.VPNAccounts {
		if account.Status != "active" || account.TrafficEnforcementStatus == "over_limit" {
			continue
		}
		protocol := strings.ToLower(strings.TrimSpace(account.VPNProtocol))
		if protocol == "" || protocol == "auto" {
			protocol = strings.ToLower(strings.TrimSpace(info.VPNProtocol))
		}
		if protocol == "" {
			protocol = platform.VPNProtocolVLESS
		}
		wanted[protocol] = true
	}
	if len(wanted) == 0 {
		protocol := strings.ToLower(strings.TrimSpace(info.VPNProtocol))
		if protocol == "" {
			protocol = platform.VPNProtocolVLESS
		}
		wanted[protocol] = true
	}

	order := []string{
		platform.VPNProtocolVLESS,
		platform.VPNProtocolWireGuard,
		platform.VPNProtocolHysteria2,
		platform.VPNProtocolShadowsocks,
		platform.VPNProtocolMTProto,
	}
	adapters := make([]vpnCoreAdapter, 0, len(wanted))
	for _, protocol := range order {
		if !wanted[protocol] {
			continue
		}
		candidateInfo := info
		candidateInfo.VPNProtocol = protocol
		adapters = append(adapters, selectedVPNCoreAdapter(candidateInfo))
	}
	return adapters
}

func configVPNCoreFromAdapter(adapter vpnCoreAdapter, realityEnabled bool) ConfigVPNCore {
	descriptor := adapter.Descriptor()
	transport := ""
	if len(descriptor.Transports) > 0 {
		transport = descriptor.Transports[0]
	}
	return ConfigVPNCore{
		Core:      descriptor.Core,
		Protocol:  descriptor.Protocol,
		Transport: transport,
		Security:  selectedAdapterSecurity(descriptor, realityEnabled),
	}
}

func renderSelectedVPNCoreAdapters(config *RenderedConfig, info ServerConfigInfo) {
	adapters := selectedVPNCoreAdapters(info)
	if len(adapters) != 1 || configVPNCoreFromAdapter(adapters[0], config.Metadata.RealityEnabled) != config.Metadata.VPNCore {
		config.Metadata.VPNCores = make([]ConfigVPNCore, 0, len(adapters))
		for _, adapter := range adapters {
			config.Metadata.VPNCores = append(config.Metadata.VPNCores, configVPNCoreFromAdapter(adapter, config.Metadata.RealityEnabled))
		}
	}
	for _, adapter := range adapters {
		adapter.Render(config, info)
	}
}

func configuredVPNCoreAdapters(config RenderedConfig) ([]vpnCoreAdapter, bool) {
	registry := defaultVPNCoreAdapterRegistry()
	descriptors := config.Metadata.VPNCores
	if len(descriptors) == 0 {
		descriptors = []ConfigVPNCore{config.Metadata.VPNCore}
	}
	adapters := make([]vpnCoreAdapter, 0, len(descriptors))
	seen := map[string]struct{}{}
	for _, descriptor := range descriptors {
		if strings.TrimSpace(descriptor.Core) == "" {
			adapter, ok := adapterForRenderedConfig(config)
			if !ok {
				return nil, false
			}
			key := adapter.Descriptor().Core + "/" + adapter.Descriptor().Protocol
			if _, exists := seen[key]; !exists {
				adapters = append(adapters, adapter)
				seen[key] = struct{}{}
			}
			continue
		}
		adapter, ok := registry.Resolve(descriptor.Core, descriptor.Protocol, descriptor.Transport, descriptor.Security)
		if !ok {
			return nil, false
		}
		key := descriptor.Core + "/" + descriptor.Protocol
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		adapters = append(adapters, adapter)
	}
	return adapters, len(adapters) > 0
}

func validateConfiguredVPNCoreAdapters(config RenderedConfig, result *ValidationResult) {
	adapters, ok := configuredVPNCoreAdapters(config)
	if !ok {
		result.Valid = false
		result.Errors = append(result.Errors, "metadata.vpnCores selects an unsupported adapter composition.")
		return
	}
	for _, adapter := range adapters {
		adapter.Validate(config, result)
	}
}

func configuredVPNServicesReady(config RenderedConfig) bool {
	adapters, ok := configuredVPNCoreAdapters(config)
	if !ok {
		return false
	}
	for _, adapter := range adapters {
		if !adapter.Ready(config) {
			return false
		}
	}
	return true
}

func ensureSingBoxBase(config *RenderedConfig) {
	if config.SingBox.Log.Level == "" {
		config.SingBox.Log = SingBoxLog{Level: "info"}
	}
	if config.SingBox.Inbounds == nil {
		config.SingBox.Inbounds = []map[string]any{}
	}
	if config.SingBox.Outbounds == nil {
		config.SingBox.Outbounds = []SingBoxOutbound{}
	}
	ensureSingBoxOutbound(config, SingBoxOutbound{Type: "direct", Tag: singBoxDirectTag})
	if config.SingBox.Route.Rules == nil {
		config.SingBox.Route.Rules = []map[string]any{}
	}
	if config.SingBox.Route.Final == "" {
		config.SingBox.Route.Final = singBoxDirectTag
	}
}

func accountUsesProtocol(account VPNAccountConfigInfo, protocol string) bool {
	selected := strings.ToLower(strings.TrimSpace(account.VPNProtocol))
	// Empty preserves pre-RG-114J unit fixtures where the adapter itself defines
	// the only protocol under test.
	return selected == "" || selected == strings.ToLower(strings.TrimSpace(protocol))
}
