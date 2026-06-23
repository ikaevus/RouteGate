package configs

import (
	"encoding/json"
	"testing"
	"time"
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
