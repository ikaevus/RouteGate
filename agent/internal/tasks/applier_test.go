package tasks

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApplyPromotesStagedConfigAndBacksUpActiveConfig(t *testing.T) {
	dir := t.TempDir()
	stagedPath := filepath.Join(dir, "staged.json")
	activePath := filepath.Join(dir, "active", "config.json")
	backupDir := filepath.Join(dir, "backups")
	if err := os.MkdirAll(filepath.Dir(activePath), 0o750); err != nil {
		t.Fatalf("create active dir: %v", err)
	}
	if err := os.WriteFile(stagedPath, []byte(`{"new":true}`), 0o600); err != nil {
		t.Fatalf("write staged config: %v", err)
	}
	if err := os.WriteFile(activePath, []byte(`{"old":true}`), 0o600); err != nil {
		t.Fatalf("write active config: %v", err)
	}

	result, err := NewApplier(activePath, backupDir).Apply(stagedPath, "version-id")
	if err != nil {
		t.Fatalf("apply config: %v", err)
	}
	if result.ActivePath != activePath || result.BackupPath != filepath.Join(backupDir, "version-id.previous.json") {
		t.Fatalf("unexpected result: %+v", result)
	}
	activeContent, err := os.ReadFile(activePath)
	if err != nil {
		t.Fatalf("read active config: %v", err)
	}
	if string(activeContent) != `{"new":true}` {
		t.Fatalf("active config = %s", activeContent)
	}
	backupContent, err := os.ReadFile(result.BackupPath)
	if err != nil {
		t.Fatalf("read backup config: %v", err)
	}
	if string(backupContent) != `{"old":true}` {
		t.Fatalf("backup config = %s", backupContent)
	}
}

func TestApplyWithoutExistingActiveConfig(t *testing.T) {
	dir := t.TempDir()
	stagedPath := filepath.Join(dir, "staged.json")
	activePath := filepath.Join(dir, "active", "config.json")
	backupDir := filepath.Join(dir, "backups")
	if err := os.WriteFile(stagedPath, []byte(`{"new":true}`), 0o600); err != nil {
		t.Fatalf("write staged config: %v", err)
	}

	result, err := NewApplier(activePath, backupDir).Apply(stagedPath, "version-id")
	if err != nil {
		t.Fatalf("apply config: %v", err)
	}
	if result.BackupPath != "" {
		t.Fatalf("backup path should be empty on first apply: %+v", result)
	}
	activeContent, err := os.ReadFile(activePath)
	if err != nil {
		t.Fatalf("read active config: %v", err)
	}
	if string(activeContent) != `{"new":true}` {
		t.Fatalf("active config = %s", activeContent)
	}
}

func TestApplyRejectsMissingStagedConfig(t *testing.T) {
	_, err := NewApplier(filepath.Join(t.TempDir(), "config.json"), t.TempDir()).Apply("/does/not/exist.json", "version-id")
	if err == nil {
		t.Fatal("expected missing staged config to fail")
	}
}
