package delivery

import "testing"

func TestTelegramStartParameter(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
		ok   bool
	}{
		{name: "plain start", text: "/start abc_DEF-123", want: "abc_DEF-123", ok: true},
		{name: "addressed start", text: "/start@RouteGateVPNBot token-1", want: "token-1", ok: true},
		{name: "missing parameter", text: "/start", ok: false},
		{name: "other command", text: "/help token", ok: false},
		{name: "too many fields", text: "/start token extra", ok: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := telegramStartParameter(test.text)
			if ok != test.ok || got != test.want {
				t.Fatalf("telegramStartParameter(%q) = %q, %v; want %q, %v", test.text, got, ok, test.want, test.ok)
			}
		})
	}
}

func TestTelegramDisplayName(t *testing.T) {
	if got := telegramDisplayName(TelegramIncomingUpdate{FirstName: "Felix", LastName: "Admin", Username: "felix"}); got != "Felix Admin" {
		t.Fatalf("display name = %q", got)
	}
	if got := telegramDisplayName(TelegramIncomingUpdate{Username: "felix"}); got != "@felix" {
		t.Fatalf("username fallback = %q", got)
	}
	if got := telegramDisplayName(TelegramIncomingUpdate{}); got != "Telegram" {
		t.Fatalf("default fallback = %q", got)
	}
}

func TestGenerateTelegramStartParameterIsBounded(t *testing.T) {
	first, err := generateTelegramStartParameter()
	if err != nil {
		t.Fatal(err)
	}
	second, err := generateTelegramStartParameter()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("pairing parameters must be random")
	}
	if len(first) == 0 || len(first) > 64 {
		t.Fatalf("pairing parameter length = %d", len(first))
	}
}
