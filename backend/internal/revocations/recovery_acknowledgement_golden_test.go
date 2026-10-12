package revocations

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

func TestRuntimeMutationRecoveryAcknowledgementGolden(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/recovery-acknowledgement-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var v struct{ Wire, SHA256 string }
	if json.Unmarshal(data, &v) != nil {
		t.Fatal("fixture")
	}
	var r RecoveryCommitReceipt
	if json.Unmarshal([]byte(v.Wire), &r) != nil {
		t.Fatal("receipt")
	}
	wire, err := json.Marshal(r)
	sum := sha256.Sum256(wire)
	if err != nil || string(wire) != v.Wire || hex.EncodeToString(sum[:]) != v.SHA256 {
		t.Fatal("Manager receipt wire/hash drift", err)
	}
}
