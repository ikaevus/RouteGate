package vpnaccounts

import (
	"strings"
	"testing"
)

func TestHashSubscriptionTokenDeterministic(t *testing.T) {
	first := HashSubscriptionToken("fixed-token")
	second := HashSubscriptionToken("fixed-token")

	if first == "" {
		t.Fatal("expected non-empty token hash")
	}
	if first != second {
		t.Fatalf("expected deterministic hash, got %q and %q", first, second)
	}
	if first == "fixed-token" {
		t.Fatal("expected hash to differ from raw token")
	}
}

func TestHashSubscriptionTokenTrimsWhitespace(t *testing.T) {
	if HashSubscriptionToken(" fixed-token ") != HashSubscriptionToken("fixed-token") {
		t.Fatal("expected subscription token hashing to trim whitespace")
	}
}

func TestGenerateSubscriptionTokenReturnsDifferentValues(t *testing.T) {
	first, err := GenerateSubscriptionToken()
	if err != nil {
		t.Fatalf("generate first token: %v", err)
	}
	second, err := GenerateSubscriptionToken()
	if err != nil {
		t.Fatalf("generate second token: %v", err)
	}

	if first == "" || second == "" {
		t.Fatal("expected non-empty generated tokens")
	}
	if first == second {
		t.Fatal("expected generated tokens to differ")
	}
}

func TestGenerateSubscriptionTokenUsesRouteGatePrefixAndEntropy(t *testing.T) {
	token, err := GenerateSubscriptionToken()
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	if !strings.HasPrefix(token, subscriptionTokenPrefix) {
		t.Fatalf("expected token prefix %q, got %q", subscriptionTokenPrefix, token)
	}
	if len(strings.TrimPrefix(token, subscriptionTokenPrefix)) < 40 {
		t.Fatalf("expected high-entropy token body, got length %d", len(strings.TrimPrefix(token, subscriptionTokenPrefix)))
	}
}

func TestMaskSubscriptionToken(t *testing.T) {
	if got := MaskSubscriptionToken("rgsub_abcdefghijklmnopqrstuvwxyz123456"); got != "rgsub_abcd...3456" {
		t.Fatalf("unexpected prefixed token mask %q", got)
	}
	if got := MaskSubscriptionToken("fixed-token"); got != "fixe...oken" {
		t.Fatalf("unexpected legacy token mask %q", got)
	}
	if got := MaskSubscriptionToken("short"); got != "..." {
		t.Fatalf("unexpected short token mask %q", got)
	}
}
