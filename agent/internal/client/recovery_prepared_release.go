package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"time"

	"github.com/ikaevus/routegate/agent/internal/tasks"
)

var ErrRecoveryPreparedReleaseUnconfirmed = errors.New("recovery prepared release storage was not confirmed")

type RecoveryPreparedReleaseReceipt = tasks.RuntimeRecoveryPreparedReleaseReceipt

// Dormant: Runner does not invoke this. Caller retains original live owner and
// must defer Close. We never release, resume, clean up or renew authority.
func (c *Client) SubmitRecoveryPreparedRelease(ctx context.Context, token string, verified VerifiedRecoveryReleaseAuthorization, owner *tasks.RuntimeRecoveryFinalization) (RecoveryPreparedReleaseReceipt, error) {
	var zero, result RecoveryPreparedReleaseReceipt
	wire, e := json.Marshal(verified.Authorization)
	if e != nil || token == "" || verified.Binding.ManagerURL != c.managerURL {
		return zero, ErrRecoveryPreparedReleaseUnconfirmed
	}
	hash := recoveryReadHash(wire)
	if hash != verified.AuthorizationHash {
		return zero, ErrRecoveryPreparedReleaseUnconfirmed
	}
	bounded, cancel := context.WithDeadline(ctx, verified.Authorization.ExpiresAt)
	defer cancel()
	e = owner.WithPreparedReleaseSubmission(bounded, verified.Binding, hash, func(g *tasks.RuntimeRecoveryPreparedReleaseGuard) error {
		frozen, e := g.PinProof(bounded)
		if e != nil {
			return ErrRecoveryPreparedReleaseUnconfirmed
		}
		result, e = c.submitRecoveryPreparedRelease(bounded, token, verified, frozen, g.Proof)
		if e != nil {
			return e
		}
		if g.RecordReceipt(bounded, result) != nil {
			return ErrRecoveryPreparedReleaseUnconfirmed
		}
		return nil
	})
	if e != nil {
		return zero, ErrRecoveryPreparedReleaseUnconfirmed
	}
	return result, nil
}
func (c *Client) submitRecoveryPreparedRelease(ctx context.Context, token string, v VerifiedRecoveryReleaseAuthorization, frozen tasks.RuntimeRecoveryPreparedReleaseProof, refresh func(context.Context) (tasks.RuntimeRecoveryPreparedReleaseProof, error)) (RecoveryPreparedReleaseReceipt, error) {
	var zero RecoveryPreparedReleaseReceipt
	wire, e := json.Marshal(frozen)
	if e != nil {
		return zero, ErrRecoveryPreparedReleaseUnconfirmed
	}
	hash := recoveryReadHash(wire)
	check := func() bool {
		p, e := refresh(ctx)
		now := time.Now().UTC()
		return e == nil && ctx.Err() == nil && !now.Before(v.Authorization.IssuedAt) && now.Before(v.Authorization.ExpiresAt) && p == frozen && frozen.SchemaVersion == 1 && frozen.Purpose == "runtime_mutation_fence_release_preparation_evidence_v1" && frozen.OperationID == v.Authorization.OperationID && frozen.AgentCredentialGeneration == v.Binding.AgentCredentialGeneration && frozen.AgentCredentialGeneration == v.Authorization.AgentCredentialGeneration && frozen.AuthorizationHash == v.AuthorizationHash && frozen.Observation == v.Authorization.Expected && !frozen.PreparedAt.Before(v.Authorization.IssuedAt) && !frozen.ObservedAt.Before(frozen.PreparedAt) && frozen.ObservedAt.Before(v.Authorization.ExpiresAt) && !frozen.ObservedAt.After(now)
	}
	valid := func(r RecoveryPreparedReleaseReceipt) bool {
		return r.SchemaVersion == 1 && r.Recorded && r.OperationID == frozen.OperationID && r.ReleaseIntentHash == frozen.ReleaseIntentHash && r.ProofHash == hash && r.RequestHash == hash
	}
	path := "/api/v1/agent/recovery-observations/" + url.PathEscape(frozen.OperationID) + "/prepared-release"
	r, e := exchangePinnedRecoveryEvidence(ctx, c, token, path, wire, check, valid)
	if e != nil {
		return zero, ErrRecoveryPreparedReleaseUnconfirmed
	}
	return r, nil
}
