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
			vless_port = CASE WHEN $2 THEN $3 ELSE vless_port END,
			vless_flow = CASE WHEN $4 THEN NULLIF($5, '') ELSE vless_flow END,
			vless_network = CASE WHEN $6 THEN NULLIF($7, '') ELSE vless_network END,
			reality_public_key = CASE WHEN $8 THEN NULLIF($9, '') ELSE reality_public_key END,
			reality_private_key = CASE WHEN $8 THEN NULL ELSE reality_private_key END,
			reality_short_id = CASE WHEN $10 THEN NULLIF($11, '') ELSE reality_short_id END,
			reality_server_name = CASE WHEN $12 THEN NULLIF($13, '') ELSE reality_server_name END,
			updated_at = now()
		WHERE id = $1::uuid
		RETURNING
			id::text,
			vless_port,
			COALESCE(vless_flow, ''),
			COALESCE(vless_network, ''),
			COALESCE(reality_public_key, ''),
			COALESCE(reality_short_id, ''),
			COALESCE(reality_server_name, ''),
			updated_at
	`,
		serverID,
		input.VLESSPort != nil, input.VLESSPort,
		input.VLESSFlow != nil, stringValue(input.VLESSFlow),
		input.VLESSNetwork != nil, stringValue(input.VLESSNetwork),
		input.RealityPublicKey != nil, stringValue(input.RealityPublicKey),
		input.RealityShortID != nil, stringValue(input.RealityShortID),
		input.RealityServerName != nil, stringValue(input.RealityServerName),
	))
}

func (r *Repository) UpdateRealityKeypair(ctx context.Context, serverID string, input UpdateRealityKeypairInput) (ProtocolSettings, error) {
	return scanProtocolSettings(r.pool.QueryRow(ctx, `
		UPDATE servers
		SET
			reality_private_key = $2,
			reality_public_key = $3,
			updated_at = now()
		WHERE id = $1::uuid
		RETURNING
			id::text,
			vless_port,
			COALESCE(vless_flow, ''),
			COALESCE(vless_network, ''),
			COALESCE(reality_public_key, ''),
			COALESCE(reality_short_id, ''),
			COALESCE(reality_server_name, ''),
			updated_at
	`, serverID, input.PrivateKey, input.PublicKey))
}

const protocolSettingsSelect = `
	SELECT
		id::text,
		vless_port,
		COALESCE(vless_flow, ''),
		COALESCE(vless_network, ''),
		COALESCE(reality_public_key, ''),
		COALESCE(reality_short_id, ''),
		COALESCE(reality_server_name, ''),
		updated_at
	FROM servers`

func scanProtocolSettings(row scanner) (ProtocolSettings, error) {
	var settings ProtocolSettings
	var vlessPort sql.NullInt32
	err := row.Scan(
		&settings.ServerID,
		&vlessPort,
		&settings.VLESSFlow,
		&settings.VLESSNetwork,
		&settings.RealityPublicKey,
		&settings.RealityShortID,
		&settings.RealityServerName,
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
