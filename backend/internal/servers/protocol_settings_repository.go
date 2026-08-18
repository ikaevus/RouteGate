package servers

import (
	"context"
	"database/sql"
)

func (r *Repository) GetProtocolSettings(ctx context.Context, serverID string) (ProtocolSettings, error) {
	return scanProtocolSettings(r.pool.QueryRow(ctx, protocolSettingsSelect+` WHERE id = $1::uuid`, serverID))
}

func (r *Repository) UpdateProtocolSettings(ctx context.Context, serverID string, input UpdateProtocolSettingsInput) (ProtocolSettings, error) {
	return scanProtocolSettings(r.pool.QueryRow(ctx, `
		UPDATE servers
		SET
			vpn_protocol = CASE WHEN $2 THEN $3 ELSE vpn_protocol END,
			vless_port = CASE WHEN $4 THEN $5 ELSE vless_port END,
			vless_flow = CASE WHEN $6 THEN NULLIF($7, '') ELSE vless_flow END,
			vless_network = CASE WHEN $8 THEN NULLIF($9, '') ELSE vless_network END,
			reality_private_key = CASE
				WHEN $10 AND NULLIF($11, '') IS DISTINCT FROM reality_public_key THEN NULL
				ELSE reality_private_key
			END,
			reality_public_key = CASE WHEN $10 THEN NULLIF($11, '') ELSE reality_public_key END,
			reality_short_id = CASE WHEN $12 THEN NULLIF($13, '') ELSE reality_short_id END,
			reality_server_name = CASE WHEN $14 THEN NULLIF($15, '') ELSE reality_server_name END,
			wireguard_port = CASE WHEN $16 THEN $17 ELSE wireguard_port END,
			wireguard_address = CASE WHEN $18 THEN $19::inet ELSE wireguard_address END,
			wireguard_dns = CASE WHEN $20 THEN $21::inet ELSE wireguard_dns END,
			hysteria2_port = CASE WHEN $22 THEN $23 ELSE hysteria2_port END,
			hysteria2_domain = CASE WHEN $24 THEN $25 ELSE hysteria2_domain END,
			hysteria2_acme_email = CASE WHEN $26 THEN $27 ELSE hysteria2_acme_email END,
			hysteria2_masquerade_url = CASE WHEN $28 THEN $29 ELSE hysteria2_masquerade_url END,
			shadowsocks_port = CASE WHEN $30 THEN $31 ELSE shadowsocks_port END,
			mtproto_port = CASE WHEN $32 THEN $33 ELSE mtproto_port END,
			protocol_updated_at = now(),
			updated_at = now()
		WHERE id = $1::uuid
		RETURNING
			id::text,
			vpn_protocol,
			vless_port,
			COALESCE(vless_flow, ''),
			COALESCE(vless_network, ''),
			COALESCE(reality_public_key, ''),
			COALESCE(reality_short_id, ''),
			COALESCE(reality_server_name, ''),
			wireguard_port,
			wireguard_address::text,
			wireguard_dns::text,
			COALESCE(wireguard_public_key, ''),
			hysteria2_port,
			hysteria2_domain,
			hysteria2_acme_email,
			hysteria2_masquerade_url,
			shadowsocks_port,
			shadowsocks_method,
			shadowsocks_server_key,
			mtproto_port,
			mtproto_secret,
			mtproto_fronting_domain,
			GREATEST(protocol_updated_at, vpn_accounts_config_updated_at)
	`,
		serverID,
		input.Protocol != nil, stringValue(input.Protocol),
		input.VLESSPort != nil, input.VLESSPort,
		input.VLESSFlow != nil, stringValue(input.VLESSFlow),
		input.VLESSNetwork != nil, stringValue(input.VLESSNetwork),
		input.RealityPublicKey != nil, stringValue(input.RealityPublicKey),
		input.RealityShortID != nil, stringValue(input.RealityShortID),
		input.RealityServerName != nil, stringValue(input.RealityServerName),
		input.WireGuardPort != nil, input.WireGuardPort,
		input.WireGuardAddress != nil, stringValue(input.WireGuardAddress),
		input.WireGuardDNS != nil, stringValue(input.WireGuardDNS),
		input.Hysteria2Port != nil, input.Hysteria2Port,
		input.Hysteria2Domain != nil, stringValue(input.Hysteria2Domain),
		input.Hysteria2ACMEEmail != nil, stringValue(input.Hysteria2ACMEEmail),
		input.Hysteria2MasqueradeURL != nil, stringValue(input.Hysteria2MasqueradeURL),
		input.ShadowsocksPort != nil, input.ShadowsocksPort,
		input.MTProtoPort != nil, input.MTProtoPort,
	))
}

