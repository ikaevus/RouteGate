package client

import (
	"context"
	"encoding/json"
	"net/url"
	"time"

	"github.com/ikaevus/routegate/agent/internal/tasks"
)

type RecoveryCleanedCommitReceipt = tasks.RuntimeRecoveryCleanedCommitReceipt

func validCleanedCommitReceipt(r RecoveryCleanedCommitReceipt, p tasks.RuntimeRecoveryCleanedCommitProof, hash string) bool {
	return r.SchemaVersion == 1 && r.Recorded && r.OperationID == p.OperationID && r.CleanedIntentHash == p.CleanedIntentHash && r.ProofHash == hash && r.RequestHash == hash
}

// Dormant original-owner submission only. The intent must already exist; this
// cannot run cleanup, remove barriers, renew authority or reconstruct an owner.
func (c *Client) SubmitRecoveryCleanedCommit(ctx context.Context, token string, v VerifiedRecoveryReleaseAuthorization, owner *tasks.RuntimeRecoveryFinalization) (RecoveryCleanedCommitReceipt, error) {
	var zero, result RecoveryCleanedCommitReceipt
	e := c.withRecoverySourceCleanupReadiness(ctx, token, v, owner, func(bounded context.Context, g *tasks.RuntimeRecoveryPreparedReleaseGuard) error {
		p, e := g.PinCleanedCommitProof(bounded)
		if e != nil {
			return e
		}
		wire, e := json.Marshal(p)
		if e != nil {
			return e
		}
		hash := recoveryReadHash(wire)
		check := func() bool {
			fresh, e := g.CleanedCommitProof(bounded)
			now := time.Now().UTC()
			return e == nil && bounded.Err() == nil && fresh == p && p.SchemaVersion == 1 && p.Purpose == tasks.RuntimeRecoveryCleanedCommitPurpose && p.OperationID == v.Authorization.OperationID && p.AgentCredentialGeneration == v.Binding.AgentCredentialGeneration && p.AgentCredentialGeneration == v.Authorization.AgentCredentialGeneration && p.AuthorizationHash == v.AuthorizationHash && p.Observation == v.Authorization.Expected && !p.IntentAt.Before(v.Authorization.IssuedAt) && !p.ObservedAt.Before(p.IntentAt) && !p.ObservedAt.After(now) && !now.Before(v.Authorization.IssuedAt) && now.Before(v.Authorization.ExpiresAt)
		}
		path := "/api/v1/agent/recovery-observations/" + url.PathEscape(p.OperationID) + "/cleaned-commit"
		result, e = exchangePinnedRecoveryEvidence(bounded, c, token, path, wire, check, func(r RecoveryCleanedCommitReceipt) bool { return validCleanedCommitReceipt(r, p, hash) })
		if e != nil {
			return e
		}
		return g.RecordCleanedCommitReceipt(bounded, result)
	})
	if e != nil {
		return zero, ErrRecoveryCleanedCommitUnconfirmed
	}
	return result, nil
}

// Read-only current-credential historical reconciliation. A missing local ack
// stays missing, including after a crash or expiry. No runtime observation.
func (c *Client) ReconcileRecoveryCleanedCommitEvidence(ctx context.Context, token, agentID, serverID, dir, operation string) (RecoveryCleanedCommitReceipt, error) {
	var zero, result RecoveryCleanedCommitReceipt
	b, e := c.verifyRecoveryIdentity(ctx, token, agentID, serverID)
	if e != nil {
		return zero, ErrRecoveryCleanedCommitUnconfirmed
	}
	e = tasks.WithRecoveryCleanedCommitEvidenceHistory(ctx, dir, operation, b, func(p tasks.RuntimeRecoveryCleanedCommitProof, local RecoveryCleanedCommitReceipt) error {
		var e error
		result, e = c.readRecoveryCleanedCommitReceipt(ctx, token, p)
		if e != nil || local.SchemaVersion != 0 && local != result {
			return ErrRecoveryCleanedCommitUnconfirmed
		}
		return nil
	})
	if e != nil {
		return zero, ErrRecoveryCleanedCommitUnconfirmed
	}
	return result, nil
}
