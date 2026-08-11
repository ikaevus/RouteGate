package secrets

import "testing"

func TestDeliverySecretKeysAreRedacted(t *testing.T) {
	metadata := map[string]any{
		"provider_api_key": "test-api-key",
		"telegram_bot_token": "test-bot-token",
		"vless_link": "test-vless-payload",
		"connect_url": "test-connect-payload",
		"qr_payload": "test-qr-payload",
		"message_body": "test-message-payload",
		"raw_provider_response": "test-provider-response",
		"recipient_masked": "f***@example.invalid",
	}

	sanitized := SanitizeMetadata(metadata)
	for _, key := range []string{
		"provider_api_key",
		"telegram_bot_token",
		"vless_link",
		"connect_url",
		"qr_payload",
		"message_body",
		"raw_provider_response",
	} {
		if sanitized[key] != RedactedValue {
			t.Fatalf("%s = %v, want redacted", key, sanitized[key])
		}
	}
	if sanitized["recipient_masked"] != "f***@example.invalid" {
		t.Fatalf("masked recipient should remain usable metadata: %v", sanitized["recipient_masked"])
	}
}
