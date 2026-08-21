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
	return wgcredentials.EnsureAccountPeerCredentials(ctx, r.pool, serverID, accountID)
}
