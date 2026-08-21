package wireguard

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// EnsureAccountPeerCredentials prepares missing WireGuard material for one
// account before its client protocol preference is persisted. Locking the
// server serializes address allocation with the normal full-server allocator.
func EnsureAccountPeerCredentials(ctx context.Context, pool *pgxpool.Pool, serverID string, accountID string) error {
	serverID = strings.TrimSpace(serverID)
	accountID = strings.TrimSpace(accountID)
	if serverID == "" || accountID == "" {
		return nil
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var serverAddress string
	if err := tx.QueryRow(ctx, `
		SELECT wireguard_address::text
		FROM servers
		WHERE id = $1::uuid
		FOR UPDATE
	`, serverID).Scan(&serverAddress); err != nil {
		return err
	}

	var privateKey, publicKey, address string
	if err := tx.QueryRow(ctx, `
		SELECT
			COALESCE(wireguard_private_key, ''),
			COALESCE(wireguard_public_key, ''),
			COALESCE(wireguard_address::text, '')
		FROM vpn_accounts
		WHERE id = $1::uuid
		  AND server_id = $2::uuid
	`, accountID, serverID).Scan(&privateKey, &publicKey, &address); err != nil {
		return err
	}

	used := []string{}
	rows, err := tx.Query(ctx, `
		SELECT COALESCE(wireguard_address::text, '')
		FROM vpn_accounts
		WHERE server_id = $1::uuid
		  AND id <> $2::uuid
		ORDER BY created_at ASC, id ASC
	`, serverID, accountID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var candidate string
		if err := rows.Scan(&candidate); err != nil {
			rows.Close()
			return err
		}
		if candidate != "" && PeerAddressInServerPrefix(serverAddress, candidate) {
			used = append(used, candidate)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	privateKey = strings.TrimSpace(privateKey)
	publicKey = strings.TrimSpace(publicKey)
	address = strings.TrimSpace(address)
	if !PeerAddressInServerPrefix(serverAddress, address) {
		address = ""
	}
	if privateKey == "" {
		keypair, err := GenerateKeypair()
		if err != nil {
			return err
		}
		privateKey, publicKey = keypair.PrivateKey, keypair.PublicKey
	} else if publicKey == "" {
		publicKey, err = PublicKeyFromPrivate(privateKey)
		if err != nil {
			return err
		}
	}
	if address == "" {
		address, err = NextPeerAddress(serverAddress, used)
		if err != nil {
			return err
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE vpn_accounts
		SET
			wireguard_private_key = $2,
			wireguard_public_key = $3,
			wireguard_address = $4::inet,
			updated_at = now(),
			config_updated_at = now()
		WHERE id = $1::uuid
	`, accountID, privateKey, publicKey, address); err != nil {
		return err
	}

	return tx.Commit(ctx)
}
