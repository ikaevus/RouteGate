package secrets

import "testing"

func TestMask(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"rgsub_abcdefghijklmnopqrstuvwxyz123456":    "rgsub_abcd...3456",
		"rg_reg_abcdefghijklmnopqrstuvwxyz123456":   "rg_reg_abcd...3456",
		"rg_agent_abcdefghijklmnopqrstuvwxyz123456": "rg_agent_abcd...3456",
		"legacy-token":                              "lega...oken",
		"short":                                     "...",
		"   ":                                       "",
	}
	for input, want := range cases {
		if got := Mask(input); got != want {
			t.Fatalf("Mask(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestClassifyKey(t *testing.T) {
	t.Parallel()

	secretKeys := []string{
		"password",
		"registrationToken",
		"subscription_url",
		"agentToken",
		"realityPrivateKey",
		"token_hash",
	}
	for _, key := range secretKeys {
		if got := ClassifyKey(key); got != ClassificationSecret {
			t.Fatalf("ClassifyKey(%q) = %q, want secret", key, got)
		}
	}

	if got := ClassifyKey("realityPublicKey"); got != ClassificationPublic {
		t.Fatalf("ClassifyKey(realityPublicKey) = %q, want public", got)
	}
	if got := ClassifyKey("token_preview"); got != ClassificationSensitive {
		t.Fatalf("ClassifyKey(token_preview) = %q, want sensitive", got)
	}
}

func TestSanitizeMetadata(t *testing.T) {
	t.Parallel()

	sanitized := SanitizeMetadata(map[string]any{
		"password":         "secret-password",
		"registrationToken": "rg_reg_secret",
		"token_preview":    "rg_reg_abcd...wxyz",
		"public_key":       "public-value",
		"nested": map[string]any{
			"agentToken": "rg_agent_secret",
			"server":     "fi-01",
		},
	})

	if sanitized["password"] != RedactedValue {
		t.Fatalf("expected password redacted, got %#v", sanitized["password"])
	}
	if sanitized["registrationToken"] != RedactedValue {
		t.Fatalf("expected registration token redacted, got %#v", sanitized["registrationToken"])
	}
	if sanitized["token_preview"] != "rg_reg_abcd...wxyz" {
		t.Fatalf("expected token preview preserved, got %#v", sanitized["token_preview"])
	}
	if sanitized["public_key"] != "public-value" {
		t.Fatalf("expected public key preserved, got %#v", sanitized["public_key"])
	}
	nested, ok := sanitized["nested"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested metadata map, got %#v", sanitized["nested"])
	}
	if nested["agentToken"] != RedactedValue {
		t.Fatalf("expected nested agent token redacted, got %#v", nested["agentToken"])
	}
	if nested["server"] != "fi-01" {
		t.Fatalf("expected nested server preserved, got %#v", nested["server"])
	}
}
