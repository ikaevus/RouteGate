package diagnostics

import "testing"

func TestDiagnosticProfilesAreStrictlyAllowListed(t *testing.T) {
	for _, profile := range []string{ProfileHostOverview, ProfileVPNCoreStatus} {
		if !ValidProfile(profile) {
			t.Fatalf("profile %q must be allow-listed", profile)
		}
	}
	for _, profile := range []string{"", "shell", "command", "host_overview; id", "../script"} {
		if ValidProfile(profile) {
			t.Fatalf("profile %q must be rejected", profile)
		}
		if _, err := Execute(profile); err == nil {
			t.Fatalf("Execute(%q) must fail", profile)
		}
	}
}

func TestDiagnosticResultsContainStructuredEvidenceOnly(t *testing.T) {
	for _, profile := range []string{ProfileHostOverview, ProfileVPNCoreStatus} {
		result, err := Execute(profile)
		if err != nil {
			t.Fatalf("Execute(%q): %v", profile, err)
		}
		if result["schemaVersion"] != SchemaVersion || result["profileKey"] != profile {
			t.Fatalf("unexpected diagnostic envelope: %#v", result)
		}
		if _, ok := result["evidence"].(map[string]any); !ok {
			t.Fatalf("diagnostic evidence must be structured object: %#v", result["evidence"])
		}
		for _, forbidden := range []string{"command", "args", "script", "shell", "output", "stdout", "stderr"} {
			if _, exists := result[forbidden]; exists {
				t.Fatalf("diagnostic result must not expose %q: %#v", forbidden, result)
			}
		}
	}
}
