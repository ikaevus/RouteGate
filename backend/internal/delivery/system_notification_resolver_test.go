package delivery

import (
	"strings"
	"testing"
)

func TestRenderAlertNotificationLocalizesKnownReasons(t *testing.T) {
	intent := systemNotificationIntent{
		Kind: "firing", Severity: "critical", ReasonCode: "disk_free_critical",
		Summary: "Root filesystem free space is critically low.",
	}
	title, message := renderAlertNotification("en", intent, "Moscow Edge")
	if !strings.Contains(title, "Critical") || !strings.Contains(message, "critically") {
		t.Fatalf("unexpected EN notification: %q / %q", title, message)
	}
	title, message = renderAlertNotification("ru", intent, "Moscow Edge")
	if !strings.Contains(title, "Критический") || !strings.Contains(message, "критически") {
		t.Fatalf("unexpected RU notification: %q / %q", title, message)
	}
}

func TestRenderAlertNotificationDoesNotLeakEnglishFallbackIntoRussian(t *testing.T) {
	intent := systemNotificationIntent{
		Kind: "firing", Severity: "warning", ReasonCode: "future_unknown_reason",
		Summary: "Future English-only alert summary.",
	}
	_, message := renderAlertNotification("ru", intent, "Moscow Edge")
	if strings.Contains(message, "Future English-only") || !strings.Contains(message, "требует внимания") {
		t.Fatalf("unexpected RU fallback: %q", message)
	}
}

func TestNotificationIntentIDFallsBackToDeliveryIdempotencyKey(t *testing.T) {
	id, ok := notificationIntentIDFromIdempotencyKey("alert-notification:intent-1:recipient-1")
	if !ok || id != "intent-1" {
		t.Fatalf("intent id=%q ok=%v", id, ok)
	}
	if _, ok := notificationIntentIDFromIdempotencyKey("vpn-access:other"); ok {
		t.Fatal("unrelated idempotency key must not resolve as alert intent")
	}
}
