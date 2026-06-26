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

func TestHashTokenTrimsWhitespace(t *testing.T) {
	t.Parallel()

	if HashToken(" rg_agent_token ") != HashToken("rg_agent_token") {
		t.Fatal("expected token hashing to trim whitespace")
	}
}

func TestMaskToken(t *testing.T) {
	t.Parallel()

	if got := MaskToken("rg_reg_abcdefghijklmnopqrstuvwxyz123456"); got != "rg_reg_abcd...3456" {
		t.Fatalf("registration token mask = %q", got)
	}
	if got := MaskToken("rg_agent_abcdefghijklmnopqrstuvwxyz123456"); got != "rg_agent_abcd...3456" {
		t.Fatalf("agent token mask = %q", got)
	}
	if got := MaskToken("legacy-token"); got != "lega...oken" {
		t.Fatalf("legacy token mask = %q", got)
	}
	if got := MaskToken("short"); got != "..." {
		t.Fatalf("short token mask = %q", got)
	}
	if got := MaskToken("   "); got != "" {
		t.Fatalf("empty token mask = %q", got)
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

func TestGenerateAgentToken(t *testing.T) {
	t.Parallel()

	first, err := GenerateAgentToken()
	if err != nil {
		t.Fatalf("GenerateAgentToken() error = %v", err)
	}
	second, err := GenerateAgentToken()
	if err != nil {
		t.Fatalf("GenerateAgentToken() second error = %v", err)
	}
	if !strings.HasPrefix(first, "rg_agent_") {
		t.Fatalf("token %q does not have rg_agent_ prefix", first)
	}
	if first == second {
		t.Fatal("two generated agent tokens were identical")
	}
}
