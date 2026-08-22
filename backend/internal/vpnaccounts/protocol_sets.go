package vpnaccounts

import (
	"context"
	"errors"
	"strings"
)

var concreteClientProtocols = []string{
	ClientProtocolVLESS,
	ClientProtocolWireGuard,
	ClientProtocolHysteria2,
	ClientProtocolShadowsocks,
	ClientProtocolMTProto,
}

type clientProtocolSetRepository interface {
	GetClientProtocolSets(context.Context, string) ([]string, []string, error)
	UpdateClientProfileWithProtocols(context.Context, string, UpdateClientProfileRequest, []string) (ClientProfile, error)
}

func normalizeConcreteClientProtocols(values []string) ([]string, error) {
	selected := make(map[string]struct{}, len(values))
	for _, value := range values {
		protocol := strings.ToLower(strings.TrimSpace(value))
		if protocol == "" {
			continue
		}
		if protocol == ClientProtocolAuto {
			return nil, errors.New("enabledProtocols must contain concrete protocols, not auto")
		}
		if _, ok := allowedClientProtocols[protocol]; !ok {
			return nil, errors.New("enabledProtocols contains an unsupported protocol")
		}
		selected[protocol] = struct{}{}
	}
	ordered := make([]string, 0, len(selected))
	for _, protocol := range concreteClientProtocols {
		if _, ok := selected[protocol]; ok {
			ordered = append(ordered, protocol)
		}
	}
	return ordered, nil
}

func containsClientProtocol(protocols []string, protocol string) bool {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	for _, candidate := range protocols {
		if strings.ToLower(strings.TrimSpace(candidate)) == protocol {
			return true
		}
	}
	return false
}

