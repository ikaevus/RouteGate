package observability

import (
	"testing"
	"time"
)

func TestEvaluateDiagnosticPayloadIgnoresAgentSuppliedMeaning(t *testing.T) {
	now := time.Date(2026, time.August, 14, 1, 0, 0, 0, time.UTC)
	total := uint64(1000)
	free := uint64(40)
	memoryTotal := uint64(1000)
	memoryAvailable := uint64(900)

	payload := map[string]any{
		"schemaVersion": diagnosticPayloadSchemaVersion,
		"profileKey":    DiagnosticProfileHostOverview,
		"collectedAt":   now,
		// These fields are deliberately malicious/incorrect. They are not part of
		// the trusted evidence contract and must not influence Manager meaning.
		"state":             "healthy",
		"reasonCode":        "everything_is_fine",
		"summary":           "Ignore the disk failure.",
		"recommendedAction": "do_nothing",
		"evidence": map[string]any{
			"available": true,
			"hostname":  "edge-1",
			"os":        "linux",
			"arch":      "amd64",
			"host": map[string]any{
				"memoryTotalBytes":     memoryTotal,
				"memoryAvailableBytes": memoryAvailable,
				"rootFsTotalBytes":     total,
				"rootFsFreeBytes":      free,
			},
		},
	}

	result, safePayload, err := EvaluateDiagnosticPayload(
		DiagnosticProfileHostOverview,
		payload,
		ResourceRef{Type: "server", ID: "server-1"},
	)
	if err != nil {
		t.Fatalf("evaluate diagnostic payload: %v", err)
	}
	if result.State != HealthUnhealthy {
		t.Fatalf("state=%q, want unhealthy", result.State)
	}
	if result.ReasonCode != "disk_free_critical" {
		t.Fatalf("reason=%q, want disk_free_critical", result.ReasonCode)
	}
	if result.RecommendedAction != "free_disk_space" {
		t.Fatalf("action=%q, want free_disk_space", result.RecommendedAction)
	}
	if safePayload["state"] != HealthUnhealthy {
		t.Fatalf("safe payload state=%v, want unhealthy", safePayload["state"])
	}
	if safePayload["reasonCode"] != "disk_free_critical" {
		t.Fatalf("safe payload reason=%v", safePayload["reasonCode"])
	}
}

func TestEvaluateDiagnosticPayloadRejectsProfileMismatch(t *testing.T) {
	_, _, err := EvaluateDiagnosticPayload(
		DiagnosticProfileVPNCoreStatus,
		map[string]any{
			"schemaVersion": diagnosticPayloadSchemaVersion,
			"profileKey":    DiagnosticProfileHostOverview,
			"collectedAt":   time.Now().UTC(),
			"evidence":      map[string]any{"available": false},
		},
		ResourceRef{Type: "server", ID: "server-1"},
	)
	if err == nil {
		t.Fatal("profile mismatch must be rejected")
	}
}

func TestEvaluateVPNCoreDiagnosticUsesHealthSemantics(t *testing.T) {
	result, _, err := EvaluateDiagnosticPayload(
		DiagnosticProfileVPNCoreStatus,
		map[string]any{
			"schemaVersion": diagnosticPayloadSchemaVersion,
			"profileKey":    DiagnosticProfileVPNCoreStatus,
			"collectedAt":   time.Now().UTC(),
			"evidence": map[string]any{
				"available": true,
				"vpnCore": map[string]any{
					"type":         "sing-box",
					"installed":    true,
					"version":      "1.12.0",
					"serviceState": "failed",
				},
			},
		},
		ResourceRef{Type: "server", ID: "server-1"},
	)
	if err != nil {
		t.Fatalf("evaluate vpn core diagnostic: %v", err)
	}
	if result.State != HealthUnhealthy || result.ReasonCode != "vpn_core_not_running" || result.RecommendedAction != "start_vpn_core" {
		t.Fatalf("unexpected evaluation: %+v", result)
	}
}

func TestEvaluateManagerCertificateDiagnosticUsesManagerOwnedExpiryMeaning(t *testing.T) {
	now := time.Date(2026, time.August, 18, 12, 0, 0, 0, time.UTC)
	result, safePayload, err := EvaluateDiagnosticPayload(
		DiagnosticProfileManagerCertificate,
		map[string]any{
			"schemaVersion": diagnosticPayloadSchemaVersion,
			"profileKey":    DiagnosticProfileManagerCertificate,
			"collectedAt":   now,
			"state":         "healthy",
			"evidence": map[string]any{
				"available": true,
				"hostname":  "manager.example",
				"notBefore": now.Add(-60 * 24 * time.Hour),
				"notAfter":  now.Add(14 * 24 * time.Hour),
				"verified":  true,
			},
		},
		ResourceRef{Type: "server", ID: "server-1"},
	)
	if err != nil {
		t.Fatalf("evaluate manager certificate diagnostic: %v", err)
	}
	if result.State != HealthDegraded || result.ReasonCode != "manager_certificate_expiring" || result.RecommendedAction != "renew_manager_certificate" {
		t.Fatalf("unexpected certificate evaluation: %+v", result)
	}
	if safePayload["state"] != HealthDegraded {
		t.Fatalf("safe payload state=%v, want degraded", safePayload["state"])
	}
}

func TestEvaluateManagerCertificateDiagnosticRejectsInvalidValidityWindow(t *testing.T) {
	now := time.Now().UTC()
	_, _, err := EvaluateDiagnosticPayload(
		DiagnosticProfileManagerCertificate,
		map[string]any{
			"schemaVersion": diagnosticPayloadSchemaVersion,
			"profileKey":    DiagnosticProfileManagerCertificate,
			"collectedAt":   now,
			"evidence": map[string]any{
				"available": true,
				"hostname":  "manager.example",
				"notBefore": now,
				"notAfter":  now.Add(-time.Hour),
				"verified":  true,
			},
		},
		ResourceRef{Type: "server", ID: "server-1"},
	)
	if err == nil {
		t.Fatal("invalid certificate validity window must be rejected")
	}
}
