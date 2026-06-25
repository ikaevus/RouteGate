package configs

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/traffic"
)

func TestBuildRenderedConfigIncludesServerAgentAndSingBoxSkeleton(t *testing.T) {
	renderedAt := time.Date(2026, time.June, 23, 12, 0, 0, 0, time.UTC)
	config := buildRenderedConfig(ServerConfigInfo{
		ID:       "server-id",
		Name:     "fi-01",
		Hostname: "fi-01.example",
		PublicIP: "203.0.113.10",
		Location: "Finland",
		Provider: "Hostkey",
		Status:   "active",
		Agent: &AgentConfigInfo{
			ID:           "agent-id",
			Hostname:     "fi-01",
			OS:           "linux",
			Arch:         "amd64",
			AgentVersion: "0.1.0",
			Status:       "online",
			Capabilities: map[string]any{"config_apply": true},
		},
	}, renderedAt)

	if config.SchemaVersion != SchemaVersion {
		t.Fatalf("schemaVersion = %q, want %q", config.SchemaVersion, SchemaVersion)
	}
	if config.Server.ID != "server-id" || config.Server.Name != "fi-01" {
		t.Fatalf("unexpected server in rendered config: %+v", config.Server)
	}
	if config.Agent == nil || config.Agent.ID != "agent-id" || config.Agent.AgentVersion != "0.1.0" {
		t.Fatalf("unexpected agent in rendered config: %+v", config.Agent)
	}
	if len(config.SingBox.Outbounds) != 1 || config.SingBox.Outbounds[0].Tag != "direct" {
		t.Fatalf("unexpected sing-box outbounds: %+v", config.SingBox.Outbounds)
	}
	if config.SingBox.Route.Final != "direct" {
		t.Fatalf("route final = %q, want direct", config.SingBox.Route.Final)
	}

	payload, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("rendered config must be JSON serializable: %v", err)
	}
	if !json.Valid(payload) {
		t.Fatalf("rendered config is not valid JSON: %s", payload)
	}
}

func TestBuildRenderedConfigIncludesRenderableVPNAccounts(t *testing.T) {
	renderedAt := time.Date(2026, time.June, 25, 12, 0, 0, 0, time.UTC)
	config := buildRenderedConfig(ServerConfigInfo{
		ID:        "server-id",
		Name:      "fi-01",
		Status:    "active",
		VLESSPort: 443,
		VPNAccounts: []VPNAccountConfigInfo{
			{
				ID:                       "account-unlimited",
				DisplayName:              "Unlimited User",
				Status:                   "active",
				VLESSUUID:                "11111111-1111-1111-1111-111111111111",
				VLESSFlow:                "xtls-rprx-vision",
				TrafficEnforcementStatus: traffic.TrafficLimitEnforcementNotEnforced,
			},
			{
				ID:                       "account-within-limit",
				DisplayName:              "Within Limit User",
				Status:                   "active",
				VLESSUUID:                "22222222-2222-2222-2222-222222222222",
				VLESSFlow:                "xtls-rprx-vision",
				TrafficEnforcementStatus: traffic.TrafficLimitEnforcementWithinLimit,
			},
		},
	}, renderedAt)

	if len(config.VPNAccounts) != 2 {
		t.Fatalf("rendered vpn accounts = %d, want 2: %+v", len(config.VPNAccounts), config.VPNAccounts)
	}
	if len(config.SingBox.Inbounds) != 1 {
		t.Fatalf("sing-box inbounds = %d, want 1: %+v", len(config.SingBox.Inbounds), config.SingBox.Inbounds)
	}
	users, ok := config.SingBox.Inbounds[0]["users"].([]map[string]any)
	if !ok {
		t.Fatalf("expected VLESS inbound users, got %+v", config.SingBox.Inbounds[0]["users"])
	}
	if len(users) != 2 {
		t.Fatalf("VLESS users = %d, want 2: %+v", len(users), users)
	}
}

