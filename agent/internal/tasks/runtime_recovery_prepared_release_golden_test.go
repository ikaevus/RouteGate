package tasks

import (
	"encoding/json"
	"os"
	"testing"
)

func TestRuntimeMutationRecoveryPreparedReleaseV1Golden(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/recovery-prepared-release-evidence-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var v struct{ Wire, SHA256 string }
	if json.Unmarshal(data, &v) != nil {
		t.Fatal("fixture")
	}
	var p RuntimeRecoveryPreparedReleaseProof
	if json.Unmarshal([]byte(v.Wire), &p) != nil {
		t.Fatal("proof")
	}
	wire, err := json.Marshal(p)
	if err != nil || string(wire) != v.Wire || markerDigest(wire) != v.SHA256 {
		t.Fatal("Agent finalization proof wire/hash drift", err)
	}
	data, err = os.ReadFile("../../../testdata/recovery-prepared-release-acknowledgement-v1.json")
	if err != nil || json.Unmarshal(data, &v) != nil {
		t.Fatal("receipt fixture", err)
	}
	var receipt RuntimeRecoveryPreparedReleaseReceipt
	if json.Unmarshal([]byte(v.Wire), &receipt) != nil {
		t.Fatal("receipt")
	}
	wire, err = json.Marshal(receipt)
	if err != nil || string(wire) != v.Wire || markerDigest(wire) != v.SHA256 {
		t.Fatal("receipt wire/hash drift", err)
	}

}
