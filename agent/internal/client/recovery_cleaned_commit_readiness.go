package client

import (
	"context"
	"encoding/json"
	"net/url"

	"github.com/ikaevus/routegate/agent/internal/tasks"
)

func (c *Client) readRecoveryCleanedCommitReceipt(ctx context.Context, token string, p tasks.RuntimeRecoveryCleanedCommitProof) (RecoveryCleanedCommitReceipt, error) {
	var zero, result RecoveryCleanedCommitReceipt
	wire, e := json.Marshal(p)
	if e != nil {
		return zero, ErrRecoveryCleanedCommitUnconfirmed
	}
	path := "/api/v1/agent/recovery-observations/" + url.PathEscape(p.OperationID) + "/cleaned-commit/verify"
	if c.recoveryRead(ctx, path, token, wire, &result) != nil || !validCleanedCommitReceipt(result, p, recoveryReadHash(wire)) {
		return zero, ErrRecoveryCleanedCommitUnconfirmed
	}
	return result, nil
}

// Read-only point-in-time readiness, never a transferable commit grant. Require
// the live owner's already durable acknowledgement, exact Manager readback and
// CURRENT original authorization while all HTTP shares the original lock.
func (c *Client) PlanRecoveryCleanedCommit(ctx context.Context, token string, v VerifiedRecoveryReleaseAuthorization, owner *tasks.RuntimeRecoveryFinalization) (tasks.RuntimeRecoveryCleanedCommitPlan, error) {
	var zero, result tasks.RuntimeRecoveryCleanedCommitPlan
	e := c.withRecoveryCleanedCommitReadiness(ctx, token, v, owner, func(bounded context.Context, g *tasks.RuntimeRecoveryPreparedReleaseGuard) error {
		var e error
		result, e = g.PlanCleanedCommit(bounded)
		return e
	})
	if e != nil {
		return zero, ErrRecoveryCleanedCommitUnconfirmed
	}
	return result, nil
}

func (c *Client) withRecoveryCleanedCommitReadiness(ctx context.Context, token string, v VerifiedRecoveryReleaseAuthorization, owner *tasks.RuntimeRecoveryFinalization, fn func(context.Context, *tasks.RuntimeRecoveryPreparedReleaseGuard) error) error {
	return c.withRecoverySourceCleanupReadiness(ctx, token, v, owner, func(bounded context.Context, g *tasks.RuntimeRecoveryPreparedReleaseGuard) error {
		p, local, e := g.AcknowledgedCleanedCommit(bounded)
		if e != nil {
			return e
		}
		remote, e := c.readRecoveryCleanedCommitReceipt(bounded, token, p)
		if e != nil || remote != local {
			return ErrRecoveryCleanedCommitUnconfirmed
		}
		fresh, ack, e := g.AcknowledgedCleanedCommit(bounded)
		if e != nil || fresh != p || ack != local {
			return ErrRecoveryCleanedCommitUnconfirmed
		}
		// Historical storage may still be readable after reservation cancellation
		// or human authority loss. Reauthenticate the original authority AFTER it.
		prepared, e := g.CleanupPreparedProof(bounded)
		if e != nil {
			return e
		}
		if _, e = c.checkRecoveryFenceReleaseReadiness(bounded, token, v, prepared, g.CleanupPreparedProof); e != nil {
			return e
		}
		return fn(bounded, g)
	})
}