func TestBuildRenderedConfigExcludesOverLimitVPNAccounts(t *testing.T) {
	renderedAt := time.Date(2026, time.June, 25, 12, 0, 0, 0, time.UTC)
	config := buildRenderedConfig(ServerConfigInfo{
		ID:        "server-id",
		Name:      "fi-01",
		Status:    "active",
		VLESSPort: 443,
		VPNAccounts: []VPNAccountConfigInfo{
			{
				ID:                       "account-allowed",
				DisplayName:              "Allowed User",
				Status:                   "active",
				VLESSUUID:                "33333333-3333-3333-3333-333333333333",
				VLESSFlow:                "xtls-rprx-vision",
				TrafficEnforcementStatus: traffic.TrafficLimitEnforcementWithinLimit,
			},
			{
				ID:                       "account-over-limit",
				DisplayName:              "Over Limit User",
				Status:                   "active",
				VLESSUUID:                "44444444-4444-4444-4444-444444444444",
				VLESSFlow:                "xtls-rprx-vision",
				TrafficEnforcementStatus: traffic.TrafficLimitEnforcementOverLimit,
			},
		},
	}, renderedAt)

	if len(config.VPNAccounts) != 1 {
		t.Fatalf("rendered vpn accounts = %d, want 1: %+v", len(config.VPNAccounts), config.VPNAccounts)
	}
	if config.VPNAccounts[0].ID != "account-allowed" {
		t.Fatalf("unexpected rendered account: %+v", config.VPNAccounts[0])
	}

	payload, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("marshal rendered config: %v", err)
	}
	content := string(payload)
	if !strings.Contains(content, "33333333-3333-3333-3333-333333333333") {
		t.Fatalf("allowed account UUID is missing from rendered config: %s", content)
	}
	if strings.Contains(content, "44444444-4444-4444-4444-444444444444") || strings.Contains(content, "account-over-limit") {
		t.Fatalf("over-limit account leaked into rendered config: %s", content)
	}
}

func TestBuildRenderedConfigSkipsInactiveVPNAccounts(t *testing.T) {
	config := buildRenderedConfig(ServerConfigInfo{
		ID:     "server-id",
		Name:   "fi-01",
		Status: "active",
		VPNAccounts: []VPNAccountConfigInfo{
			{
				ID:                       "account-suspended",
				DisplayName:              "Suspended User",
				Status:                   "suspended",
				VLESSUUID:                "55555555-5555-5555-5555-555555555555",
				TrafficEnforcementStatus: traffic.TrafficLimitEnforcementNotEnforced,
			},
		},
	}, time.Date(2026, time.June, 25, 12, 0, 0, 0, time.UTC))

	if len(config.VPNAccounts) != 0 {
		t.Fatalf("inactive accounts must not be rendered: %+v", config.VPNAccounts)
	}
	payload, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("marshal rendered config: %v", err)
	}
	if strings.Contains(string(payload), "55555555-5555-5555-5555-555555555555") {
		t.Fatalf("inactive account leaked into rendered config: %s", payload)
	}
}

func TestValidateRenderedConfigWarnsWhenAgentIsMissing(t *testing.T) {
	config := buildRenderedConfig(ServerConfigInfo{
		ID:     "server-id",
		Name:   "fi-01",
		Status: "pending",
	}, time.Date(2026, time.June, 23, 12, 0, 0, 0, time.UTC))

	result := ValidateRenderedConfig(config)

	if !result.Valid {
		t.Fatalf("config should remain valid before apply: %+v", result)
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("warnings = %v, want one missing-agent warning", result.Warnings)
	}
}

func TestValidateRenderedConfigRejectsMissingRequiredFields(t *testing.T) {
	result := ValidateRenderedConfig(RenderedConfig{})

	if result.Valid {
		t.Fatal("empty rendered config must be invalid")
	}
	if len(result.Errors) == 0 {
		t.Fatalf("expected validation errors: %+v", result)
	}
}

func TestHashRenderedConfigIsDeterministic(t *testing.T) {
	renderedAt := time.Date(2026, time.June, 23, 12, 0, 0, 0, time.UTC)
	config := buildRenderedConfig(ServerConfigInfo{ID: "server-id", Name: "fi-01", Status: "active"}, renderedAt)

	first, err := hashRenderedConfig(config)
	if err != nil {
		t.Fatalf("hash rendered config: %v", err)
	}
	second, err := hashRenderedConfig(config)
	if err != nil {
		t.Fatalf("hash rendered config again: %v", err)
	}
	if first == "" || first != second {
		t.Fatalf("hashes must be stable, got %q and %q", first, second)
	}
}
