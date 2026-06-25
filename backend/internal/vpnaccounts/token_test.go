package vpnaccounts

import "testing"

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
