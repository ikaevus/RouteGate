package revocations

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRecoveryReleaseReadStrictRequest(t *testing.T) {
	in := RecoveryReleaseReadInput{RecoveryGrantReadInput: RecoveryGrantReadInput{1, "11111111-1111-4111-8111-111111111111", "33333333-3333-4333-8333-333333333333", "44444444-4444-4444-8444-444444444444", 7, strings.Repeat("a", 64)}, GrantHash: strings.Repeat("b", 64), ConsumptionHash: strings.Repeat("c", 64), CommitProofHash: strings.Repeat("d", 64)}
	wire, _ := json.Marshal(in)
	if out, err := decodeRecoveryReleaseRead(wire); err != nil || out != in {
		t.Fatal("valid request", err)
	}
	s := string(wire)
	for _, bad := range []string{s + `{}`, `{"schemaVersion":1,` + s[1:], strings.Replace(s, "nonce", "Nonce", 1), strings.Replace(s, "commitProofHash", "unknown", 1), strings.Replace(s, strings.Repeat("c", 64), "bad", 1), strings.Replace(s, `"agentCredentialGeneration":7`, `"agentCredentialGeneration":0`, 1), s + strings.Repeat(" ", 4096)} {
		if _, err := decodeRecoveryReleaseRead([]byte(bad)); err == nil {
			t.Fatal("unsafe request")
		}
	}
}
