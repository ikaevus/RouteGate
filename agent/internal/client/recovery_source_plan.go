package client

import (
	"context"
	"errors"

	"github.com/ikaevus/routegate/agent/internal/tasks"
)

var ErrRecoverySourcePlanUnconfirmed = errors.New("recovery source cleanup plan was not confirmed")

// Dormant read-only plan. Requires the independently retained archive already
// owned by this original live owner; never creates it, deletes or releases files.
func (c *Client) PlanRecoverySourceCleanup(ctx context.Context, token string, v VerifiedRecoveryReleaseAuthorization, owner *tasks.RuntimeRecoveryFinalization) (tasks.RuntimeRecoverySourceCleanupPlan, error) {
	var result tasks.RuntimeRecoverySourceCleanupPlan
	err := c.withAcknowledgedRecoveryRetention(ctx, token, v, owner, func(bounded context.Context, g *tasks.RuntimeRecoveryPreparedReleaseGuard) error {
		var err error
		result, err = g.PlanArchivedSourceCleanup(bounded)
		return err
	})
	if err != nil {
		return tasks.RuntimeRecoverySourceCleanupPlan{}, ErrRecoverySourcePlanUnconfirmed
	}
	return result, nil
}
