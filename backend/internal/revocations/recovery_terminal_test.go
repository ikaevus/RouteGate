package revocations

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestRuntimeMutationRecoveryTerminalContract(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/recovery-finalization-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var v struct{ Wire, SHA256 string }
	if json.Unmarshal(data, &v) != nil {
		t.Fatal("fixture")
	}
	f, err := DecodeRecoveryFinalizationProof([]byte(v.Wire))
	if err != nil {
		t.Fatal(err)
	}
	ack, _ := json.Marshal(recoveryFinalizationReceipt(f, []byte(v.Wire), []byte(v.Wire)))
	hash := sha256.Sum256(ack)
	p := RecoveryTerminalProof{1, RecoveryTerminalPurpose, f.OperationID, 7, f.AuthorizationHash, f.FinalConsumptionHash, v.SHA256, hex.EncodeToString(hash[:]), strings.Repeat("b", 64), strings.Repeat("c", 64), f.ObservedAt, f.Observation}
	wire, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeRecoveryTerminalProof(wire)
	if err != nil || decoded != p || !terminalMatchesFinal(p, v.Wire, v.SHA256) {
		t.Fatal("contract", err)
	}
	var terminalGolden struct{ Wire, SHA256 string }
	data, err = os.ReadFile("../../../testdata/recovery-terminal-evidence-v1.json")
	if err != nil || json.Unmarshal(data, &terminalGolden) != nil {
		t.Fatal("terminal golden", err)
	}
	sum := sha256.Sum256(wire)
	if string(wire) != terminalGolden.Wire || hex.EncodeToString(sum[:]) != terminalGolden.SHA256 {
		t.Fatal("terminal wire/hash drift")
	}
	for _, bad := range []string{
		string(wire) + "{}", `{"schemaVersion":1,` + string(wire[1:]),
		`{"untrusted":"secret",` + string(wire[1:]),
		strings.Replace(string(wire), `"schemaVersion":1`, `"SchemaVersion":1`, 1),
		strings.Replace(string(wire), `"schemaVersion":1`, `"schemaVersion":null`, 1),
		strings.Replace(string(wire), `"schemaVersion":1`, `"schemaVersion":2`, 1),
		strings.Replace(string(wire), RecoveryTerminalPurpose, "released", 1),
		strings.Replace(string(wire), `"agentCredentialGeneration":7`, `"agentCredentialGeneration":0`, 1),
		strings.Replace(string(wire), `"intentHash":"`+strings.Repeat("b", 64)+`"`, `"intentHash":"bad"`, 1),
		strings.Replace(string(wire), `"listenerPort":443`, `"listenerPort":0`, 1),
		strings.Repeat(" ", 8192) + string(wire),
	} {
		if _, err = DecodeRecoveryTerminalProof([]byte(bad)); err == nil {
			t.Fatal("unsafe contract")
		}
	}
	for _, mode := range []string{"proof", "receipt", "authorization", "consumption", "operation", "time", "observation"} {
		q := p
		switch mode {
		case "proof":
			q.FinalProofHash = strings.Repeat("d", 64)
		case "receipt":
			q.FinalReceiptHash = strings.Repeat("d", 64)
		case "authorization":
			q.AuthorizationHash = strings.Repeat("d", 64)
		case "consumption":
			q.FinalConsumptionHash = strings.Repeat("d", 64)
		case "operation":
			q.OperationID = p.Observation.AgentID
		case "time":
			q.ObservedAt = q.ObservedAt.Add(-1)
		case "observation":
			q.Observation.Witness.ListenerPort++
		}
		if terminalMatchesFinal(q, v.Wire, v.SHA256) {
			t.Fatal("foreign final binding", mode)
		}
	}
	receipt := recoveryTerminalReceipt(p, wire, append([]byte(" "), wire...))
	if !receipt.Recorded || receipt.TerminalHash != p.TerminalHash || receipt.ProofHash == receipt.RequestHash {
		t.Fatal("request/canonical hash")
	}
	data, err = os.ReadFile("../../../testdata/recovery-terminal-acknowledgement-v1.json")
	if err != nil || json.Unmarshal(data, &terminalGolden) != nil {
		t.Fatal("terminal receipt golden", err)
	}
	ackWire, err := json.Marshal(recoveryTerminalReceipt(p, wire, wire))
	sum = sha256.Sum256(ackWire)
	if err != nil || string(ackWire) != terminalGolden.Wire || hex.EncodeToString(sum[:]) != terminalGolden.SHA256 {
		t.Fatal("terminal receipt wire/hash drift", err)
	}
}
