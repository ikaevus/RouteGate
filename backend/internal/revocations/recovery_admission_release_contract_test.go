package revocations

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestRuntimeMutationAdmissionReleaseContract(t *testing.T) {
	read := func(name string) (string, string) {
		t.Helper()
		data, e := os.ReadFile("../../../testdata/" + name + ".json")
		var f struct{ Wire, SHA256 string }
		if e != nil || json.Unmarshal(data, &f) != nil {
			t.Fatal(e)
		}
		return f.Wire, f.SHA256
	}
	wire, hash := read("recovery-admission-release-evidence-v1")
	p, e := DecodeRecoveryAdmissionReleaseProof([]byte(wire))
	if e != nil {
		t.Fatal(e)
	}
	canonical, _ := json.Marshal(p)
	sum := sha256.Sum256(canonical)
	if string(canonical) != wire || hex.EncodeToString(sum[:]) != hash {
		t.Fatal("Manager wire/hash drift")
	}
	cleaned, cleanedHash := read("recovery-cleaned-commit-evidence-v1")
	if !admissionReleaseMatchesCleaned(p, cleaned, cleanedHash) {
		t.Fatal("predecessor mismatch")
	}
	for _, bad := range []string{wire + "{}", `{"schemaVersion":1,` + wire[1:], `{"secret":"private",` + wire[1:], strings.Replace(wire, `"schemaVersion":1`, `"SchemaVersion":1`, 1), strings.Replace(wire, `"schemaVersion":1`, `"schemaVersion":null`, 1), strings.Replace(wire, RecoveryAdmissionReleasePurpose, "released", 1), strings.Replace(wire, `"agentCredentialGeneration":7`, `"agentCredentialGeneration":0`, 1), strings.Replace(wire, strings.Repeat("4", 64), "bad", 1), strings.Replace(wire, `"listenerPort":443`, `"listenerPort":0`, 1), strings.Repeat(" ", 8192) + wire} {
		if _, e := DecodeRecoveryAdmissionReleaseProof([]byte(bad)); e == nil {
			t.Fatal("unsafe wire")
		}
	}
	c, e := DecodeRecoveryCleanedCommitProof([]byte(cleaned))
	if e != nil {
		t.Fatal(e)
	}
	for _, mode := range []string{"operation", "generation", "authorization", "proof_hash", "receipt_hash", "release_time", "observed_time", "observation", "stored_hash", "stored_wire"} {
		q := p
		data, h := cleaned, cleanedHash
		switch mode {
		case "operation":
			q.OperationID = p.Observation.AgentID
		case "generation":
			q.AgentCredentialGeneration++
		case "authorization":
			q.AuthorizationHash = strings.Repeat("f", 64)
		case "proof_hash":
			q.CleanedProofHash = strings.Repeat("f", 64)
		case "receipt_hash":
			q.CleanedReceiptHash = strings.Repeat("f", 64)
		case "release_time":
			q.ReleasedAt = c.ObservedAt.Add(-1)
		case "observed_time":
			q.ObservedAt = q.ReleasedAt.Add(-1)
		case "observation":
			q.Observation.Witness.ListenerPort++
		case "stored_hash":
			h = strings.Repeat("f", 64)
		case "stored_wire":
			data = "{}"
		}
		if admissionReleaseMatchesCleaned(q, data, h) {
			t.Fatal("foreign chain", mode)
		}
	}
}
