package tasks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStageExtractsSingBoxPayloadFromRouteGateEnvelope(t *testing.T) {
	dir := t.TempDir()
	stager := NewStager(dir)

	result, err := stager.Stage(ConfigTask{
		ID:              "job-id",
		ConfigVersionID: "version-id",
		ConfigHash:      "hash",
		RenderedConfig:  []byte(`{"schemaVersion":"routegate.config.v1","server":{"id":"server-id"},"singBox":{"log":{"level":"info"},"outbounds":[{"type":"direct","tag":"direct"}]}}`),
	})
	if err != nil {
		t.Fatalf("stage config: %v", err)
	}
	if result.StagedPath != filepath.Join(dir, "version-id.json") {
		t.Fatalf("staged path = %q", result.StagedPath)
	}
	if result.ConfigHash != "hash" {
		t.Fatalf("config hash = %q", result.ConfigHash)
	}

	content, err := os.ReadFile(result.StagedPath)
	if err != nil {
		t.Fatalf("read staged config: %v", err)
	}
	if !json.Valid(content) {
		t.Fatalf("staged config is not valid JSON: %s", content)
	}
	if strings.Contains(string(content), "routegate.config.v1") || strings.Contains(string(content), "server-id") {
		t.Fatalf("staged config must not include RouteGate envelope metadata: %s", content)
	}
	if !strings.Contains(string(content), `"outbounds"`) || !strings.Contains(string(content), `"direct"`) {
		t.Fatalf("staged config must include sing-box payload: %s", content)
	}
	if !strings.HasSuffix(string(content), "\n") {
		t.Fatalf("staged config must end with newline: %s", content)
	}
	if _, err := os.Stat(result.StagedPath + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temporary file should not remain after successful stage")
	}
}

func TestStageKeepsPlainSingBoxConfig(t *testing.T) {
	dir := t.TempDir()
	stager := NewStager(dir)

	result, err := stager.Stage(ConfigTask{
		ID:              "job-id",
		ConfigVersionID: "version-id",
		ConfigHash:      "hash",
		RenderedConfig:  []byte(`{"log":{"level":"info"},"outbounds":[{"type":"direct","tag":"direct"}]}`),
	})
	if err != nil {
		t.Fatalf("stage config: %v", err)
	}

	content, err := os.ReadFile(result.StagedPath)
	if err != nil {
		t.Fatalf("read staged config: %v", err)
	}
	if !strings.Contains(string(content), `"outbounds"`) || !strings.Contains(string(content), `"direct"`) {
		t.Fatalf("staged config must keep plain sing-box payload: %s", content)
	}
}

func TestStageRejectsRouteGateEnvelopeWithoutSingBoxPayload(t *testing.T) {
	_, err := NewStager(t.TempDir()).Stage(ConfigTask{
		ID:              "job-id",
		ConfigVersionID: "version-id",
		RenderedConfig:  []byte(`{"schemaVersion":"routegate.config.v1","server":{"id":"server-id"}}`),
	})
	if err == nil {
		t.Fatal("stage should reject RouteGate envelope without singBox payload")
	}
}

func TestStageRejectsInvalidJSON(t *testing.T) {
	_, err := NewStager(t.TempDir()).Stage(ConfigTask{
		ID:              "job-id",
		ConfigVersionID: "version-id",
		RenderedConfig:  []byte(`not-json`),
	})
	if err == nil {
		t.Fatal("stage should reject invalid JSON")
	}
}
