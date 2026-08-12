package delivery

import "testing"

func TestSupportedDeliveryProvidersExcludeWhatsApp(t *testing.T) {
	if len(canonicalProviderNames) != 2 || canonicalProviderNames[0] != "smtp" || canonicalProviderNames[1] != "telegram" {
		t.Fatalf("canonical providers = %v, want [smtp telegram]", canonicalProviderNames)
	}
	if supportedProviderName("whatsapp") {
		t.Fatal("WhatsApp must remain retired from the active Delivery provider set")
	}
	if channel := channelForProvider("whatsapp"); channel != "" {
		t.Fatalf("WhatsApp channel = %q, want empty", channel)
	}
}

func TestRetiredWhatsAppChannelIsRejectedAtRequestBoundary(t *testing.T) {
	provider, recipient, err := normalizeChannelRecipient("whatsapp", "+15551234567")
	if err == nil {
		t.Fatal("WhatsApp channel must be rejected")
	}
	if err.Code != "delivery_channel_unsupported" {
		t.Fatalf("error code = %q, want delivery_channel_unsupported", err.Code)
	}
	if provider != "" || recipient != "" {
		t.Fatalf("retired channel resolved provider=%q recipient=%q", provider, recipient)
	}
}
