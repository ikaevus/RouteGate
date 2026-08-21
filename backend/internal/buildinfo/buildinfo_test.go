package buildinfo

import "testing"

func TestCurrentUsesDevelopmentDefaults(t *testing.T) {
	info := Current()

	if info.Version != "dev" || info.GitCommit != "unknown" || info.BuildDate != "unknown" {
		t.Fatalf("unexpected build defaults: %+v", info)
	}
	if info.AgentProtocolVersion != 1 || info.MinimumAgentProtocolVersion != 1 {
		t.Fatalf("unexpected protocol defaults: %+v", info)
	}
	if ExpectedDatabaseSchemaVersion != 131 {
		t.Fatalf("schema version = %d, want 131", info.ExpectedDatabaseSchemaVersion)
	}
	if info.AutomaticUpdatesSupported {
		t.Fatal("automatic updates must remain disabled for the MVP")
	}
}
