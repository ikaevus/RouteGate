package vpnaccounts

import (
	"context"
	"strings"

	wgcredentials "github.com/ikaevus/routegate/backend/internal/wireguard"
)

type clientProtocolPreparer interface {
	PrepareClientProtocol(context.Context, string, string, string) error
}

func (r *Repository) PrepareClientProtocol(ctx context.Context, accountID string, serverID string, protocol string) error {
	if strings.ToLower(strings.TrimSpace(protocol)) != ClientProtocolWireGuard {
		return nil
	}
	if err := r.ensureWireGuardServerKeypair(ctx, serverID); err != nil {
		return err
	}
	return wgcredentials.EnsureAccountPeerCredentials(ctx, r.pool, serverID, accountID)
}

// ensureWireGuardServerKeypair makes an account-level WireGuard selection safe
// even when the node default is still VLESS and the administrator has never
// opened the node-level recommended WireGuard setup. The client renderer needs
// the server public key immediately, while the subsequent config deployment
// needs the matching private key.
func (r *Repository) ensureWireGuardServerKeypair(ctx context.Context, serverID string) error {
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		return nil
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var privateKey, publicKey string
	if err := tx.QueryRow(ctx, `
		SELECT
			COALESCE(wireguard_private_key, ''),
			COALESCE(wireguard_public_key, '')
		FROM servers
		WHERE id = $1::uuid
		FOR UPDATE
	`, serverID).Scan(&privateKey, &publicKey); err != nil {
		return err
	}

	privateKey = strings.TrimSpace(privateKey)
	publicKey = strings.TrimSpace(publicKey)
	changed := false
	if privateKey == "" {
		keypair, err := wgcredentials.GenerateKeypair()
		if err != nil {
			return err
		}
		privateKey, publicKey = keypair.PrivateKey, keypair.PublicKey
		changed = true
	} else if publicKey == "" {
		publicKey, err = wgcredentials.PublicKeyFromPrivate(privateKey)
		if err != nil {
			return err
		}
		changed = true
	}

	if changed {
		if _, err := tx.Exec(ctx, `
			UPDATE servers
			SET
				wireguard_private_key = $2,
				wireguard_public_key = $3,
				protocol_updated_at = now(),
				updated_at = now()
			WHERE id = $1::uuid
		`, serverID, privateKey, publicKey); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}
