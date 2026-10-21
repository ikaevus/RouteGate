package revocations

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestRecoveryAdmissionReleaseStoredHistory(t *testing.T) {
	read := func(name string) (string, string) {
		t.Helper()
		data, err := os.ReadFile("../../../testdata/" + name + ".json")
		var fixture struct{ Wire, SHA256 string }
		if err != nil || json.Unmarshal(data, &fixture) != nil {
			t.Fatalf("fixture %s: %v", name, err)
		}
		sum := sha256.Sum256([]byte(fixture.Wire))
		if hex.EncodeToString(sum[:]) != fixture.SHA256 {
			t.Fatalf("fixture %s hash", name)
		}
		return fixture.Wire, fixture.SHA256
	}
	release, releaseHash := read("recovery-admission-release-evidence-v1")
	cleaned, cleanedHash := read("recovery-cleaned-commit-evidence-v1")
	retention, retentionHash := read("recovery-retention-evidence-v1")
	prepared, preparedHash := read("recovery-prepared-release-evidence-v1")
	terminal, terminalHash := read("recovery-terminal-evidence-v1")
	final, finalHash := read("recovery-finalization-v1")
	p, err := DecodeRecoveryAdmissionReleaseProof([]byte(release))
	if err != nil {
		t.Fatal(err)
	}
	if !releaseMatchesStoredHistory(p, cleaned, cleanedHash, retention, retentionHash,
		prepared, preparedHash, terminal, terminalHash, final, finalHash) {
		t.Fatal("valid complete chain refused")
	}
	ack := recoveryAdmissionReleaseReceipt(p, []byte(release), []byte(release))
	if !ack.Recorded || ack.OperationID != p.OperationID || ack.ProofHash != releaseHash || ack.RequestHash != releaseHash {
		t.Fatal("receipt drift")
	}
	changed := recoveryAdmissionReleaseReceipt(p, []byte(release), append([]byte(" "), []byte(release)...))
	if changed.ProofHash != ack.ProofHash || changed.RequestHash == ack.RequestHash {
		t.Fatal("request/proof hashes conflated")
	}
	for _, tc := range []struct {
		name     string
		wire     string
		hash     string
		ancestor int
	}{
		{"cleaned hash", cleaned, strings.Repeat("0", 64), 0},
		{"retention hash", retention, strings.Repeat("0", 64), 1},
		{"prepared hash", prepared, strings.Repeat("0", 64), 2},
		{"terminal hash", terminal, strings.Repeat("0", 64), 3},
		{"final hash", final, strings.Repeat("0", 64), 4},
		{"changed final wire", "{}", finalHash, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wires := []string{cleaned, retention, prepared, terminal, final}
			hashes := []string{cleanedHash, retentionHash, preparedHash, terminalHash, finalHash}
			wires[tc.ancestor], hashes[tc.ancestor] = tc.wire, tc.hash
			if releaseMatchesStoredHistory(p, wires[0], hashes[0], wires[1], hashes[1],
				wires[2], hashes[2], wires[3], hashes[3], wires[4], hashes[4]) {
				t.Fatal("foreign or corrupted predecessor accepted")
			}
		})
	}
}
