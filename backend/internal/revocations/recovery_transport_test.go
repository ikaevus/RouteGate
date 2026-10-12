package revocations

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestRuntimeMutationRecoveryResponseGolden(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/recovery-response-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var v struct{ Wire, SHA256 string }
	if json.Unmarshal(data, &v) != nil {
		t.Fatal("invalid fixture")
	}
	r, err := DecodeRecoveryChallengeResponse([]byte(v.Wire))
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(r)
	sum := sha256.Sum256(wire)
	if err != nil || string(wire) != v.Wire || hex.EncodeToString(sum[:]) != v.SHA256 {
		t.Fatal("response wire drift", err)
	}
	for _, bad := range []string{
		`{"schemaVersion":1,` + v.Wire[1:],
		strings.Replace(v.Wire, `"schemaVersion":1`, `"SchemaVersion":1`, 1),
		`{"unexpected":"secret",` + v.Wire[1:],
		v.Wire + `{}`,
		strings.Repeat(" ", 8192) + v.Wire,
		strings.Replace(v.Wire, `"listenerPort":443`, `"listenerPort":0`, 1),
	} {
		if _, err = DecodeRecoveryChallengeResponse([]byte(bad)); err == nil {
			t.Fatal("unsafe response accepted")
		}
	}
}
