package servers

import (
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/agents"
)

func TestNodeInventorySummaryGuidesRemoteVPNNodeOnboarding(t *testing.T) {
	now := time.Date(2026, time.August, 18, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name                string
		server              ServerWithAgent
		connectionState     string
		capabilityStatus    string
		nextAction           string
		managedAdapterCount int
	}{
		{
			name:             "management node needs no Agent",
			server:           ServerWithAgent{Server: Server{DeploymentRole: "management"}},
			connectionState:  NodeConnectionNotApplicable,
			capabilityStatus: NodeCapabilityNotApplicable,
			nextAction:       NodeNextActionNone,
		},
		{
			name:             "new VPN node installs Agent next",
			server:           ServerWithAgent{Server: Server{DeploymentRole: "vpn"}},
			connectionState:  NodeConnectionAwaitingAgent,
			capabilityStatus: NodeCapabilityNotReported,
			nextAction:       NodeNextActionInstallAgent,
		},
		{
			name: "fresh capable Agent is online",
			server: ServerWithAgent{
				Server: Server{DeploymentRole: "vpn"},
				Agent: &agents.Agent{
					Status:        agents.StatusOnline,
					LastSeenAt:    timePointer(now.Add(-30 * time.Second)),
					Compatibility: agents.Compatibility{Status: agents.CompatibilityCompatible},
					Capabilities: agents.Capabilities{"routegate": map[string]any{
						"schemaVersion": 1,
						"nodeCapabilities": []string{"vpn"},
						"vpnCoreAdapters": []map[string]any{{"core": "sing-box", "protocol": "vless"}},
					}},
				},
			},
			connectionState:     NodeConnectionOnline,
			capabilityStatus:    NodeCapabilityCompatible,
			nextAction:          NodeNextActionNone,
			managedAdapterCount: 1,
		},
		{
			name: "stale Agent restores connection next",
			server: ServerWithAgent{
				Server: Server{DeploymentRole: "vpn"},
				Agent: &agents.Agent{
					Status:     agents.StatusOnline,
					LastSeenAt: timePointer(now.Add(-3 * time.Minute)),
					Capabilities: agents.Capabilities{"routegate": map[string]any{
						"schemaVersion": 1,
						"nodeCapabilities": []string{"vpn"},
					}},
				},
			},
			connectionState:  NodeConnectionOffline,
			capabilityStatus: NodeCapabilityCompatible,
			nextAction:       NodeNextActionRestoreConnection,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := newNodeInventorySummary(test.server, now)
			if got.ConnectionState != test.connectionState || got.CapabilityStatus != test.capabilityStatus || got.NextAction != test.nextAction || got.ManagedAdapterCount != test.managedAdapterCount {
				t.Fatalf("inventory = %+v", got)
			}
		})
	}
}

func timePointer(value time.Time) *time.Time {
	return &value
}
