package client

import (
	"context"

	"github.com/ikaevus/routegate/agent/internal/tasks"
)

// Dormant original-owner local admission release. Manager reservation remains
// prepared. No automatic dispatch or coordinated reservation completion exists.
// A non-nil error NEVER promises that the final marker remains: preserve the
// explicit uncertain outcome through every outer readiness/context check.
func (c *Client) ReleaseRecoveryAdmission(ctx context.Context, token string, v VerifiedRecoveryReleaseAuthorization, owner *tasks.RuntimeRecoveryFinalization) (tasks.RuntimeRecoveryAdmissionReleaseResult, error) {
	result, _, e := c.releaseRecoveryAdmission(ctx, token, v, owner, false)
	return result, e
}

// Release and retain a frozen post-release statement before returning from the
// same live readiness callback. No Manager submission/completion is added.
func (c *Client) ReleaseRecoveryAdmissionWithProof(ctx context.Context, token string, v VerifiedRecoveryReleaseAuthorization, owner *tasks.RuntimeRecoveryFinalization) (tasks.RuntimeRecoveryAdmissionReleaseResult, tasks.RuntimeRecoveryAdmissionReleaseProof, error) {
	return c.releaseRecoveryAdmission(ctx, token, v, owner, true)
}
func (c *Client) releaseRecoveryAdmission(ctx context.Context, token string, v VerifiedRecoveryReleaseAuthorization, owner *tasks.RuntimeRecoveryFinalization, withProof bool) (tasks.RuntimeRecoveryAdmissionReleaseResult, tasks.RuntimeRecoveryAdmissionReleaseProof, error) {
	var proof tasks.RuntimeRecoveryAdmissionReleaseProof
	result := tasks.RuntimeRecoveryAdmissionReleaseResult{Outcome: tasks.RuntimeRecoveryAdmissionNotCommitted}
	e := c.withRecoveryCleanedCommitReadiness(ctx, token, v, owner, func(bounded context.Context, g *tasks.RuntimeRecoveryPreparedReleaseGuard) error {
		var e error
		result, e = g.ReleaseAdmission(bounded)
		if e == nil && withProof {
			proof, e = g.PinAdmissionReleaseProof(bounded)
		}
		return e
	})
	if e != nil {
		if result.Outcome == tasks.RuntimeRecoveryAdmissionDurable {
			result.Outcome = tasks.RuntimeRecoveryAdmissionUncertain
		}
		return result, tasks.RuntimeRecoveryAdmissionReleaseProof{}, ErrRecoveryCleanedCommitUnconfirmed
	}
	return result, proof, nil
}
