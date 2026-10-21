package revocations

import "testing"

func TestPreparedRemovalJSONExactNumericSemantics(t *testing.T) {
	if !samePreparedRemovalJSON(
		[]byte(`{"a":{"x":9007199254740993,"y":[1,2]},"b":true}`),
		[]byte(`{"b":true,"a":{"y":[1,2],"x":9007199254740993}}`)) {
		t.Fatal("semantic JSON reordering rejected")
	}
	for _, altered := range []string{
		`{"a":{"x":9007199254740992,"y":[1,2]},"b":true}`,
		`{"a":{"x":9007199254740993,"y":[2,1]},"b":true}`,
		`{"a":{"x":9007199254740993,"y":[1,2]},"b":false}`,
		`{"a":{"x":9007199254740993,"y":[1,2]},"b":true,"extra":1}`,
		`broken`,
	} {
		if samePreparedRemovalJSON([]byte(`{"a":{"x":9007199254740993,"y":[1,2]},"b":true}`), []byte(altered)) {
			t.Fatal("tampered candidate JSON matched frozen payload")
		}
	}
}
