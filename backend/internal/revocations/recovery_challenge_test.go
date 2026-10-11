package revocations

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

func TestRecoveryChallengeV1Golden(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/recovery-challenge-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var vector struct{ Wire, SHA256 string }
	if json.Unmarshal(data, &vector) != nil {
		t.Fatal("bad vector")
	}
	var c RecoveryChallenge
	if err = json.Unmarshal([]byte(vector.Wire), &c); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(c)
	sum := sha256.Sum256(encoded)
	if err != nil || string(encoded) != vector.Wire || hex.EncodeToString(sum[:]) != vector.SHA256 {
		t.Fatal("Manager challenge serialization drift", err)
	}
	observation, _ := json.Marshal(c.Expected)
	if _, err = decodeRecoveryObservation(observation); err != nil {
		t.Fatal("invalid golden observation", err)
	}
}
