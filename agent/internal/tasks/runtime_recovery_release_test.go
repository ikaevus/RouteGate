package tasks

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestRuntimeMutationRecoveryReleaseGolden(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/recovery-release-authorization-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var v struct{ Wire, SHA256 string }
	json.Unmarshal(data, &v)
	var a RuntimeRecoveryReleaseAuthorization
	if json.Unmarshal([]byte(v.Wire), &a) != nil {
		t.Fatal("fixture")
	}
	wire, _ := json.Marshal(a)
	if string(wire) != v.Wire || markerDigest(wire) != v.SHA256 {
		t.Fatal("Manager/Agent wire drift")
	}
	p := RuntimeRecoveryCommitProof{1, a.OperationID, a.GrantHash, a.ConsumptionHash, a.IssuedAt.Add(-time.Second), a.Expected}
	proofWire, _ := json.Marshal(p)
	a.CommitProofHash = markerDigest(proofWire)
	b := RuntimeRecoveryGrantBinding{RuntimeResultBinding: RuntimeResultBinding{ManagerURL: "https://isolated.invalid", AgentID: a.Expected.AgentID, ServerID: a.Expected.ServerID}, AgentCredentialGeneration: 7}
	wire, _ = json.Marshal(a)
	if _, _, err = ValidateRuntimeRecoveryReleaseAuthorization(wire, b, p, a.IssuedAt); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{append([]byte(`{"schemaVersion":1,`), wire[1:]...), append(wire, []byte(`{}`)...)} {
		if _, _, err = ValidateRuntimeRecoveryReleaseAuthorization(bad, b, p, a.IssuedAt); err == nil {
			t.Fatal("malformed authority accepted")
		}
	}
	if _, _, err = ValidateRuntimeRecoveryReleaseAuthorization(wire, b, p, a.ExpiresAt); err == nil {
		t.Fatal("expired authority")
	}
	b.AgentCredentialGeneration++
	if _, _, err = ValidateRuntimeRecoveryReleaseAuthorization(wire, b, p, a.IssuedAt); err == nil {
		t.Fatal("generation copied from authority")
	}
}
