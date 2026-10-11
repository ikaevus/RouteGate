package tasks

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestRuntimeMutationRecoveryChallengeV1Golden(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/recovery-challenge-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var vector struct{ Wire, SHA256 string }
	if json.Unmarshal(data, &vector) != nil {
		t.Fatal("bad vector")
	}
	var c RuntimeRecoveryChallenge
	if err = json.Unmarshal([]byte(vector.Wire), &c); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(c)
	if err != nil || string(encoded) != vector.Wire || markerDigest(encoded) != vector.SHA256 {
		t.Fatal("Agent challenge serialization drift", err)
	}
	binding := RuntimeResultBinding{ManagerURL: "https://isolated.invalid", AgentID: c.Expected.AgentID, ServerID: c.Expected.ServerID}
	if !c.valid(binding, c.IssuedAt.Add(time.Second)) {
		t.Fatal("invalid Manager golden challenge")
	}
}
