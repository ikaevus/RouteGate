package tasks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStageWritesRenderedConfigAtomically(t *testing.T) {
	dir := t.TempDir()
	stager := NewStager(dir)

	result, err := stager.Stage(ConfigTask{
		ID:              "job-id",
		ConfigVersionID: "version-id",
		ConfigHash:      "hash",
		RenderedConfig:  []byte(`{"schemaVersion":"routegate.config.v1","server":{"id":"server-id"}}`),
	})
	if err != nil {
		t.Fatalf("stage config: %v", err)
	}
	if result.StagedPath != filepath.Join(dir, "version-id.json") {
		t.Fatalf("staged path = %q", result.StagedPath)
	}
	content, err := os.ReadFile(result.StagedPath)
	if err != nil {
		t.Fatalf("read staged config: %v", err)
	}
	if !strings.Contains(string(content), "routegate.config.v1") || !strings.HasSuffix(string(content), "\n") {
		t.Fatalf("unexpected staged config content: %s", content)
	}
	if _, err := os.Stat(result.StagedPath + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temporary file should not remain after successful stage")
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
