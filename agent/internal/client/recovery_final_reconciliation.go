package client

import (
	"context"
	"encoding/json"
	"net/url"

	"github.com/ikaevus/routegate/agent/internal/tasks"
)

// Dormant read-only crash reconciliation. Authenticate current credentials and
// generation, retain exact private history under flock through historical
// readback only. No new proof submission, receipt write, runtime observation,
// authority renewal, owner resume, fence or reservation release occurs.
func (c *Client) ReconcileRecoveryFinalization(ctx context.Context, token, agentID, serverID, dir string) (RecoveryFinalizationReceipt, error) {
	var zero, result RecoveryFinalizationReceipt
	binding, err := c.verifyRecoveryIdentity(ctx, token, agentID, serverID)
	if err != nil {
		return zero, ErrRecoveryFinalizationUnconfirmed
	}
	err = tasks.WithHistoricalRecoveryFinalization(ctx, dir, binding, func(p tasks.RuntimeRecoveryFinalizationProof) error {
		wire, e := json.Marshal(p)
		if e != nil {
			return ErrRecoveryFinalizationUnconfirmed
		}
		path := "/api/v1/agent/recovery-observations/" + url.PathEscape(p.OperationID) + "/finalization/verify"
		if c.recoveryRead(ctx, path, token, wire, &result) != nil || result.SchemaVersion != 1 || !result.Recorded || result.OperationID != p.OperationID || result.AuthorizationHash != p.AuthorizationHash || result.FinalConsumptionHash != p.FinalConsumptionHash || result.ProofHash != recoveryReadHash(wire) || result.RequestHash != result.ProofHash {
			return ErrRecoveryFinalizationUnconfirmed
		}
		return nil
	})
	if err != nil {
		return zero, ErrRecoveryFinalizationUnconfirmed
	}
	return result, nil
}
