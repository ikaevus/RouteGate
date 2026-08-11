package delivery

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDeliveryResponseMasksRecipientAndOmitsSensitiveFields(t *testing.T) {
	item := Delivery{
		ID:                "11111111-1111-1111-1111-111111111111",
		VPNAccountID:      "22222222-2222-2222-2222-222222222222",
		Channel:           "email",
		Provider:          "smtp",
		Recipient:         "felix@example.invalid",
		TemplateKey:       TemplateVPNAccess,
		Locale:            "en",
		Status:            StatusSent,
		AttemptCount:      1,
		MaxAttempts:       5,
		ProviderReference: "provider-message-fixture",
		IdempotencyKey:    "idempotency-secret-like-fixture",
		CreatedAt:         time.Date(2026, 8, 11, 20, 0, 0, 0, time.UTC),
		UpdatedAt:         time.Date(2026, 8, 11, 20, 0, 1, 0, time.UTC),
	}

	response := toDeliveryResponse(item)
	payload, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	text := string(payload)
	if response.RecipientDisplay != "f***@example.invalid" {
		t.Fatalf("masked recipient=%q", response.RecipientDisplay)
	}
	for _, forbidden := range []string{
		"felix@example.invalid",
		"provider-message-fixture",
		"idempotency-secret-like-fixture",
		"vless://",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("safe response leaked %q: %s", forbidden, text)
		}
	}
}

func TestIdempotencyKeyValidationIsBoundedAndHeaderSafe(t *testing.T) {
	for _, value := range []string{"request-123", "8chars__", "client:request.123"} {
		if !validIdempotencyKey(value) {
			t.Fatalf("valid key rejected: %q", value)
		}
	}
	for _, value := range []string{"short", "contains space", "contains/slash", strings.Repeat("a", 201)} {
		if validIdempotencyKey(value) {
			t.Fatalf("invalid key accepted: %q", value)
		}
	}
}
