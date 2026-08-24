package updates

import "testing"

func TestNewUpdateJobIDProducesUUIDv4(t *testing.T) {
	id, err := newUpdateJobID()
	if err != nil {
		t.Fatalf("generate update job ID: %v", err)
	}
	if !uuidPattern.MatchString(id) {
		t.Fatalf("generated ID %q is not a UUID", id)
	}
	if id[14] != '4' {
		t.Fatalf("generated ID %q is not UUIDv4", id)
	}
	switch id[19] {
	case '8', '9', 'a', 'b':
	default:
		t.Fatalf("generated ID %q has invalid RFC 4122 variant", id)
	}
}