func (r *Repository) UpdateRealityKeypair(ctx context.Context, serverID string, input UpdateRealityKeypairInput) (ProtocolSettings, error) {
	return scanProtocolSettings(r.pool.QueryRow(ctx, `
		UPDATE servers
		SET
			reality_private_key = $2,
			reality_public_key = $3,
			protocol_updated_at = now(),
			updated_at = now()
		WHERE id = $1::uuid
		RETURNING
			id::text,
			vpn_protocol,
			vless_port,
			COALESCE(vless_flow, ''),
			COALESCE(vless_network, ''),
			COALESCE(reality_public_key, ''),
			COALESCE(reality_short_id, ''),
			COALESCE(reality_server_name, ''),
			wireguard_port,
			wireguard_address::text,
			wireguard_dns::text,
			COALESCE(wireguard_public_key, ''),
			hysteria2_port,
			hysteria2_domain,
			hysteria2_acme_email,
			hysteria2_masquerade_url,
			shadowsocks_port,
			shadowsocks_method,
			shadowsocks_server_key,
			mtproto_port,
			mtproto_secret,
			mtproto_fronting_domain,
			GREATEST(protocol_updated_at, vpn_accounts_config_updated_at)
	`, serverID, input.PrivateKey, input.PublicKey))
}

func (r *Repository) ConfigureRecommendedWireGuard(ctx context.Context, serverID string, input UpdateWireGuardKeypairInput) (ProtocolSettings, error) {
	return scanProtocolSettings(r.pool.QueryRow(ctx, `
		UPDATE servers
		SET
			vpn_protocol = 'wireguard',
			wireguard_port = 51820,
			wireguard_address = '10.66.0.1/24',
			wireguard_dns = '1.1.1.1',
			wireguard_private_key = $2,
			wireguard_public_key = $3,
			protocol_updated_at = now(),
			updated_at = now()
		WHERE id = $1::uuid
		RETURNING
			id::text,
			vpn_protocol,
			vless_port,
			COALESCE(vless_flow, ''),
			COALESCE(vless_network, ''),
			COALESCE(reality_public_key, ''),
			COALESCE(reality_short_id, ''),
			COALESCE(reality_server_name, ''),
			wireguard_port,
			wireguard_address::text,
			wireguard_dns::text,
			COALESCE(wireguard_public_key, ''),
			hysteria2_port,
			hysteria2_domain,
			hysteria2_acme_email,
			hysteria2_masquerade_url,
			shadowsocks_port,
			shadowsocks_method,
			shadowsocks_server_key,
			mtproto_port,
			mtproto_secret,
			mtproto_fronting_domain,
			GREATEST(protocol_updated_at, vpn_accounts_config_updated_at)
	`, serverID, input.PrivateKey, input.PublicKey))
}

const protocolSettingsSelect = `
	SELECT
		id::text,
		vpn_protocol,
		vless_port,
		COALESCE(vless_flow, ''),
		COALESCE(vless_network, ''),
		COALESCE(reality_public_key, ''),
		COALESCE(reality_short_id, ''),
		COALESCE(reality_server_name, ''),
		wireguard_port,
		wireguard_address::text,
		wireguard_dns::text,
		COALESCE(wireguard_public_key, ''),
		hysteria2_port,
		hysteria2_domain,
		hysteria2_acme_email,
		hysteria2_masquerade_url,
		shadowsocks_port,
		shadowsocks_method,
		shadowsocks_server_key,
		mtproto_port,
		mtproto_secret,
		mtproto_fronting_domain,
		GREATEST(protocol_updated_at, vpn_accounts_config_updated_at)
	FROM servers`

func scanProtocolSettings(row scanner) (ProtocolSettings, error) {
	var settings ProtocolSettings
	var vlessPort sql.NullInt32
	err := row.Scan(
		&settings.ServerID,
		&settings.Protocol,
		&vlessPort,
		&settings.VLESSFlow,
		&settings.VLESSNetwork,
		&settings.RealityPublicKey,
		&settings.RealityShortID,
		&settings.RealityServerName,
		&settings.WireGuardPort,
		&settings.WireGuardAddress,
		&settings.WireGuardDNS,
		&settings.WireGuardPublicKey,
		&settings.Hysteria2Port,
		&settings.Hysteria2Domain,
		&settings.Hysteria2ACMEEmail,
		&settings.Hysteria2MasqueradeURL,
		&settings.ShadowsocksPort,
		&settings.ShadowsocksMethod,
		&settings.ShadowsocksServerKey,
		&settings.MTProtoPort,
		&settings.MTProtoSecret,
		&settings.MTProtoFrontingDomain,
		&settings.UpdatedAt,
	)
	if err != nil {
		return ProtocolSettings{}, err
	}
	settings.VLESSPort = defaultVLESSPort
	if vlessPort.Valid {
		settings.VLESSPort = int(vlessPort.Int32)
	}
	return settings, nil
}
