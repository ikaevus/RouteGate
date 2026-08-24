package updates

import (
	"reflect"
	"testing"

	"github.com/ikaevus/routegate/backend/internal/buildinfo"
)

func releaseInfo() buildinfo.Info {
	return buildinfo.Info{
		Version:                       "v0.2.0",
		GitCommit:                     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		BuildDate:                     "2026-08-24T00:00:00Z",
		ExpectedDatabaseSchemaVersion: 135,
		UpdateStatus:                  "manual",
		UpdateChannel:                 "development",
		AutomaticUpdatesSupported:     false,
	}
}

func TestEvaluatePreflightProceed(t *testing.T) {
	result := EvaluatePreflight(releaseInfo(), "000135_update_jobs")
	if result.Decision != DecisionProceed {
		t.Fatalf("decision = %q, want %q", result.Decision, DecisionProceed)
	}
	if len(result.Blockers) != 0 {
		t.Fatalf("blockers = %#v, want none", result.Blockers)
	}
	if result.HostTrustPreflight != HostTrustPreflightDeferred {
		t.Fatalf("hostTrustPreflight = %q", result.HostTrustPreflight)
	}
	if result.AutomaticUpdatesSupported {
		t.Fatal("C1 must not enable automatic updates")
	}
}

func TestEvaluatePreflightBlocksSchemaMismatch(t *testing.T) {
	result := EvaluatePreflight(releaseInfo(), "000134_distinct_tcp_listener_ports")
	if result.Decision != DecisionBlocked {
		t.Fatalf("decision = %q, want %q", result.Decision, DecisionBlocked)
	}
	if !reflect.DeepEqual(result.Blockers, []string{BlockerDatabaseSchemaMismatch}) {
		t.Fatalf("blockers = %#v", result.Blockers)
	}
}

func TestEvaluatePreflightBlocksDevelopmentBuildMetadata(t *testing.T) {
	info := releaseInfo()
	info.Version = "dev"
	info.GitCommit = "unknown"
	info.BuildDate = "unknown"

	result := EvaluatePreflight(info, "000135_update_jobs")
	if result.Decision != DecisionBlocked {
		t.Fatalf("decision = %q, want %q", result.Decision, DecisionBlocked)
	}
	if !reflect.DeepEqual(result.Blockers, []string{BlockerManagerBuildMetadataUnavailable}) {
		t.Fatalf("blockers = %#v", result.Blockers)
	}
}

func TestMigrationGenerationSupportsRepairMigrationSuffixes(t *testing.T) {
	generation, ok := migrationGeneration("000131z_safe_client_protocol_activation_repair")
	if !ok || generation != 131 {
		t.Fatalf("generation = %d, ok = %v", generation, ok)
	}
}
