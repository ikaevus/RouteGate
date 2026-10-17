package client

import (
	"context"
	"encoding/json"
	"net/url"

	"github.com/ikaevus/routegate/agent/internal/tasks"
)

// Dormant historical readback only. Authenticate current credentials/generation
// before reading private history. Retain its flock and snapshots through the
// exact verify request. Never submit new evidence, write a missing receipt,
// capture runtime, renew authority, resume an owner or release any barrier.
func (c *Client) ReconcileRecoveryPreparedSubmission(ctx context.Context, token, agentID, serverID, dir string) (RecoveryPreparedReleaseReceipt, error) {
	var zero, result RecoveryPreparedReleaseReceipt
	b, e := c.verifyRecoveryIdentity(ctx, token, agentID, serverID)
	if e != nil {
		return zero, ErrRecoveryPreparedReleaseUnconfirmed
	}
	e = tasks.WithHistoricalRecoveryPreparedSubmission(ctx, dir, b, func(h tasks.RuntimeRecoveryPreparedSubmissionHistory) error {
		wire, e := json.Marshal(h.Proof)
		if e != nil {
			return ErrRecoveryPreparedReleaseUnconfirmed
		}
		path := "/api/v1/agent/recovery-observations/" + url.PathEscape(h.Proof.OperationID) + "/prepared-release/verify"
		hash := recoveryReadHash(wire)
		if c.recoveryRead(ctx, path, token, wire, &result) != nil || result.SchemaVersion != 1 || !result.Recorded || result.OperationID != h.Proof.OperationID || result.ReleaseIntentHash != h.Proof.ReleaseIntentHash || result.ProofHash != hash || result.RequestHash != hash || (h.LocalReceipt.SchemaVersion != 0 && result != h.LocalReceipt) {
			return ErrRecoveryPreparedReleaseUnconfirmed
		}
		return nil
	})
	if e != nil {
		return zero, ErrRecoveryPreparedReleaseUnconfirmed
	}
	return result, nil
}
