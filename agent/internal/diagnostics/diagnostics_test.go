package diagnostics

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestDiagnosticProfilesAreStrictlyAllowListed(t *testing.T) {
	for _, profile := range []string{ProfileHostOverview, ProfileVPNCoreStatus, ProfileManagerCertificate} {
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
	for _, profile := range []string{ProfileHostOverview, ProfileVPNCoreStatus, ProfileManagerCertificate} {
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

func TestManagerCertificateDiagnosticReturnsMetadataWithoutCertificateMaterial(t *testing.T) {
	server := httptest.NewTLSServer(nil)
	defer server.Close()

	result, err := ExecuteWithOptions(ProfileManagerCertificate, Options{ManagerURL: server.URL})
	if err != nil {
		t.Fatalf("execute manager certificate diagnostic: %v", err)
	}
	evidence, ok := result["evidence"].(map[string]any)
	if !ok || evidence["available"] != true {
		t.Fatalf("unexpected certificate evidence: %#v", result["evidence"])
	}
	if evidence["verified"] != false {
		t.Fatalf("httptest certificate must remain untrusted: %#v", evidence)
	}
	if _, ok := evidence["notAfter"].(time.Time); !ok {
		t.Fatalf("certificate expiry is missing: %#v", evidence)
	}
	for _, forbidden := range []string{"certificate", "pem", "privateKey", "serialNumber", "error"} {
		if _, exists := evidence[forbidden]; exists {
			t.Fatalf("certificate evidence must not expose %q: %#v", forbidden, evidence)
		}
	}
}

func TestManagerCertificateDiagnosticRejectsNonHTTPSManagerURL(t *testing.T) {
	result, err := ExecuteWithOptions(ProfileManagerCertificate, Options{ManagerURL: "http://manager.example"})
	if err != nil {
		t.Fatalf("execute manager certificate diagnostic: %v", err)
	}
	evidence := result["evidence"].(map[string]any)
	if evidence["available"] != false {
		t.Fatalf("non-HTTPS target must be unavailable: %#v", evidence)
	}
}
