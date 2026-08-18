package servers

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/ikaevus/routegate/backend/internal/agents"
	"github.com/ikaevus/routegate/backend/internal/platform"
)

const nodeConnectionStaleAfter = 2 * time.Minute

const (
	NodeConnectionNotApplicable = "not_applicable"
	NodeConnectionAwaitingAgent = "awaiting_agent"
	NodeConnectionOnline        = "online"
	NodeConnectionOffline       = "offline"

	NodeCapabilityNotApplicable = "not_applicable"
	NodeCapabilityNotReported   = "not_reported"
	NodeCapabilityCompatible    = "compatible"
	NodeCapabilityIncompatible  = "incompatible"

	NodeNextActionNone                = "none"
	NodeNextActionInstallAgent        = "install_agent"
	NodeNextActionRestoreConnection   = "restore_connection"
	NodeNextActionReviewCompatibility = "review_compatibility"
	NodeNextActionReviewCapabilities  = "review_capabilities"
)

type NodeInventorySummary struct {
	ConnectionState         string `json:"connectionState"`
	CapabilityStatus        string `json:"capabilityStatus"`
	NextAction              string `json:"nextAction"`
	CapabilitySchemaVersion int    `json:"capabilitySchemaVersion,omitempty"`
	ManagedAdapterCount     int    `json:"managedAdapterCount"`
}

type routeGateCapabilities struct {
	SchemaVersion    int               `json:"schemaVersion"`
	NodeCapabilities []string          `json:"nodeCapabilities"`
	VPNCoreAdapters  []json.RawMessage `json:"vpnCoreAdapters"`
}

func newNodeInventorySummary(server ServerWithAgent, now time.Time) NodeInventorySummary {
	if !platform.EffectiveDeploymentRole(server.Server.DeploymentRole).HostsVPNPlane() {
		return NodeInventorySummary{
			ConnectionState:  NodeConnectionNotApplicable,
			CapabilityStatus: NodeCapabilityNotApplicable,
			NextAction:       NodeNextActionNone,
		}
	}
	if server.Agent == nil {
		return NodeInventorySummary{
			ConnectionState:  NodeConnectionAwaitingAgent,
			CapabilityStatus: NodeCapabilityNotReported,
			NextAction:       NodeNextActionInstallAgent,
		}
	}

	summary := NodeInventorySummary{ConnectionState: NodeConnectionOnline, NextAction: NodeNextActionNone}
	if server.Agent.LastSeenAt == nil || now.Sub(server.Agent.LastSeenAt.UTC()) > nodeConnectionStaleAfter || server.Agent.Status == agents.StatusOffline {
		summary.ConnectionState = NodeConnectionOffline
		summary.NextAction = NodeNextActionRestoreConnection
	}

	capabilities, ok := parseRouteGateCapabilities(server.Agent.Capabilities)
	if !ok {
		summary.CapabilityStatus = NodeCapabilityNotReported
		if summary.NextAction == NodeNextActionNone {
			summary.NextAction = NodeNextActionReviewCapabilities
		}
	} else {
		summary.CapabilitySchemaVersion = capabilities.SchemaVersion
		summary.ManagedAdapterCount = len(capabilities.VPNCoreAdapters)
		if capabilities.SchemaVersion == 1 && slices.Contains(capabilities.NodeCapabilities, "vpn") {
			summary.CapabilityStatus = NodeCapabilityCompatible
		} else {
			summary.CapabilityStatus = NodeCapabilityIncompatible
			if summary.NextAction == NodeNextActionNone {
				summary.NextAction = NodeNextActionReviewCapabilities
			}
		}
	}

	if summary.NextAction == NodeNextActionNone {
		switch server.Agent.Compatibility.Status {
		case agents.CompatibilityUpgradeRequired, agents.CompatibilityUnsupported:
			summary.NextAction = NodeNextActionReviewCompatibility
		}
	}
	return summary
}

func parseRouteGateCapabilities(capabilities agents.Capabilities) (routeGateCapabilities, bool) {
	raw, ok := capabilities["routegate"]
	if !ok {
		return routeGateCapabilities{}, false
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return routeGateCapabilities{}, false
	}
	var parsed routeGateCapabilities
	if err := json.Unmarshal(data, &parsed); err != nil || parsed.SchemaVersion <= 0 {
		return routeGateCapabilities{}, false
	}
	return parsed, true
}
