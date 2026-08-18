package platform

import (
	"fmt"
	"strings"
)

const (
	VPNCoreSingBox = "sing-box"
	VPNCoreWireGuard = "wireguard"

	VPNProtocolVLESS = "vless"
	VPNProtocolWireGuard = "wireguard"

	VPNTransportTCP = "tcp"
	VPNTransportUDP = "udp"

	VPNSecurityNone    = "none"
	VPNSecurityReality = "reality"
	VPNSecurityWireGuard = "wireguard"
)

// VPNCoreAdapterDescriptor describes one RouteGate-managed protocol path.
// Protocol, transport, and security remain separate so adapters can declare
// only combinations whose complete lifecycle RouteGate can manage safely.
type VPNCoreAdapterDescriptor struct {
	Core          string
	Protocol      string
	Transports    []string
	SecurityModes []string
}

// Supports reports whether this adapter owns the requested composition.
func (d VPNCoreAdapterDescriptor) Supports(core, protocol, transport, security string) bool {
	if !strings.EqualFold(strings.TrimSpace(d.Core), strings.TrimSpace(core)) ||
		!strings.EqualFold(strings.TrimSpace(d.Protocol), strings.TrimSpace(protocol)) {
		return false
	}
	return containsFold(d.Transports, transport) && containsFold(d.SecurityModes, security)
}

func containsFold(values []string, candidate string) bool {
	candidate = strings.TrimSpace(candidate)
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), candidate) {
			return true
		}
	}
	return false
}

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