// GetClientProtocolSets returns the desired and last successfully applied
// protocol sets. It lazily seeds rows for profiles created after the migration,
// preserving the legacy active_protocol and requested primary preference.
func (r *Repository) GetClientProtocolSets(ctx context.Context, accountID string) ([]string, []string, error) {
	if _, err := r.pool.Exec(ctx, `
		INSERT INTO vpn_account_protocols (
			vpn_account_id, protocol, desired_enabled, active_enabled, updated_at, activated_at
		)
		SELECT
			cp.vpn_account_id,
			COALESCE(NULLIF(cp.active_protocol, ''), 'vless'),
			TRUE,
			TRUE,
			cp.updated_at,
			now()
		FROM vpn_client_profiles cp
		WHERE cp.vpn_account_id = $1::uuid
		ON CONFLICT (vpn_account_id, protocol) DO NOTHING
	`, accountID); err != nil {
		return nil, nil, err
	}
	if _, err := r.pool.Exec(ctx, `
		INSERT INTO vpn_account_protocols (
			vpn_account_id, protocol, desired_enabled, active_enabled, updated_at
		)
		SELECT
			cp.vpn_account_id,
			COALESCE(NULLIF(cp.protocol, 'auto'), NULLIF(s.vpn_protocol, 'auto'), 'vless'),
			TRUE,
			FALSE,
			cp.updated_at
		FROM vpn_client_profiles cp
		JOIN vpn_accounts a ON a.id = cp.vpn_account_id
		LEFT JOIN servers s ON s.id = a.server_id
		WHERE cp.vpn_account_id = $1::uuid
		ON CONFLICT (vpn_account_id, protocol) DO NOTHING
	`, accountID); err != nil {
		return nil, nil, err
	}

	rows, err := r.pool.Query(ctx, `
		SELECT protocol, desired_enabled, active_enabled
		FROM vpn_account_protocols
		WHERE vpn_account_id = $1::uuid
		ORDER BY CASE protocol
			WHEN 'vless' THEN 1
			WHEN 'wireguard' THEN 2
			WHEN 'hysteria2' THEN 3
			WHEN 'shadowsocks' THEN 4
			WHEN 'mtproto' THEN 5
			ELSE 99
		END
	`, accountID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	desired := []string{}
	active := []string{}
	for rows.Next() {
		var protocol string
		var desiredEnabled, activeEnabled bool
		if err := rows.Scan(&protocol, &desiredEnabled, &activeEnabled); err != nil {
			return nil, nil, err
		}
		if desiredEnabled {
			desired = append(desired, protocol)
		}
		if activeEnabled {
			active = append(active, protocol)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return desired, active, nil
}

// UpdateClientProfileWithProtocols persists the legacy primary preference and
// the complete desired protocol set atomically. active_enabled is intentionally
// untouched here; only a successful config apply promotes desired state.
func (r *Repository) UpdateClientProfileWithProtocols(ctx context.Context, accountID string, request UpdateClientProfileRequest, enabledProtocols []string) (ClientProfile, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ClientProfile{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		INSERT INTO vpn_client_profiles (vpn_account_id)
		VALUES ($1::uuid)
		ON CONFLICT (vpn_account_id) DO NOTHING
	`, accountID); err != nil {
		return ClientProfile{}, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE vpn_client_profiles
		SET
			name = $2,
			client_type = $3,
			device_type = $4,
			fingerprint_mode = $5,
			fingerprint = $6,
			server_name_override = NULLIF($7, ''),
			spider_x = $8,
			mtu = $9,
			protocol = $10,
			updated_at = now()
		WHERE vpn_account_id = $1::uuid
	`, accountID, request.Name, request.ClientType, request.DeviceType, request.FingerprintMode, request.Fingerprint, request.ServerNameOverride, request.SpiderX, request.MTU, request.Protocol); err != nil {
		return ClientProfile{}, err
	}

	for _, protocol := range concreteClientProtocols {
		desired := containsClientProtocol(enabledProtocols, protocol)
		if _, err := tx.Exec(ctx, `
			INSERT INTO vpn_account_protocols (
				vpn_account_id, protocol, desired_enabled, active_enabled, updated_at
			)
			VALUES ($1::uuid, $2, $3, FALSE, now())
			ON CONFLICT (vpn_account_id, protocol) DO UPDATE
			SET
				desired_enabled = EXCLUDED.desired_enabled,
				updated_at = EXCLUDED.updated_at
		`, accountID, protocol, desired); err != nil {
			return ClientProfile{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return ClientProfile{}, err
	}
	return r.GetOrCreateClientProfile(ctx, accountID)
}

func hydrateClientProtocolSets(ctx context.Context, source any, accountID string, profile *ClientProfile) error {
	if profile == nil {
		return nil
	}
	repository, ok := source.(clientProtocolSetRepository)
	if !ok {
		return nil
	}
	desired, active, err := repository.GetClientProtocolSets(ctx, accountID)
	if err != nil {
		return err
	}
	profile.EnabledProtocols = desired
	profile.ActiveProtocols = active
	return nil
}

func effectiveRequestedProtocols(profile ClientProfile, server *SubscriptionServer) []string {
	if len(profile.EnabledProtocols) > 0 {
		return append([]string(nil), profile.EnabledProtocols...)
	}
	return []string{resolveEffectiveClientProtocol(profile, server)}
}

type ClientProtocolConnection struct {
	Protocol        string `json:"protocol"`
	Format          string `json:"format"`
	VLESSLink       string `json:"vlessLink,omitempty"`
	WireGuardConfig string `json:"wireGuardConfig,omitempty"`
	Hysteria2URI    string `json:"hysteria2Uri,omitempty"`
	ShadowsocksURI  string `json:"shadowsocksUri,omitempty"`
	MTProtoURI      string `json:"mtprotoUri,omitempty"`
	Endpoint        string `json:"endpoint,omitempty"`
	ServerName      string `json:"serverName,omitempty"`
	Network         string `json:"network,omitempty"`
	Flow            string `json:"flow,omitempty"`
}

func protocolConnectionFromResponse(response ClientConnectionResponse) ClientProtocolConnection {
	return ClientProtocolConnection{
		Protocol:        response.Protocol,
		Format:          response.Format,
		VLESSLink:       response.VLESSLink,
		WireGuardConfig: response.WireGuardConfig,
		Hysteria2URI:    response.Hysteria2URI,
		ShadowsocksURI:  response.ShadowsocksURI,
		MTProtoURI:      response.MTProtoURI,
		Endpoint:        response.Endpoint,
		ServerName:      response.ServerName,
		Network:         response.Network,
		Flow:            response.Flow,
	}
}

func attachActiveProtocolConnections(accountID string, subscription SubscriptionProfile, profile ClientProfile, response *ClientConnectionResponse) error {
	if response == nil {
		return nil
	}
	protocols := profile.ActiveProtocols
	if len(protocols) == 0 {
		protocols = []string{response.Protocol}
	}
	connections := make([]ClientProtocolConnection, 0, len(protocols))
	for _, protocol := range protocols {
		connection, err := buildClientConnectionResponseForProtocol(accountID, subscription, profile, protocol)
		if err != nil {
			return err
		}
		connections = append(connections, protocolConnectionFromResponse(connection))
	}
	response.Connections = connections
	response.Profile = profile
	return nil
}
