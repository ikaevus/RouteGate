package vpnaccounts

import (
	"context"
	"strings"
)

type activeClientProtocolSource interface {
	GetActiveClientProtocol(context.Context, string) (string, error)
}

func (r *Repository) GetActiveClientProtocol(ctx context.Context, accountID string) (string, error) {
	var protocol string
	err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(cp.active_protocol, s.vpn_protocol, 'vless')
		FROM vpn_accounts a
		LEFT JOIN servers s ON s.id = a.server_id
		LEFT JOIN vpn_client_profiles cp ON cp.vpn_account_id = a.id
		WHERE a.id = $1::uuid
	`, accountID).Scan(&protocol)
	return normalizeConcreteClientProtocol(protocol), err
}

func normalizeConcreteClientProtocol(protocol string) string {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	switch protocol {
	case ClientProtocolVLESS,
		ClientProtocolWireGuard,
		ClientProtocolHysteria2,
		ClientProtocolShadowsocks,
		ClientProtocolMTProto:
		return protocol
	default:
		return ClientProtocolVLESS
	}
}

func resolveActiveClientProtocol(ctx context.Context, source any, accountID string, profile ClientProfile, server *SubscriptionServer) (string, error) {
	if activeSource, ok := source.(activeClientProtocolSource); ok {
		protocol, err := activeSource.GetActiveClientProtocol(ctx, accountID)
		if err != nil {
			return "", err
		}
		return normalizeConcreteClientProtocol(protocol), nil
	}
	// Compatibility fallback for unit-test sources and older in-memory callers.
	return resolveEffectiveClientProtocol(profile, server), nil
}
