package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"time"

	"github.com/ikaevus/routegate/agent/internal/tasks"
)

// Unconfirmed includes uncertain storage. It never means safe to clear state.
var ErrRecoveryFinalizationUnconfirmed = errors.New("recovery finalization storage was not confirmed")

// Historical storage receipt only, never permission to release an Agent fence.
type RecoveryFinalizationReceipt = tasks.RuntimeRecoveryFinalizationReceipt

// SubmitRecoveryFinalization is dormant: no Runner invokes it. Caller owns the live
// lease and must defer Close. We never close, unlock, delete or resume state.
func (c *Client) SubmitRecoveryFinalization(ctx context.Context, token string, verified VerifiedRecoveryReleaseAuthorization, owner *tasks.RuntimeRecoveryFinalization) (RecoveryFinalizationReceipt, error) {
	var zero, result RecoveryFinalizationReceipt
	wire, err := json.Marshal(verified.Authorization)
	if err != nil || token == "" || verified.Binding.ManagerURL != c.managerURL {
		return zero, ErrRecoveryFinalizationUnconfirmed
	}
	hash := recoveryReadHash(wire)
	if hash != verified.AuthorizationHash {
		return zero, ErrRecoveryFinalizationUnconfirmed
	}
	bounded, cancel := context.WithDeadline(ctx, verified.Authorization.ExpiresAt)
	defer cancel()
	err = owner.WithSubmission(bounded, verified.Binding, hash, func(guard *tasks.RuntimeRecoveryFinalizationGuard) error {
		frozen, e := guard.PinProof(bounded)
		if e != nil {
			return ErrRecoveryFinalizationUnconfirmed
		}
		result, e = c.submitRecoveryFinalization(bounded, token, verified, frozen, guard.Proof)
		if e != nil {
			return e
		}
		if guard.RecordReceipt(bounded, result) != nil {
			return ErrRecoveryFinalizationUnconfirmed
		}
		return nil
	})
	if err != nil {
		return zero, ErrRecoveryFinalizationUnconfirmed
	}
	return result, nil
}

func (c *Client) submitRecoveryFinalization(ctx context.Context, token string, verified VerifiedRecoveryReleaseAuthorization, frozen tasks.RuntimeRecoveryFinalizationProof, refresh func(context.Context) (tasks.RuntimeRecoveryFinalizationProof, error)) (RecoveryFinalizationReceipt, error) {
	var zero RecoveryFinalizationReceipt
	wire, err := json.Marshal(frozen)
	if err != nil {
		return zero, ErrRecoveryFinalizationUnconfirmed
	}
	hash := recoveryReadHash(wire)
	check := func() bool {
		p, e := refresh(ctx)
		now := time.Now().UTC()
		return e == nil && ctx.Err() == nil && !now.Before(verified.Authorization.IssuedAt) && now.Before(verified.Authorization.ExpiresAt) && frozen.SchemaVersion == 1 && frozen.OperationID == verified.Authorization.OperationID && frozen.AuthorizationHash == verified.AuthorizationHash && frozen.Observation == verified.Authorization.Expected && !frozen.ObservedAt.Before(verified.Authorization.IssuedAt) && !frozen.ObservedAt.After(now) && len(frozen.FinalConsumptionHash) == 64 && p.SchemaVersion == frozen.SchemaVersion && p.OperationID == frozen.OperationID && p.AuthorizationHash == frozen.AuthorizationHash && p.FinalConsumptionHash == frozen.FinalConsumptionHash && p.Observation == frozen.Observation && !p.ObservedAt.Before(frozen.ObservedAt)
	}
	valid := func(r RecoveryFinalizationReceipt) bool {
		return r.SchemaVersion == 1 && r.Recorded && r.OperationID == frozen.OperationID && r.AuthorizationHash == frozen.AuthorizationHash && r.FinalConsumptionHash == frozen.FinalConsumptionHash && r.ProofHash == hash && r.RequestHash == hash
	}
	path := "/api/v1/agent/recovery-observations/" + url.PathEscape(frozen.OperationID) + "/finalization"
	r, e := exchangePinnedRecoveryEvidence(ctx, c, token, path, wire, check, valid)
	if e != nil {
		return zero, ErrRecoveryFinalizationUnconfirmed
	}
	return r, nil
}
