package revocations

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestRuntimeMutationRecoveryFinalizationGolden(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/recovery-finalization-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var v struct{ Wire, SHA256 string }
	if json.Unmarshal(data, &v) != nil {
		t.Fatal("fixture")
	}
	p, err := DecodeRecoveryFinalizationProof([]byte(v.Wire))
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(p)
	sum := sha256.Sum256(wire)
	if err != nil || string(wire) != v.Wire || hex.EncodeToString(sum[:]) != v.SHA256 {
		t.Fatal("finalization proof wire/hash drift", err)
	}
	for _, bad := range []string{
		v.Wire + `{}`,
		`{"schemaVersion":1,` + v.Wire[1:],
		`{"untrusted":"secret",` + v.Wire[1:],
		strings.Replace(v.Wire, `"schemaVersion":1`, `"SchemaVersion":1`, 1),
		strings.Replace(v.Wire, `"schemaVersion":1`, `"schemaVersion":null`, 1),
		strings.Replace(v.Wire, `"schemaVersion":1`, `"schemaVersion":2`, 1),
		strings.Replace(v.Wire, `"finalConsumptionHash":"`+strings.Repeat("8", 64)+`"`, `"finalConsumptionHash":"bad"`, 1),
		strings.Replace(v.Wire, `"observedAt":"2026-10-13T04:50:01.123456789Z"`, `"observedAt":"0001-01-01T00:00:00Z"`, 1),
		strings.Replace(v.Wire, `"listenerPort":443`, `"listenerPort":0`, 1),
		strings.Replace(v.Wire, `"operationId"`, `"operationID"`, 1),
		strings.Repeat(" ", 8192) + v.Wire,
	} {
		if _, err = DecodeRecoveryFinalizationProof([]byte(bad)); err == nil {
			t.Fatal("unsafe proof contract accepted")
		}
	}
	receipt := recoveryFinalizationReceipt(p, wire, append([]byte(" "), wire...))
	if !receipt.Recorded || receipt.ProofHash != v.SHA256 || receipt.RequestHash == receipt.ProofHash {
		t.Fatal("receipt lost exact request hash")
	}
	data, err = os.ReadFile("../../../testdata/recovery-final-acknowledgement-v1.json")
	if err != nil || json.Unmarshal(data, &v) != nil {
		t.Fatal("receipt fixture", err)
	}
	encoded, err := json.Marshal(recoveryFinalizationReceipt(p, wire, wire))
	sum = sha256.Sum256(encoded)
	if err != nil || string(encoded) != v.Wire || hex.EncodeToString(sum[:]) != v.SHA256 {
		t.Fatal("Manager receipt wire/hash drift", err)
	}
}
