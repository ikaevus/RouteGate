package client

import (
	"context"
	"encoding/json"
	"net/url"

	"github.com/ikaevus/routegate/agent/internal/tasks"
)

// ReconcileRecoveryCommit is dormant and read-only. It independently verifies
// current credentials, reads exact private pinned bytes under flock and calls
// only historical /commit/verify. It does not fetch/consume a grant, submit a new
// commit, persist a receipt, resume an owner or infer current runtime readiness.
// Expired authority can prove historical storage only. All uncertainty remains
// unconfirmed, including absent evidence or rotated credentials.
func (c *Client) ReconcileRecoveryCommit(ctx context.Context, token, agentID, serverID, dir string) (RecoveryCommitReceipt, error) {
	var zero, result RecoveryCommitReceipt
	binding, err := c.verifyRecoveryIdentity(ctx, token, agentID, serverID)
	if err != nil {
		return zero, ErrRecoveryCommitUnconfirmed
	}
	err = tasks.WithHistoricalRecoverySubmission(ctx, dir, binding, func(p tasks.RuntimeRecoveryCommitProof) error {
		wire, e := json.Marshal(p)
		if e != nil {
			return ErrRecoveryCommitUnconfirmed
		}
		path := "/api/v1/agent/recovery-observations/" + url.PathEscape(p.OperationID) + "/commit/verify"
		if c.recoveryRead(ctx, path, token, wire, &result) != nil || result.SchemaVersion != 1 || !result.Recorded || result.OperationID != p.OperationID || result.GrantHash != p.GrantHash || result.ConsumptionHash != p.ConsumptionHash || result.ProofHash != recoveryReadHash(wire) || result.RequestHash != result.ProofHash {
			return ErrRecoveryCommitUnconfirmed
		}
		return nil
	})
	if err != nil {
		return zero, ErrRecoveryCommitUnconfirmed
	}
	return result, nil
}
