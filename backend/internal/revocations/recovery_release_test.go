package revocations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRecoveryReleaseAuthorizationGolden(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/recovery-release-authorization-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var v struct{ Wire, SHA256 string }
	if json.Unmarshal(data, &v) != nil {
		t.Fatal("fixture")
	}
	a, err := decodeRecoveryReleaseAuthorization([]byte(v.Wire))
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(a)
	sum := sha256.Sum256(wire)
	if err != nil || string(wire) != v.Wire || hex.EncodeToString(sum[:]) != v.SHA256 {
		t.Fatal("wire drift", err)
	}
	for _, mode := range []string{"duplicate", "alias", "unknown", "trailing", "oversize", "schema", "purpose", "operation", "nonce", "generation", "grant", "consumption", "proof", "expected", "lifetime", "zero_time"} {
		bad := v.Wire
		b := a
		switch mode {
		case "duplicate":
			bad = `{"schemaVersion":1,` + bad[1:]
		case "alias":
			bad = strings.Replace(bad, `"schemaVersion"`, `"SchemaVersion"`, 1)
		case "unknown":
			bad = `{"secret":"payload",` + bad[1:]
		case "trailing":
			bad += `{}`
		case "oversize":
			bad = strings.Repeat(" ", 8192) + bad
		case "schema":
			b.SchemaVersion++
		case "purpose":
			b.Purpose = recoveryGrantPurpose
		case "operation":
			b.OperationID = "foreign"
		case "nonce":
			b.Nonce = "bad"
		case "generation":
			b.AgentCredentialGeneration = 0
		case "grant":
			b.GrantHash = "bad"
		case "consumption":
			b.ConsumptionHash = "bad"
		case "proof":
			b.CommitProofHash = "bad"
		case "expected":
			b.Expected.Witness.ListenerPort = 0
		case "lifetime":
			b.ExpiresAt = b.ExpiresAt.Add(b.ExpiresAt.Sub(b.IssuedAt))
		case "zero_time":
			b.IssuedAt = time.Time{}
		}
		if b != a {
			data, _ := json.Marshal(b)
			bad = string(data)
		}
		if _, err = decodeRecoveryReleaseAuthorization([]byte(bad)); err == nil {
			t.Fatal("unsafe wire accepted", mode)
		}
	}
}

func TestRecoveryReleaseAuthorizationNoActor(t *testing.T) {
	// Rejection must precede even a database access and reveal no inputs.
	s := NewService(nil)
	out, err := s.IssueRecoveryReleaseAuthorization(context.Background(), RecoveryReleaseAuthorizationInput{PreparationID: "secret", ConfirmFinalFenceRelease: true})
	if err != ErrPreflight || out != (RecoveryReleaseApproval{}) {
		t.Fatal("missing actor admitted", err)
	}
}
