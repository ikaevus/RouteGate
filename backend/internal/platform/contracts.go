package platform

import (
	"fmt"
	"strings"
)

// DeploymentRole describes which RouteGate planes are intentionally hosted by
// a node. It is an assigned deployment property, not a hierarchy between
// nodes. Runtime capabilities are reported separately by RouteGate Agent.
type DeploymentRole string

const (
	DeploymentRoleManagement DeploymentRole = "management"
	DeploymentRoleVPN        DeploymentRole = "vpn"
	DeploymentRoleHybrid     DeploymentRole = "hybrid"
)

// ParseDeploymentRole validates an API or persistence value. Empty values use
// the supplied default so older API clients can continue creating VPN nodes.
func ParseDeploymentRole(value string, defaultRole DeploymentRole) (DeploymentRole, error) {
	role := DeploymentRole(strings.ToLower(strings.TrimSpace(value)))
	if role == "" {
		role = defaultRole
	}
	if !role.Valid() {
		return "", fmt.Errorf("deploymentRole must be one of: management, vpn, hybrid")
	}
	return role, nil
}

func (r DeploymentRole) Valid() bool {
	switch r {
	case DeploymentRoleManagement, DeploymentRoleVPN, DeploymentRoleHybrid:
		return true
	default:
		return false
	}
}

func (r DeploymentRole) HostsManagementPlane() bool {
	return r == DeploymentRoleManagement || r == DeploymentRoleHybrid
}

func (r DeploymentRole) HostsVPNPlane() bool {
	return r == DeploymentRoleVPN || r == DeploymentRoleHybrid
}

// EffectiveDeploymentRole keeps pre-RG-114 in-memory fixtures and serialized
// objects compatible. Persisted rows are non-null after migration 000124.
func EffectiveDeploymentRole(value string) DeploymentRole {
	role, err := ParseDeploymentRole(value, DeploymentRoleVPN)
	if err != nil {
		return ""
	}
	return role
}
