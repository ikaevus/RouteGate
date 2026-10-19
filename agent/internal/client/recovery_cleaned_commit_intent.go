package client

import (
	"context"
	"errors"

	"github.com/ikaevus/routegate/agent/internal/tasks"
)

var ErrRecoveryCleanedCommitUnconfirmed = errors.New("recovery cleaned commit intent was not confirmed")

// Dormant post-cleanup commitment only, no barrier deletion or Manager state
// transition. Reuse current authenticated identity, prepared storage, original
// authorization and retention verification under the original owner/deadline.
func (c *Client) PrepareRecoveryCleanedCommit(ctx context.Context, token string, v VerifiedRecoveryReleaseAuthorization, owner *tasks.RuntimeRecoveryFinalization) (tasks.RuntimeRecoveryCleanedCommitIntent, error) {
	var zero, result tasks.RuntimeRecoveryCleanedCommitIntent
	e := c.withRecoverySourceCleanupReadiness(ctx, token, v, owner, func(bounded context.Context, g *tasks.RuntimeRecoveryPreparedReleaseGuard) error {
		var err error
		result, err = g.PrepareCleanedCommit(bounded)
		return err
	})
	if e != nil {
		return zero, ErrRecoveryCleanedCommitUnconfirmed
	}
	return result, nil
}

type RecoveryCleanedCommitHistory struct {
	Intent  tasks.RuntimeRecoveryCleanedCommitIntent
	Receipt RecoveryRetentionReceipt
}

// Historical observation only; not proof of completed fsync or an authorized
// release. Reopening history never reconstructs the live original owner.
func (c *Client) ReconcileRecoveryCleanedCommit(ctx context.Context, token, agentID, serverID, dir, operation string) (RecoveryCleanedCommitHistory, error) {
	var zero, result RecoveryCleanedCommitHistory
	b, e := c.verifyRecoveryIdentity(ctx, token, agentID, serverID)
	if e != nil {
		return zero, ErrRecoveryCleanedCommitUnconfirmed
	}
	e = tasks.WithRecoveryCleanedCommitHistory(ctx, dir, operation, b, func(p tasks.RuntimeRecoveryCleanedCommitIntent, h tasks.RuntimeRecoveryRetentionHistory) error {
		r, e := c.readRecoveryRetentionReceipt(ctx, token, h.Proof)
		if e != nil || r != h.LocalReceipt {
			return ErrRecoveryCleanedCommitUnconfirmed
		}
		result = RecoveryCleanedCommitHistory{p, r}
		return nil
	})
	if e != nil {
		return zero, ErrRecoveryCleanedCommitUnconfirmed
	}
	return result, nil
}
