package audit

import "testing"

func TestSanitizeMetadataDefaultsToEmptyMap(t *testing.T) {
	sanitized := SanitizeMetadata(nil)
	if sanitized == nil {
		t.Fatal("expected non-nil metadata map")
	}
	if len(sanitized) != 0 {
		t.Fatalf("expected empty metadata, got %#v", sanitized)
	}
}

func TestSanitizeMetadataRedactsSensitiveValues(t *testing.T) {
	sanitized := SanitizeMetadata(map[string]any{
		"password":          "secret-password",
		"registrationToken": "rgst_secret_token",
		"subscription_url":  "https://example.test/subscription/secret",
		"realityPrivateKey": "private-key",
		"email":             "admin@routegate.local",
		"token_preview":     "abcd...wxyz",
		"nested": map[string]any{
			"agent_credentials": "agent-secret",
			"server_name":       "fi-01",
		},
	})

	for _, key := range []string{"password", "registrationToken", "subscription_url", "realityPrivateKey"} {
		if sanitized[key] != redactedValue {
			t.Fatalf("expected %s to be redacted, got %#v", key, sanitized[key])
		}
	}
	if sanitized["email"] != "admin@routegate.local" {
		t.Fatalf("expected email to be preserved, got %#v", sanitized["email"])
	}
	if sanitized["token_preview"] != "abcd...wxyz" {
		t.Fatalf("expected token preview to be preserved, got %#v", sanitized["token_preview"])
	}

	nested, ok := sanitized["nested"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested metadata map, got %#v", sanitized["nested"])
	}
	if nested["agent_credentials"] != redactedValue {
		t.Fatalf("expected nested agent credentials to be redacted, got %#v", nested["agent_credentials"])
	}
	if nested["server_name"] != "fi-01" {
		t.Fatalf("expected nested server name to be preserved, got %#v", nested["server_name"])
	}
}

func TestMaskSecret(t *testing.T) {
	if got := MaskSecret("abcd1234wxyz"); got != "abcd...wxyz" {
		t.Fatalf("expected masked secret, got %q", got)
	}
	if got := MaskSecret("short"); got != "..." {
		t.Fatalf("expected short secret mask, got %q", got)
	}
	if got := MaskSecret("   "); got != "" {
		t.Fatalf("expected empty secret mask, got %q", got)
	}
}
