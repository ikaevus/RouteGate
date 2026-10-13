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
var ErrRecoveryCommitUnconfirmed = errors.New("recovery commit storage was not confirmed")

// Historical storage receipt only, never permission to release an Agent fence.
type RecoveryCommitReceipt = tasks.RuntimeRecoveryCommitReceipt

// SubmitRecoveryCommit is dormant: no Runner invokes it. Caller owns the live
// lease and must defer Close. We never close, unlock, delete or resume state.
func (c *Client) SubmitRecoveryCommit(ctx context.Context, token string, verified VerifiedRecoveryGrant, owner *tasks.RuntimeRecoveryCommit) (RecoveryCommitReceipt, error) {
	var zero, result RecoveryCommitReceipt
	wire, err := json.Marshal(verified.Grant)
	if err != nil || token == "" || verified.Binding.ManagerURL != c.managerURL {
		return zero, ErrRecoveryCommitUnconfirmed
	}
	_, hash, err := tasks.ValidateRuntimeRecoveryGrant(wire, verified.Binding, time.Now())
	if err != nil || hash != verified.GrantHash {
		return zero, ErrRecoveryCommitUnconfirmed
	}
	bounded, cancel := context.WithDeadline(ctx, verified.Grant.ExpiresAt)
	defer cancel()
	err = owner.WithSubmission(bounded, verified.Binding, hash, func(guard *tasks.RuntimeRecoveryCommitGuard) error {
		frozen, e := guard.PinProof(bounded)
		if e != nil {
			return ErrRecoveryCommitUnconfirmed
		}
		result, e = c.submitRecoveryCommit(bounded, token, verified, frozen, guard.Proof)
		if e != nil {
			return e
		}
		if guard.RecordReceipt(bounded, result) != nil {
			return ErrRecoveryCommitUnconfirmed
		}
		return nil
	})
	if err != nil {
		return zero, ErrRecoveryCommitUnconfirmed
	}
	return result, nil
}

func (c *Client) submitRecoveryCommit(ctx context.Context, token string, verified VerifiedRecoveryGrant, frozen tasks.RuntimeRecoveryCommitProof, refresh func(context.Context) (tasks.RuntimeRecoveryCommitProof, error)) (RecoveryCommitReceipt, error) {
	var zero RecoveryCommitReceipt
	wire, err := json.Marshal(frozen)
	if err != nil {
		return zero, ErrRecoveryCommitUnconfirmed
	}
	hash := recoveryReadHash(wire)
	check := func() bool {
		p, e := refresh(ctx)
		now := time.Now().UTC()
		return e == nil && ctx.Err() == nil && !now.Before(verified.Grant.IssuedAt) && now.Before(verified.Grant.ExpiresAt) && frozen.SchemaVersion == 1 && frozen.OperationID == verified.Grant.OperationID && frozen.GrantHash == verified.GrantHash && frozen.Observation == verified.Grant.Expected && !frozen.ObservedAt.Before(verified.Grant.IssuedAt) && !frozen.ObservedAt.After(now) && len(frozen.ConsumptionHash) == 64 && p.SchemaVersion == frozen.SchemaVersion && p.OperationID == frozen.OperationID && p.GrantHash == frozen.GrantHash && p.ConsumptionHash == frozen.ConsumptionHash && p.Observation == frozen.Observation && !p.ObservedAt.Before(frozen.ObservedAt)
	}
	valid := func(r RecoveryCommitReceipt) bool {
		return r.SchemaVersion == 1 && r.Recorded && r.OperationID == frozen.OperationID && r.GrantHash == frozen.GrantHash && r.ConsumptionHash == frozen.ConsumptionHash && r.ProofHash == hash && r.RequestHash == hash
	}
	path := "/api/v1/agent/recovery-observations/" + url.PathEscape(frozen.OperationID) + "/commit"
	return exchangePinnedRecoveryEvidence(ctx, c, token, path, wire, check, valid)
}
