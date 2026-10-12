package tasks

import (
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
	var r RuntimeRecoveryCommitReceipt
	if json.Unmarshal([]byte(v.Wire), &r) != nil {
		t.Fatal("receipt")
	}
	wire, err := json.Marshal(r)
	if err != nil || string(wire) != v.Wire || markerDigest(wire) != v.SHA256 {
		t.Fatal("Agent receipt wire/hash drift", err)
	}
}
