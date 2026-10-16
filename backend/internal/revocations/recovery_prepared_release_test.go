package revocations

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestRuntimeMutationRecoveryPreparedReleaseContract(t *testing.T) {
	read := func(name string) (string, string) {
		t.Helper()
		data, err := os.ReadFile("../../../testdata/" + name + ".json")
		var v struct{ Wire, SHA256 string }
		if err != nil || json.Unmarshal(data, &v) != nil {
			t.Fatal("fixture", err)
		}
		return v.Wire, v.SHA256
	}
	wire, hash := read("recovery-prepared-release-evidence-v1")
	p, err := DecodeRecoveryPreparedReleaseProof([]byte(wire))
	if err != nil {
		t.Fatal(err)
	}
	canonical, _ := json.Marshal(p)
	sum := sha256.Sum256(canonical)
	if string(canonical) != wire || hex.EncodeToString(sum[:]) != hash {
		t.Fatal("wire/hash drift")
	}
	terminal, terminalHash := read("recovery-terminal-evidence-v1")
	if !preparedReleaseMatchesTerminal(p, terminal, terminalHash) {
		t.Fatal("terminal chain")
	}
	for _, bad := range []string{wire + "{}", `{"schemaVersion":1,` + wire[1:], `{"secret":"nonce",` + wire[1:], strings.Replace(wire, `"schemaVersion":1`, `"SchemaVersion":1`, 1), strings.Replace(wire, `"schemaVersion":1`, `"schemaVersion":null`, 1), strings.Replace(wire, RecoveryPreparedReleasePurpose, "released", 1), strings.Replace(wire, `"agentCredentialGeneration":7`, `"agentCredentialGeneration":0`, 1), strings.Replace(wire, strings.Repeat("d", 64), "bad", 1), strings.Replace(wire, `"listenerPort":443`, `"listenerPort":0`, 1), strings.Repeat(" ", 8192) + wire} {
		if _, err = DecodeRecoveryPreparedReleaseProof([]byte(bad)); err == nil {
			t.Fatal("unsafe wire")
		}
	}
	for _, mode := range []string{"proof", "receipt", "authorization", "operation", "generation", "prepared", "observed", "observation"} {
		q := p
		switch mode {
		case "proof":
			q.TerminalProofHash = strings.Repeat("e", 64)
		case "receipt":
			q.TerminalReceiptHash = strings.Repeat("e", 64)
		case "authorization":
			q.AuthorizationHash = strings.Repeat("e", 64)
		case "operation":
			q.OperationID = p.Observation.AgentID
		case "generation":
			q.AgentCredentialGeneration++
		case "prepared":
			q.PreparedAt = q.PreparedAt.Add(-1)
		case "observed":
			q.ObservedAt = q.PreparedAt.Add(-1)
		case "observation":
			q.Observation.Witness.ListenerPort++
		}
		if preparedReleaseMatchesTerminal(q, terminal, terminalHash) {
			t.Fatal("foreign chain", mode)
		}
	}
	ack, ackHash := read("recovery-prepared-release-acknowledgement-v1")
	r := recoveryPreparedReleaseReceipt(p, canonical, canonical)
	aw, _ := json.Marshal(r)
	sum = sha256.Sum256(aw)
	if string(aw) != ack || hex.EncodeToString(sum[:]) != ackHash {
		t.Fatal("receipt wire/hash drift")
	}
	pretty := append([]byte(" "), canonical...)
	retry := recoveryPreparedReleaseReceipt(p, canonical, pretty)
	if retry.ProofHash != r.ProofHash || retry.RequestHash == r.RequestHash {
		t.Fatal("request hash")
	}
}
