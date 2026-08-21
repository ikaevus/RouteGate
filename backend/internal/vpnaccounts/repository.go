package vpnaccounts

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	wgcredentials "github.com/ikaevus/routegate/backend/internal/wireguard"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) CreateAccount(ctx context.Context, input CreateAccountInput) (Account, error) {
	status := input.Status
	if status == "" {
		status = StatusCreated
	}

	return scanAccount(r.pool.QueryRow(ctx, `
		INSERT INTO vpn_accounts (
			username, protocol, display_name, email, status, expires_at, max_devices, server_id
		)
		VALUES (
			$1, 'sing-box', $1, NULLIF($2, ''), $3, $4, $5, NULLIF($6, '')::uuid
		)
		RETURNING
			id::text,
			display_name,
			COALESCE(email, ''),
			status,
			expires_at,
			max_devices,
			COALESCE(server_id::text, ''),
			COALESCE(vless_uuid::text, ''),
			created_at,
			updated_at,
			config_updated_at
	`, input.DisplayName, input.Email, status, input.ExpiresAt, input.MaxDevices, input.ServerID))
}

func (r *Repository) ListAccounts(ctx context.Context, filter AccountFilter) ([]Account, error) {
	rows, err := r.pool.Query(ctx, accountSelect+accountFilterSQL+`
		ORDER BY created_at DESC, id DESC
		LIMIT CASE WHEN $5 > 0 THEN $5 ELSE 50 END
		OFFSET CASE WHEN $6 > 0 THEN $6 ELSE 0 END
	`, filter.Status, filter.ServerID, filter.Search, filter.SearchUUID, filter.Limit, filter.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Account, 0)
	for rows.Next() {
		item, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CountAccounts(ctx context.Context, filter AccountFilter) (int, error) {
	var total int
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM vpn_accounts
	`+accountFilterSQL, filter.Status, filter.ServerID, filter.Search, filter.SearchUUID).Scan(&total)
	return total, err
}

func (r *Repository) GetAccountByID(ctx context.Context, id string) (Account, error) {
	return scanAccount(r.pool.QueryRow(ctx, accountSelect+` WHERE id = $1::uuid`, id))
}

func (r *Repository) UpdateAccount(ctx context.Context, id string, input UpdateAccountInput) (Account, error) {
	return scanAccount(r.pool.QueryRow(ctx, `
		UPDATE vpn_accounts
		SET
			username = CASE WHEN $2 THEN $3 ELSE username END,
			display_name = CASE WHEN $2 THEN $3 ELSE display_name END,
			email = CASE WHEN $4 THEN NULLIF($5, '') ELSE email END,
			status = CASE WHEN $6 THEN $7 ELSE status END,
			expires_at = CASE
				WHEN $8 THEN NULL
				WHEN $9 THEN $10
				ELSE expires_at
			END,
			max_devices = CASE
				WHEN $11 THEN NULL
				WHEN $12 THEN $13
				ELSE max_devices
			END,
			server_id = CASE WHEN $14 THEN NULLIF($15, '')::uuid ELSE server_id END,
			updated_at = now(),
			config_updated_at = CASE
				WHEN $2 OR $6 OR $14 THEN now()
				ELSE config_updated_at
			END
		WHERE id = $1::uuid
		RETURNING
			id::text,
			display_name,
			COALESCE(email, ''),
			status,
			expires_at,
			max_devices,
			COALESCE(server_id::text, ''),
			COALESCE(vless_uuid::text, ''),
			created_at,
			updated_at,
			config_updated_at
	`,
		id,
		input.DisplayName != nil, stringValue(input.DisplayName),
		input.Email != nil, stringValue(input.Email),
		input.Status != nil, stringValue(input.Status),
		input.ClearExpiresAt, input.ExpiresAt != nil, input.ExpiresAt,
		input.ClearMaxDevices, input.MaxDevices != nil, input.MaxDevices,
		input.ServerID != nil, stringValue(input.ServerID),
	))
}

func (r *Repository) SetAccountStatus(ctx context.Context, id string, status string) (Account, error) {
	return scanAccount(r.pool.QueryRow(ctx, `
		UPDATE vpn_accounts
		SET status = $2, updated_at = now(), config_updated_at = now()
		WHERE id = $1::uuid
		RETURNING
			id::text,
			display_name,
			COALESCE(email, ''),
			status,
			expires_at,
			max_devices,
			COALESCE(server_id::text, ''),
			COALESCE(vless_uuid::text, ''),
			created_at,
			updated_at,
			config_updated_at
	`, id, status))
}

func (r *Repository) DeleteAccount(ctx context.Context, id string) error {
	result, err := r.pool.Exec(ctx, `DELETE FROM vpn_accounts WHERE id = $1::uuid`, id)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (r *Repository) CreateSubscriptionToken(ctx context.Context, input CreateSubscriptionTokenInput) (SubscriptionToken, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return SubscriptionToken{}, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		UPDATE vpn_subscription_tokens
		SET status = 'revoked', revoked_at = now(), updated_at = now()
		WHERE vpn_account_id = $1::uuid AND status = 'active'
	`, input.VPNAccountID); err != nil {
		return SubscriptionToken{}, err
	}

	token, err := scanSubscriptionToken(tx.QueryRow(ctx, `
		INSERT INTO vpn_subscription_tokens (vpn_account_id, token_hash, expires_at)
		VALUES ($1::uuid, $2, $3)
		RETURNING
			id::text,
			vpn_account_id::text,
			token_hash,
			status,
			expires_at,
			last_used_at,
			revoked_at,
			created_at,
			updated_at
	`, input.VPNAccountID, input.TokenHash, input.ExpiresAt))
	if err != nil {
		return SubscriptionToken{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SubscriptionToken{}, err
	}
	return token, nil
}

func (r *Repository) RevokeActiveSubscriptionTokens(ctx context.Context, vpnAccountID string) error {
	result, err := r.pool.Exec(ctx, `
		UPDATE vpn_subscription_tokens
		SET status = 'revoked', revoked_at = now(), updated_at = now()
		WHERE vpn_account_id = $1::uuid AND status = 'active'
	`, vpnAccountID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (r *Repository) GetActiveSubscriptionTokenByHash(ctx context.Context, vpnAccountID string, tokenHash string) (SubscriptionToken, error) {
	return scanSubscriptionToken(r.pool.QueryRow(ctx, subscriptionTokenSelect+`
		WHERE vpn_account_id = $1::uuid
		  AND token_hash = $2
		  AND status = 'active'
		  AND (expires_at IS NULL OR expires_at > now())
	`, vpnAccountID, tokenHash))
}

func (r *Repository) FindActiveSubscriptionTokenByHash(ctx context.Context, tokenHash string) (SubscriptionToken, error) {
	return scanSubscriptionToken(r.pool.QueryRow(ctx, subscriptionTokenSelect+`
		WHERE token_hash = $1
		  AND status = 'active'
		  AND (expires_at IS NULL OR expires_at > now())
	`, tokenHash))
}

func (r *Repository) GetSubscriptionProfileByAccountID(ctx context.Context, id string) (SubscriptionProfile, error) {
	var serverID, effectiveProtocol string
	if err := r.pool.QueryRow(ctx, `
		SELECT
			COALESCE(a.server_id::text, ''),
			COALESCE(NULLIF(cp.protocol, 'auto'), s.vpn_protocol, 'vless')
		FROM vpn_accounts a
		LEFT JOIN servers s ON s.id = a.server_id
		LEFT JOIN vpn_client_profiles cp ON cp.vpn_account_id = a.id
		WHERE a.id = $1::uuid
	`, id).Scan(&serverID, &effectiveProtocol); err != nil {
		return SubscriptionProfile{}, err
	}
	if serverID != "" && effectiveProtocol == ClientProtocolWireGuard {
		if err := wgcredentials.EnsureServerPeerCredentials(ctx, r.pool, serverID); err != nil {
			return SubscriptionProfile{}, err
		}
	}
	profile, err := scanSubscriptionProfile(r.pool.QueryRow(ctx, `
		SELECT
			a.id::text,
			a.display_name,
			COALESCE(a.email, ''),
			a.status,
			a.expires_at,
			a.max_devices,
			COALESCE(a.server_id::text, ''),
			COALESCE(a.vless_uuid::text, ''),
			COALESCE(a.wireguard_private_key, ''),
			COALESCE(a.wireguard_public_key, ''),
			COALESCE(a.wireguard_address::text, ''),
			a.hysteria2_password,
			a.shadowsocks_user_key,
			a.created_at,
			a.updated_at,
			a.config_updated_at,
			s.id::text,
			COALESCE(s.name, ''),
			COALESCE(s.hostname, ''),
			COALESCE(s.public_ip::text, ''),
			COALESCE(s.location, ''),
			COALESCE(s.provider, ''),
			s.vless_port,
			COALESCE(s.vless_flow, ''),
			COALESCE(s.vless_network, ''),
			COALESCE(s.reality_public_key, ''),
			COALESCE(s.reality_short_id, ''),
			COALESCE(s.reality_server_name, ''),
			COALESCE(NULLIF(cp.protocol, 'auto'), s.vpn_protocol, 'vless'),
			COALESCE(s.wireguard_port, 51820),
			COALESCE(s.wireguard_address::text, '10.66.0.1/24'),
			COALESCE(s.wireguard_dns::text, '1.1.1.1'),
			COALESCE(s.wireguard_public_key, ''),
			s.hysteria2_port,
			s.hysteria2_domain,
			s.hysteria2_acme_email,
			s.shadowsocks_port,
			s.shadowsocks_method,
			s.shadowsocks_server_key,
			s.mtproto_port,
			s.mtproto_secret,
			s.mtproto_fronting_domain
		FROM vpn_accounts a
		LEFT JOIN servers s ON s.id = a.server_id
		LEFT JOIN vpn_client_profiles cp ON cp.vpn_account_id = a.id
		WHERE a.id = $1::uuid
	`, id))
	if err != nil {
		return SubscriptionProfile{}, err
	}
	if profile.Account.ServerID == "" {
		return profile, nil
	}

	routingProfile, err := r.getSubscriptionRoutingProfile(ctx, profile.Account.ID, profile.Account.ServerID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return SubscriptionProfile{}, err
	}
	if err == nil {
		profile.RoutingProfile = &routingProfile
	}
	return profile, nil
}

func (r *Repository) MarkSubscriptionTokenUsed(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE vpn_subscription_tokens
		SET last_used_at = now(), updated_at = now()
		WHERE id = $1::uuid
	`, id)
	return err
}

func (r *Repository) getSubscriptionRoutingProfile(ctx context.Context, accountID, serverID string) (RoutingProfile, error) {
	profile, err := scanRoutingProfile(r.pool.QueryRow(ctx, `
		SELECT
			p.id::text,
			p.name,
			COALESCE(p.description, ''),
			p.is_default
		FROM routing_profiles p
		WHERE p.id = COALESCE(
			(
				SELECT arp.routing_profile_id
				FROM vpn_account_routing_profiles arp
				WHERE arp.vpn_account_id = $1::uuid
			),
			(
				SELECT srp.routing_profile_id
				FROM server_routing_profiles srp
				WHERE srp.server_id = $2::uuid
			),
			(
				SELECT rp.id
				FROM routing_profiles rp
				WHERE rp.is_default = TRUE
				ORDER BY rp.created_at ASC
				LIMIT 1
			)
		)
		LIMIT 1
	`, accountID, serverID))
	if err != nil {
		return RoutingProfile{}, err
	}

	rules, err := r.listRoutingProfileRules(ctx, profile.ID)
	if err != nil {
		return RoutingProfile{}, err
	}
	profile.Rules = rules
	return profile, nil
}

func (r *Repository) listRoutingProfileRules(ctx context.Context, profileID string) ([]RoutingProfileRule, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			id::text,
			name,
			priority,
			action,
			domains,
			domain_suffixes,
			domain_keywords,
			ip_cidrs,
			geo_sites,
			geo_ips
		FROM routing_profile_rules
		WHERE routing_profile_id = $1::uuid
		  AND enabled = TRUE
		ORDER BY priority ASC, created_at ASC
	`, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rules := make([]RoutingProfileRule, 0)
	for rows.Next() {
		rule, err := scanRoutingProfileRule(rows)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

const accountFilterSQL = `
		WHERE ($1 = '' OR status = $1)
		  AND ($2 = '' OR server_id = NULLIF($2, '')::uuid)
		  AND (
			$3 = ''
			OR display_name ILIKE '%' || $3 || '%'
			OR email ILIKE '%' || $3 || '%'
			OR EXISTS (
				SELECT 1
				FROM vpn_account_notes n
				WHERE n.vpn_account_id = vpn_accounts.id
				  AND n.notes ILIKE '%' || $3 || '%'
			)
			OR id = $4::uuid
			OR vless_uuid = $4::uuid
		  )
`

const accountSelect = `
	SELECT
		id::text,
		display_name,
		COALESCE(email, ''),
		status,
		expires_at,
		max_devices,
		COALESCE(server_id::text, ''),
		COALESCE(vless_uuid::text, ''),
		created_at,
		updated_at,
		config_updated_at
	FROM vpn_accounts`

const subscriptionTokenSelect = `
	SELECT
		id::text,
		vpn_account_id::text,
		token_hash,
		status,
		expires_at,
		last_used_at,
		revoked_at,
		created_at,
		updated_at
	FROM vpn_subscription_tokens`

type scanner interface {
	Scan(dest ...any) error
}

func scanAccount(row scanner) (Account, error) {
	var account Account
	var expiresAt sql.NullTime
	var maxDevices sql.NullInt32
	err := row.Scan(
		&account.ID,
		&account.DisplayName,
		&account.Email,
		&account.Status,
		&expiresAt,
		&maxDevices,
		&account.ServerID,
		&account.VLESSUUID,
		&account.CreatedAt,
		&account.UpdatedAt,
		&account.ConfigUpdatedAt,
	)
	if err != nil {
		return Account{}, err
	}
	if expiresAt.Valid {
		account.ExpiresAt = &expiresAt.Time
	}
	if maxDevices.Valid {
		value := int(maxDevices.Int32)
		account.MaxDevices = &value
	}
	return account, nil
}

func scanSubscriptionToken(row scanner) (SubscriptionToken, error) {
	var token SubscriptionToken
	var expiresAt, lastUsedAt, revokedAt sql.NullTime
	err := row.Scan(
		&token.ID,
		&token.VPNAccountID,
		&token.TokenHash,
		&token.Status,
		&expiresAt,
		&lastUsedAt,
		&revokedAt,
		&token.CreatedAt,
		&token.UpdatedAt,
	)
	if err != nil {
		return SubscriptionToken{}, err
	}
	if expiresAt.Valid {
		token.ExpiresAt = &expiresAt.Time
	}
	if lastUsedAt.Valid {
		token.LastUsedAt = &lastUsedAt.Time
	}
	if revokedAt.Valid {
		token.RevokedAt = &revokedAt.Time
	}
	return token, nil
}

func scanSubscriptionProfile(row scanner) (SubscriptionProfile, error) {
	var profile SubscriptionProfile
	var expiresAt sql.NullTime
	var maxDevices sql.NullInt32
	var serverID, serverName, serverHostname, serverPublicIP, serverLocation, serverProvider sql.NullString
	var vlessPort sql.NullInt32
	var vlessFlow, vlessNetwork, realityPublicKey, realityShortID, realityServerName sql.NullString
	var wireGuardPrivateKey, wireGuardPublicKey, wireGuardAddress, hysteria2Password, shadowsocksUserKey sql.NullString
	var vpnProtocol, serverWireGuardAddress, wireGuardDNS, serverWireGuardPublicKey sql.NullString
	var wireGuardPort sql.NullInt32
	var hysteria2Port sql.NullInt32
	var hysteria2Domain, hysteria2ACMEEmail sql.NullString
	var shadowsocksMethod, shadowsocksServerKey, mtprotoSecret, mtprotoFrontingDomain sql.NullString
	var shadowsocksPort, mtprotoPort sql.NullInt32

	err := row.Scan(
		&profile.Account.ID,
		&profile.Account.DisplayName,
		&profile.Account.Email,
		&profile.Account.Status,
		&expiresAt,
		&maxDevices,
		&profile.Account.ServerID,
		&profile.Account.VLESSUUID,
		&wireGuardPrivateKey,
		&wireGuardPublicKey,
		&wireGuardAddress,
		&hysteria2Password,
		&shadowsocksUserKey,
		&profile.Account.CreatedAt,
		&profile.Account.UpdatedAt,
		&profile.Account.ConfigUpdatedAt,
		&serverID,
		&serverName,
		&serverHostname,
		&serverPublicIP,
		&serverLocation,
		&serverProvider,
		&vlessPort,
		&vlessFlow,
		&vlessNetwork,
		&realityPublicKey,
		&realityShortID,
		&realityServerName,
		&vpnProtocol,
		&wireGuardPort,
		&serverWireGuardAddress,
		&wireGuardDNS,
		&serverWireGuardPublicKey,
		&hysteria2Port,
		&hysteria2Domain,
		&hysteria2ACMEEmail,
		&shadowsocksPort,
		&shadowsocksMethod,
		&shadowsocksServerKey,
		&mtprotoPort,
		&mtprotoSecret,
		&mtprotoFrontingDomain,
	)
	if err != nil {
		return SubscriptionProfile{}, err
	}
	if expiresAt.Valid {
		profile.Account.ExpiresAt = &expiresAt.Time
	}
	if maxDevices.Valid {
		value := int(maxDevices.Int32)
		profile.Account.MaxDevices = &value
	}
	profile.Credentials.VLESS.UUID = profile.Account.VLESSUUID
	if serverID.Valid {
		server := SubscriptionServer{
			ID:                serverID.String,
			Name:              serverName.String,
			Hostname:          serverHostname.String,
			PublicIP:          serverPublicIP.String,
			Location:          serverLocation.String,
			Provider:          serverProvider.String,
			VLESSPort:         defaultSingBoxServerPort,
			VLESSFlow:         vlessFlow.String,
			VLESSNetwork:      vlessNetwork.String,
			RealityPublicKey:  realityPublicKey.String,
			RealityShortID:    realityShortID.String,
			RealityServerName: realityServerName.String,
			VPNProtocol:       vpnProtocol.String,
			WireGuardPort:      int(wireGuardPort.Int32),
			WireGuardAddress:   serverWireGuardAddress.String,
			WireGuardDNS:       wireGuardDNS.String,
			WireGuardPublicKey: serverWireGuardPublicKey.String,
			Hysteria2Port:       int(hysteria2Port.Int32),
			Hysteria2Domain:     hysteria2Domain.String,
			Hysteria2ACMEEmail:  hysteria2ACMEEmail.String,
			ShadowsocksPort:      int(shadowsocksPort.Int32),
			ShadowsocksMethod:    shadowsocksMethod.String,
			ShadowsocksServerKey: shadowsocksServerKey.String,
			MTProtoPort:           int(mtprotoPort.Int32),
			MTProtoSecret:         mtprotoSecret.String,
			MTProtoFrontingDomain: mtprotoFrontingDomain.String,
		}
		if vlessPort.Valid {
			server.VLESSPort = int(vlessPort.Int32)
		}
		profile.Server = &server
		profile.Credentials.VLESS.Flow = server.VLESSFlow
		profile.Credentials.VLESS.Network = server.VLESSNetwork
		profile.Credentials.Reality = RealityCredentials{
			PublicKey:  server.RealityPublicKey,
			ShortID:    server.RealityShortID,
			ServerName: server.RealityServerName,
		}
		profile.Credentials.WireGuard = WireGuardCredentials{
			PrivateKey: wireGuardPrivateKey.String,
			PublicKey:  wireGuardPublicKey.String,
			Address:    wireGuardAddress.String,
		}
		profile.Credentials.Hysteria2 = Hysteria2Credentials{
			Username: strings.ToLower(profile.Account.ID),
			Password: hysteria2Password.String,
		}
		profile.Credentials.Shadowsocks = ShadowsocksCredentials{
			Username: strings.ToLower(profile.Account.ID),
			UserKey:  shadowsocksUserKey.String,
		}
	}
	return profile, nil
}

func scanRoutingProfile(row scanner) (RoutingProfile, error) {
	var profile RoutingProfile
	err := row.Scan(
		&profile.ID,
		&profile.Name,
		&profile.Description,
		&profile.IsDefault,
	)
	if err != nil {
		return RoutingProfile{}, err
	}
	return profile, nil
}

func scanRoutingProfileRule(row scanner) (RoutingProfileRule, error) {
	var rule RoutingProfileRule
	err := row.Scan(
		&rule.ID,
		&rule.Name,
		&rule.Priority,
		&rule.Action,
		&rule.Domains,
		&rule.DomainSuffixes,
		&rule.DomainKeywords,
		&rule.IPCIDRs,
		&rule.GeoSites,
		&rule.GeoIPs,
	)
	if err != nil {
		return RoutingProfileRule{}, err
	}
	return rule, nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
