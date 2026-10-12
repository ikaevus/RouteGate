package revocations

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestRuntimeMutationRecoveryGrantGolden(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/recovery-grant-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var v struct{ Wire, SHA256 string }
	if json.Unmarshal(data, &v) != nil {
		t.Fatal("invalid fixture")
	}
	g, err := decodeRecoveryMutationGrant([]byte(v.Wire))
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(g)
	sum := sha256.Sum256(wire)
	if err != nil || string(wire) != v.Wire || hex.EncodeToString(sum[:]) != v.SHA256 {
		t.Fatal("grant wire drift", err)
	}
	for _, bad := range []string{
		`{"schemaVersion":1,` + v.Wire[1:],
		strings.Replace(v.Wire, `"schemaVersion":1`, `"SchemaVersion":1`, 1),
		`{"untrusted":"secret",` + v.Wire[1:],
		v.Wire + `{}`,
		strings.Repeat(" ", 8192) + v.Wire,
		strings.Replace(v.Wire, `"agentCredentialGeneration":7`, `"agentCredentialGeneration":0`, 1),
		strings.Replace(v.Wire, `runtime_mutation_fence_release_v1`, `credential_removal_v1`, 1),
		strings.Replace(v.Wire, strings.Repeat("f", 64), "bad-nonce", 1),
		strings.Replace(v.Wire, `"listenerPort":443`, `"listenerPort":0`, 1),
		strings.Replace(v.Wire, `2026-10-11T20:02:00Z`, `2026-10-11T20:05:00Z`, 1),
		strings.Replace(v.Wire, `2026-10-11T20:02:00Z`, `2026-10-11T20:00:00Z`, 1),
	} {
		if _, err = decodeRecoveryMutationGrant([]byte(bad)); err == nil {
			t.Fatal("unsafe grant contract accepted")
		}
	}
}
