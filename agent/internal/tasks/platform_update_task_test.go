package tasks

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodePlatformUpdateRequestAcceptsCanonicalVersion(t *testing.T) {
	request, err := DecodePlatformUpdateRequest(json.RawMessage(`{"schemaVersion":1,"targetVersion":"v1.2.3"}`))
	if err != nil {
		t.Fatalf("DecodePlatformUpdateRequest() error = %v", err)
	}
	if request.TargetVersion != "v1.2.3" {
		t.Fatalf("TargetVersion = %q, want v1.2.3", request.TargetVersion)
	}
}

func TestDecodePlatformUpdateRequestRejectsPrivilegedSelectors(t *testing.T) {
	cases := []string{
		`{"schemaVersion":1,"targetVersion":"v1.2.3","url":"https://example.com/release"}`,
		`{"schemaVersion":1,"targetVersion":"v1.2.3","path":"/tmp/bundle"}`,
		`{"schemaVersion":1,"targetVersion":"v1.2.3","command":"sh -c id"}`,
		`{"schemaVersion":1,"targetVersion":"v1.2.3","repository":"other/project"}`,
		`{"schemaVersion":1,"targetVersion":"v1.2.3","checksum":"deadbeef"}`,
		`{"schemaVersion":1,"targetVersion":"v1.2.3","role":"hybrid"}`,
	}
	for _, payload := range cases {
		if _, err := DecodePlatformUpdateRequest(json.RawMessage(payload)); err == nil {
			t.Fatalf("DecodePlatformUpdateRequest(%s) unexpectedly succeeded", payload)
		}
	}
}

func TestDecodePlatformUpdateRequestRejectsInvalidShape(t *testing.T) {
	cases := []string{
		``,
		`{}`,
		`{"schemaVersion":2,"targetVersion":"v1.2.3"}`,
		`{"schemaVersion":1,"targetVersion":"latest"}`,
		`{"schemaVersion":1,"targetVersion":"../v1.2.3"}`,
		`{"schemaVersion":1,"targetVersion":"https://example.com/v1.2.3"}`,
		`{"schemaVersion":1,"targetVersion":"v1.2.3"} {"schemaVersion":1,"targetVersion":"v1.2.4"}`,
	}
	for _, payload := range cases {
		if _, err := DecodePlatformUpdateRequest(json.RawMessage(payload)); err == nil {
			t.Fatalf("DecodePlatformUpdateRequest(%q) unexpectedly succeeded", payload)
		}
	}
}

func TestDecodePlatformUpdateRequestRejectsOversizedPayload(t *testing.T) {
	payload := `{"schemaVersion":1,"targetVersion":"v1.2.3","padding":"` + strings.Repeat("x", 300) + `"}`
	if _, err := DecodePlatformUpdateRequest(json.RawMessage(payload)); err == nil {
		t.Fatal("DecodePlatformUpdateRequest() unexpectedly accepted oversized payload")
	}
}
