package revocations

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRecoveryGrantReadStrictRequest(t *testing.T) {
	in := RecoveryGrantReadInput{1, "11111111-1111-4111-8111-111111111111", "33333333-3333-4333-8333-333333333333", "44444444-4444-4444-8444-444444444444", 7, strings.Repeat("a", 64)}
	wire, _ := json.Marshal(in)
	valid := string(wire)
	if got, err := decodeRecoveryGrantRead(wire); err != nil || got != in {
		t.Fatal("valid request", err)
	}
	for _, data := range []string{valid + "{}", strings.Replace(valid, `"schemaVersion":1`, `"schemaVersion":1,"schemaVersion":1`, 1), strings.Replace(valid, "operationId", "OperationID", 1), strings.Replace(valid, "nonce", "other", 1), strings.Replace(valid, strings.Repeat("a", 64), "bad", 1), strings.Replace(valid, `"agentCredentialGeneration":7`, `"agentCredentialGeneration":0`, 1), strings.Replace(valid, `"schemaVersion":1`, `"schemaVersion":null`, 1), valid + strings.Repeat(" ", 4096)} {
		if _, err := decodeRecoveryGrantRead([]byte(data)); err == nil {
			t.Fatal("unsafe request accepted")
		}
	}
}
