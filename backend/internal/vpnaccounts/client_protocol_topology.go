package vpnaccounts

import (
	"context"
	"fmt"

	"github.com/ikaevus/routegate/backend/internal/platform"
)

type clientProtocolTopologyValidator interface {
	ValidateClientProtocolTopology(context.Context, string, string) error
}

func validateClientProtocolDeploymentRole(protocol, deploymentRole string) error {
	role := platform.EffectiveDeploymentRole(deploymentRole)
	if platform.ProtocolSupportsDeploymentRole(protocol, role) {
		return nil
	}
	if protocol == ClientProtocolHysteria2 {
		return unavailableClientConnection(fmt.Errorf("Hysteria2 is supported only on a dedicated VPN Node because its current ACME lifecycle cannot share the Hybrid Manager/nginx topology"))
	}
	return unavailableClientConnection(fmt.Errorf("protocol %q is not supported on deployment role %q", protocol, deploymentRole))
}

func validateClientProtocolTopologyForSource(
	ctx context.Context,
	source any,
	subscription SubscriptionProfile,
	profile ClientProfile,
) error {
	if subscription.Server == nil {
		return nil
	}
	validator, ok := source.(clientProtocolTopologyValidator)
	if !ok {
		return nil
	}
	return validator.ValidateClientProtocolTopology(
		ctx,
		subscription.Server.ID,
		resolveEffectiveClientProtocol(profile, subscription.Server),
	)
}

func (r *Repository) ValidateClientProtocolTopology(ctx context.Context, serverID, protocol string) error {
	var deploymentRole string
	if err := r.pool.QueryRow(ctx, `
		SELECT deployment_role
		FROM servers
		WHERE id = $1::uuid
	`, serverID).Scan(&deploymentRole); err != nil {
		return err
	}
	return validateClientProtocolDeploymentRole(protocol, deploymentRole)
}
