package agents

import (
	"strings"
	"testing"
)

func TestHashToken(t *testing.T) {
	t.Parallel()

	const want = "4d639a937318e7110946aa9f9afbd5d0ace3e03bbd80a580774abaa3f2398cd9"
	if got := HashToken("routegate-agent-token"); got != want {
		t.Fatalf("HashToken() = %q, want %q", got, want)
	}
}

func TestGenerateRegistrationToken(t *testing.T) {
	t.Parallel()

	first, err := GenerateRegistrationToken()
	if err != nil {
		t.Fatalf("GenerateRegistrationToken() error = %v", err)
	}
	second, err := GenerateRegistrationToken()
	if err != nil {
		t.Fatalf("GenerateRegistrationToken() second error = %v", err)
	}
	if !strings.HasPrefix(first, "rg_reg_") {
		t.Fatalf("token %q does not have rg_reg_ prefix", first)
	}
	if first == second {
		t.Fatal("two generated registration tokens were identical")
	}
}
