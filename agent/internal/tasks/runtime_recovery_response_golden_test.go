package tasks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

func TestRuntimeMutationRecoveryResponseGolden(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/recovery-response-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var v struct{ Wire, SHA256 string }
	if json.Unmarshal(data, &v) != nil {
		t.Fatal("invalid fixture")
	}
	var r RuntimeRecoveryChallengeResponse
	if json.Unmarshal([]byte(v.Wire), &r) != nil {
		t.Fatal("invalid wire")
	}
	wire, err := json.Marshal(r)
	sum := sha256.Sum256(wire)
	if err != nil || string(wire) != v.Wire || hex.EncodeToString(sum[:]) != v.SHA256 {
		t.Fatal("Agent response wire drift", err)
	}
}
