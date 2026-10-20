package client

import (
	"context"

	"github.com/ikaevus/routegate/agent/internal/tasks"
)

// Dormant copy-only retention; readiness, runtime checks and archive publication
// share the original owner mutex/flock and original authorization deadline.
func (c *Client) ArchiveRecoveryCleanedCommitHistory(ctx context.Context, token string, v VerifiedRecoveryReleaseAuthorization, owner *tasks.RuntimeRecoveryFinalization) (string, error) {
	var hash string
	e := c.withRecoveryCleanedCommitReadiness(ctx, token, v, owner, func(bounded context.Context, g *tasks.RuntimeRecoveryPreparedReleaseGuard) error {
		var e error
		hash, e = g.ArchiveCleanedCommitHistory(bounded)
		return e
	})
	if e != nil {
		return "", ErrRecoveryCleanedCommitUnconfirmed
	}
	return hash, nil
}

// Read-only current-authenticated historical reconciliation. This never adopts
// the archive as a live owner or recreates missing source records/acknowledgements.
func (c *Client) ReconcileArchivedRecoveryCleanedCommit(ctx context.Context, token, agentID, serverID, dir, operation string) (string, error) {
	b, e := c.verifyRecoveryIdentity(ctx, token, agentID, serverID)
	if e != nil {
		return "", ErrRecoveryCleanedCommitUnconfirmed
	}
	var hash string
	e = tasks.WithArchivedRecoveryCleanedCommit(ctx, dir, operation, b, func(h tasks.RuntimeRecoveryCleanedCommitArchivedHistory) error {
		r, e := c.readRecoveryCleanedCommitReceipt(ctx, token, h.Proof)
		if e != nil || r != h.Receipt {
			return ErrRecoveryCleanedCommitUnconfirmed
		}
		hash = h.ManifestHash
		return nil
	})
	if e != nil {
		return "", ErrRecoveryCleanedCommitUnconfirmed
	}
	return hash, nil
}
