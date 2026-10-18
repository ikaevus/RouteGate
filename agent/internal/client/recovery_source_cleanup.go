package client

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/ikaevus/routegate/agent/internal/tasks"
)

var ErrRecoverySourceCleanupUnconfirmed = errors.New("recovery source cleanup was not confirmed")

// Dormant local duplicate cleanup, never fence release. Reuse original current
// authority and retention readback across the same owner lock and deadline.
func (c *Client) CleanupRecoverySources(ctx context.Context, token string, v VerifiedRecoveryReleaseAuthorization, owner *tasks.RuntimeRecoveryFinalization) (tasks.RuntimeRecoverySourceCleanupStatus, error) {
	var zero, result tasks.RuntimeRecoverySourceCleanupStatus
	started := time.Now().UTC()
	wire, e := json.Marshal(v.Authorization)
	if e != nil || token == "" || v.Binding.ManagerURL != c.managerURL || recoveryReadHash(wire) != v.AuthorizationHash {
		return zero, ErrRecoverySourceCleanupUnconfirmed
	}
	bounded, cancel := context.WithDeadline(ctx, v.Authorization.ExpiresAt)
	defer cancel()
	e = owner.WithSourceCleanupSubmission(bounded, v.Binding, v.AuthorizationHash, func(g *tasks.RuntimeRecoveryPreparedReleaseGuard) error {
		p, e := g.CleanupPreparedProof(bounded)
		if e != nil {
			return e
		}
		if _, e = c.checkRecoveryFenceReleaseReadiness(bounded, token, v, p, g.CleanupPreparedProof); e != nil {
			return e
		}
		r, e := g.CleanupRetentionProof(bounded)
		if e != nil {
			return e
		}
		if _, e = c.readRecoveryRetentionReceipt(bounded, token, r); e != nil {
			return e
		}
		fresh, e := g.CleanupRetentionProof(bounded)
		if e != nil || fresh != r {
			return ErrRecoverySourceCleanupUnconfirmed
		}
		result, e = g.CleanupArchivedSources(bounded)
		return e
	})
	finished := time.Now().UTC()
	if e != nil || bounded.Err() != nil || finished.Before(started) || finished.Before(v.Authorization.IssuedAt) || !finished.Before(v.Authorization.ExpiresAt) {
		return zero, ErrRecoverySourceCleanupUnconfirmed
	}
	return result, nil
}

type RecoverySourceCleanupHistory struct {
	Status  tasks.RuntimeRecoverySourceCleanupStatus
	Receipt RecoveryRetentionReceipt
}

// Current authentication plus exact storage readback of historical prefix state.
// Never resumes removal, repairs files, captures runtime or grants new authority.
func (c *Client) ReconcileRecoverySourceCleanup(ctx context.Context, token, agentID, serverID, dir, operation string) (RecoverySourceCleanupHistory, error) {
	var zero, result RecoverySourceCleanupHistory
	b, e := c.verifyRecoveryIdentity(ctx, token, agentID, serverID)
	if e != nil {
		return zero, ErrRecoverySourceCleanupUnconfirmed
	}
	e = tasks.WithRecoverySourceCleanupHistory(ctx, dir, operation, b, func(s tasks.RuntimeRecoverySourceCleanupStatus, h tasks.RuntimeRecoveryRetentionHistory) error {
		r, e := c.readRecoveryRetentionReceipt(ctx, token, h.Proof)
		if e != nil || r != h.LocalReceipt {
			return ErrRecoverySourceCleanupUnconfirmed
		}
		result = RecoverySourceCleanupHistory{s, r}
		return nil
	})
	if e != nil {
		return zero, ErrRecoverySourceCleanupUnconfirmed
	}
	return result, nil
}
