package revocations

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestRuntimeMutationRecoveryCommitGolden(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/recovery-commit-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var v struct{ Wire, SHA256 string }
	if json.Unmarshal(data, &v) != nil {
		t.Fatal("fixture")
	}
	p, err := DecodeRecoveryCommitProof([]byte(v.Wire))
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(p)
	sum := sha256.Sum256(wire)
	if err != nil || string(wire) != v.Wire || hex.EncodeToString(sum[:]) != v.SHA256 {
		t.Fatal("commit proof wire/hash drift", err)
	}
	for _, bad := range []string{
		v.Wire + `{}`,
		`{"schemaVersion":1,` + v.Wire[1:],
		`{"untrusted":"secret",` + v.Wire[1:],
		strings.Replace(v.Wire, `"schemaVersion":1`, `"SchemaVersion":1`, 1),
		strings.Replace(v.Wire, `"schemaVersion":1`, `"schemaVersion":null`, 1),
		strings.Replace(v.Wire, `"schemaVersion":1`, `"schemaVersion":2`, 1),
		strings.Replace(v.Wire, `"consumptionHash":"`+strings.Repeat("9", 64)+`"`, `"consumptionHash":"bad"`, 1),
		strings.Replace(v.Wire, `"observedAt":"2026-10-11T20:01:02Z"`, `"observedAt":"0001-01-01T00:00:00Z"`, 1),
		strings.Replace(v.Wire, `"listenerPort":443`, `"listenerPort":0`, 1),
		strings.Replace(v.Wire, `"operationId"`, `"operationID"`, 1),
		strings.Repeat(" ", 8192) + v.Wire,
	} {
		if _, err = DecodeRecoveryCommitProof([]byte(bad)); err == nil {
			t.Fatal("unsafe proof contract accepted")
		}
	}
	receipt := recoveryCommitReceipt(p, wire, append([]byte(" "), wire...))
	if !receipt.Recorded || receipt.ProofHash != v.SHA256 || receipt.RequestHash == receipt.ProofHash {
		t.Fatal("receipt lost exact request hash")
	}
}
