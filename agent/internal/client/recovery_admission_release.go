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
	result := tasks.RuntimeRecoveryAdmissionReleaseResult{Outcome: tasks.RuntimeRecoveryAdmissionNotCommitted}
	e := c.withRecoveryCleanedCommitReadiness(ctx, token, v, owner, func(bounded context.Context, g *tasks.RuntimeRecoveryPreparedReleaseGuard) error {
		var e error
		result, e = g.ReleaseAdmission(bounded)
		return e
	})
	if e != nil {
		if result.Outcome == tasks.RuntimeRecoveryAdmissionDurable {
			result.Outcome = tasks.RuntimeRecoveryAdmissionUncertain
		}
		return result, ErrRecoveryCleanedCommitUnconfirmed
	}
	return result, nil
}
