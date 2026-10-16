package client

import (
	"context"
	"time"

	"github.com/ikaevus/routegate/agent/internal/tasks"
)

// Local release intent is not sent to or attested by the Manager. Only the
// existing terminal storage receipt is independently verified at the Manager.
type RecoveryPreparedReleaseReadback struct {
	ReleaseIntentHash      string
	PreparedAt             time.Time
	AuthorizationExpiresAt time.Time
	TerminalReceipt        RecoveryTerminalReceipt
}

// Dormant read-only prepared-phase reconciliation. Independently authenticate
// current credentials/generation, hold the original admission flock through
// exact /terminal/verify, and expose zero on any transport or snapshot failure.
// Never submit new evidence, renew authority, resume an owner, write or release.
func (c *Client) ReconcileRecoveryPreparedRelease(ctx context.Context, token, agentID, serverID, dir string) (RecoveryPreparedReleaseReadback, error) {
	var zero, result RecoveryPreparedReleaseReadback
	b, e := c.verifyRecoveryIdentity(ctx, token, agentID, serverID)
	if e != nil {
		return zero, ErrRecoveryTerminalUnconfirmed
	}
	e = tasks.WithHistoricalRecoveryPreparedRelease(ctx, dir, b, func(h tasks.RuntimeRecoveryPreparedReleaseHistory) error {
		r, e := c.verifyHistoricalRecoveryTerminal(ctx, token, h.TerminalProof)
		if e != nil {
			return e
		}
		result = RecoveryPreparedReleaseReadback{h.ReleaseIntentHash, h.PreparedAt, h.AuthorizationExpiresAt, r}
		return nil
	})
	if e != nil {
		return zero, ErrRecoveryTerminalUnconfirmed
	}
	return result, nil
}
