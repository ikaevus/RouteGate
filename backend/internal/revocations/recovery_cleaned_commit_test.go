package revocations

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestRuntimeMutationRecoveryCleanedCommitContract(t *testing.T) {
	read := func(name string) (string, string) {
		t.Helper()
		data, err := os.ReadFile("../../../testdata/" + name + ".json")
		var v struct{ Wire, SHA256 string }
		if err != nil || json.Unmarshal(data, &v) != nil {
			t.Fatal("fixture", err)
		}
		return v.Wire, v.SHA256
	}
	wire, hash := read("recovery-cleaned-commit-evidence-v1")
	p, err := DecodeRecoveryCleanedCommitProof([]byte(wire))
	if err != nil {
		t.Fatal(err)
	}
	canonical, _ := json.Marshal(p)
	sum := sha256.Sum256(canonical)
	if string(canonical) != wire || hex.EncodeToString(sum[:]) != hash {
		t.Fatal("wire/hash drift")
	}
	retained, retainedHash := read("recovery-retention-evidence-v1")
	prepared, preparedHash := read("recovery-prepared-release-evidence-v1")
	if !cleanedCommitMatchesRetention(p, retained, retainedHash, prepared, preparedHash) {
		t.Fatal("predecessor chain")
	}
	for _, bad := range []string{wire + "{}", `{"schemaVersion":1,` + wire[1:], `{"secret":"nonce",` + wire[1:], strings.Replace(wire, `"schemaVersion":1`, `"SchemaVersion":1`, 1), strings.Replace(wire, `"schemaVersion":1`, `"schemaVersion":null`, 1), strings.Replace(wire, RecoveryCleanedCommitPurpose, "released", 1), strings.Replace(wire, `"agentCredentialGeneration":7`, `"agentCredentialGeneration":0`, 1), strings.Replace(wire, strings.Repeat("2", 64), "bad", 1), strings.Replace(wire, `"listenerPort":443`, `"listenerPort":0`, 1), strings.Repeat(" ", 8192) + wire} {
		if _, err = DecodeRecoveryCleanedCommitProof([]byte(bad)); err == nil {
			t.Fatal("unsafe wire")
		}
	}
	previous, err := DecodeRecoveryRetentionProof([]byte(retained))
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"proof", "receipt", "release_intent", "authorization", "operation", "generation", "intent_time", "observed", "observation", "retention_hash", "prepared_hash"} {
		q := p
		rh, ph := retainedHash, preparedHash
		switch mode {
		case "proof":
			q.RetentionProofHash = strings.Repeat("e", 64)
		case "receipt":
			q.RetentionReceiptHash = strings.Repeat("e", 64)
		case "release_intent":
			q.ReleaseIntentHash = strings.Repeat("e", 64)
		case "authorization":
			q.AuthorizationHash = strings.Repeat("e", 64)
		case "operation":
			q.OperationID = p.Observation.AgentID
		case "generation":
			q.AgentCredentialGeneration++
		case "intent_time":
			q.IntentAt = previous.ObservedAt.Add(-1)
		case "observed":
			q.ObservedAt = q.IntentAt.Add(-1)
		case "observation":
			q.Observation.Witness.ListenerPort++
		case "retention_hash":
			rh = strings.Repeat("e", 64)
		case "prepared_hash":
			ph = strings.Repeat("e", 64)
		}
		if cleanedCommitMatchesRetention(q, retained, rh, prepared, ph) {
			t.Fatal("foreign chain", mode)
		}
	}
	ack, ackHash := read("recovery-cleaned-commit-acknowledgement-v1")
	receipt := recoveryCleanedCommitReceipt(p, canonical, canonical)
	aw, _ := json.Marshal(receipt)
	sum = sha256.Sum256(aw)
	if string(aw) != ack || hex.EncodeToString(sum[:]) != ackHash {
		t.Fatal("receipt wire/hash drift")
	}
	retry := recoveryCleanedCommitReceipt(p, canonical, append([]byte(" "), canonical...))
	if retry.ProofHash != receipt.ProofHash || retry.RequestHash == receipt.RequestHash {
		t.Fatal("request hash")
	}
}
