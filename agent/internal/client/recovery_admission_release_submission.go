package client

import (
	"context"
	"encoding/json"
	"net/url"
	"time"

	"github.com/ikaevus/routegate/agent/internal/tasks"
)

// Dormant, explicit original-owner operation. This API must not be connected to
// a Runner, poller or ordinary mutation retry. Release, proof pinning and the
// Manager exchange share the same bounded callback and ORIGINAL admission flock.
// An HTTP receipt acknowledges immutable Manager storage, not VPN revocation.
func (c *Client) ReleaseRecoveryAdmissionAndSubmitEvidence(ctx context.Context, token string, v VerifiedRecoveryReleaseAuthorization, owner *tasks.RuntimeRecoveryFinalization) (tasks.RuntimeRecoveryAdmissionReleaseResult, RecoveryAdmissionReleaseReceipt, error) {
	result := tasks.RuntimeRecoveryAdmissionReleaseResult{Outcome: tasks.RuntimeRecoveryAdmissionNotCommitted}
	var receipt RecoveryAdmissionReleaseReceipt
	err := c.withRecoveryCleanedCommitReadiness(ctx, token, v, owner, func(bounded context.Context, g *tasks.RuntimeRecoveryPreparedReleaseGuard) error {
		var err error
		result, err = g.ReleaseAdmission(bounded)
		if err != nil {
			return err
		}
		frozen, err := g.PinAdmissionReleaseProof(bounded)
		if err != nil {
			return err
		}
		receipt, err = c.submitRecoveryAdmissionRelease(bounded, token, v, frozen, g.PinAdmissionReleaseProof)
		return err
	})
	if err != nil {
		// Local release cannot be undone, even if the Manager accepted an HTTP
		// request just before a network failure or the original owner went stale.
		if result.Outcome == tasks.RuntimeRecoveryAdmissionDurable {
			result.Outcome = tasks.RuntimeRecoveryAdmissionUncertain
		}
		return result, RecoveryAdmissionReleaseReceipt{}, ErrRecoveryAdmissionReleaseUnconfirmed
	}
	return result, receipt, nil
}

func (c *Client) submitRecoveryAdmissionRelease(ctx context.Context, token string, v VerifiedRecoveryReleaseAuthorization, frozen tasks.RuntimeRecoveryAdmissionReleaseProof, refresh func(context.Context) (tasks.RuntimeRecoveryAdmissionReleaseProof, error)) (RecoveryAdmissionReleaseReceipt, error) {
	var zero RecoveryAdmissionReleaseReceipt
	if refresh == nil || token == "" || v.Binding.ManagerURL != c.managerURL {
		return zero, ErrRecoveryAdmissionReleaseUnconfirmed
	}
	wire, err := json.Marshal(frozen)
	if err != nil || len(wire) > 8192 {
		return zero, ErrRecoveryAdmissionReleaseUnconfirmed
	}
	hash := recoveryReadHash(wire)
	check := func() bool {
		p, err := refresh(ctx)
		now := time.Now().UTC()
		return err == nil && ctx.Err() == nil && p == frozen &&
			frozen.SchemaVersion == 1 && frozen.Purpose == tasks.RuntimeRecoveryAdmissionReleasePurpose &&
			frozen.OperationID == v.Authorization.OperationID &&
			frozen.AgentCredentialGeneration == v.Binding.AgentCredentialGeneration &&
			frozen.AgentCredentialGeneration == v.Authorization.AgentCredentialGeneration &&
			frozen.AuthorizationHash == v.AuthorizationHash &&
			frozen.Observation == v.Authorization.Expected &&
			!frozen.ReleasedAt.Before(v.Authorization.IssuedAt) &&
			!frozen.ObservedAt.Before(frozen.ReleasedAt) &&
			frozen.ObservedAt.Before(v.Authorization.ExpiresAt) &&
			!frozen.ObservedAt.After(now) && !now.Before(v.Authorization.IssuedAt) &&
			now.Before(v.Authorization.ExpiresAt)
	}
	path := "/api/v1/agent/recovery-observations/" + url.PathEscape(frozen.OperationID) + "/admission-release"
	receipt, err := exchangePinnedRecoveryEvidence(ctx, c, token, path, wire, check,
		func(r RecoveryAdmissionReleaseReceipt) bool { return validAdmissionReleaseReceipt(r, frozen, hash) })
	if err != nil {
		return zero, ErrRecoveryAdmissionReleaseUnconfirmed
	}
	return receipt, nil
}
