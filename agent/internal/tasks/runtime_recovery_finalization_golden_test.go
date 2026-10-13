package tasks

import (
	"encoding/json"
	"os"
	"testing"
)

func TestRuntimeMutationRecoveryFinalizationV1Golden(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/recovery-finalization-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var v struct{ Wire, SHA256 string }
	if json.Unmarshal(data, &v) != nil {
		t.Fatal("fixture")
	}
	var p RuntimeRecoveryFinalizationProof
	if json.Unmarshal([]byte(v.Wire), &p) != nil {
		t.Fatal("proof")
	}
	wire, err := json.Marshal(p)
	if err != nil || string(wire) != v.Wire || markerDigest(wire) != v.SHA256 {
		t.Fatal("Agent finalization proof wire/hash drift", err)
	}
}
