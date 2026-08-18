package wireguard

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// EnsureServerPeerCredentials fills only missing WireGuard account material.
// The server row lock serializes address allocation for a peer pool.
func EnsureServerPeerCredentials(ctx context.Context, pool *pgxpool.Pool, serverID string) error {
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		return nil
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var protocol, serverAddress string
	if err := tx.QueryRow(ctx, `
		SELECT vpn_protocol, wireguard_address::text
		FROM servers
		WHERE id = $1::uuid
		FOR UPDATE
	`, serverID).Scan(&protocol, &serverAddress); err != nil {
		return err
	}
	if protocol != "wireguard" {
		return tx.Commit(ctx)
	}

	rows, err := tx.Query(ctx, `
		SELECT
			id::text,
			COALESCE(wireguard_private_key, ''),
			COALESCE(wireguard_public_key, ''),
			COALESCE(wireguard_address::text, '')
		FROM vpn_accounts
		WHERE server_id = $1::uuid
		ORDER BY created_at ASC, id ASC
	`, serverID)
	if err != nil {
		return err
	}
	type peer struct{ id, privateKey, publicKey, address string }
	peers := []peer{}
	used := []string{}
	for rows.Next() {
		var item peer
		if err := rows.Scan(&item.id, &item.privateKey, &item.publicKey, &item.address); err != nil {
			rows.Close()
			return err
		}
		peers = append(peers, item)
		if item.address != "" {
			used = append(used, item.address)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, item := range peers {
		privateKey := strings.TrimSpace(item.privateKey)
		publicKey := strings.TrimSpace(item.publicKey)
		address := strings.TrimSpace(item.address)
		if privateKey == "" {
			keypair, keyErr := GenerateKeypair()
			if keyErr != nil {
				return keyErr
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
			used = append(used, address)
		}
		if privateKey == item.privateKey && publicKey == item.publicKey && address == item.address {
			continue
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
		`, item.id, privateKey, publicKey, address); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}
