package runtimecleanup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ikaevus/routegate/agent/internal/config"
)

func TestAnalyzeCleanupVerifyPreservesNewestBackup(t *testing.T) {
	staging := t.TempDir()
	backups := t.TempDir()
	cleaner := New(config.Config{ConfigStagingDir: staging, ConfigBackupDir: backups})
	cutoff := time.Now().UTC().Add(-30 * 24 * time.Hour)
	old := cutoff.Add(-time.Hour)

	staged := "11111111-1111-4111-8111-111111111111.json"
	oldBackup := "22222222-2222-4222-8222-222222222222.previous.json"
	newestBackup := "33333333-3333-4333-8333-333333333333.previous.json"
	for _, path := range []string{filepath.Join(staging, staged), filepath.Join(backups, oldBackup), filepath.Join(backups, newestBackup)} {
		if err := os.WriteFile(path, []byte("artifact"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(filepath.Join(staging, staged), old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(backups, oldBackup), old.Add(-time.Hour), old.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(backups, newestBackup), old, old); err != nil {
		t.Fatal(err)
	}

	analyzed, err := cleaner.Execute(OperationAnalyze, Request{SchemaVersion: SchemaVersion, Cutoff: cutoff})
	if err != nil {
		t.Fatal(err)
	}
	if analyzed.CandidateCount != 2 {
		t.Fatalf("candidate count=%d, want 2: %#v", analyzed.CandidateCount, analyzed.Candidates)
	}
	cleaned, err := cleaner.Execute(OperationCleanup, Request{SchemaVersion: SchemaVersion, Cutoff: cutoff, Candidates: analyzed.Candidates})
	if err != nil {
		t.Fatal(err)
	}
	verified, err := cleaner.Execute(OperationVerify, Request{SchemaVersion: SchemaVersion, Cutoff: cutoff, Candidates: analyzed.Candidates})
	if err != nil {
		t.Fatal(err)
	}
	if cleaned.DeletedCount != 2 || verified.RemainingCount != 0 || !fileExists(filepath.Join(backups, newestBackup)) {
		t.Fatalf("cleanup=%#v verify=%#v newestExists=%v", cleaned, verified, fileExists(filepath.Join(backups, newestBackup)))
	}
}

func TestCleanupRejectsNonCanonicalCandidate(t *testing.T) {
	staging := t.TempDir()
	cleaner := New(config.Config{ConfigStagingDir: staging})
	cutoff := time.Now().UTC().Add(-30 * 24 * time.Hour)
	request := Request{SchemaVersion: SchemaVersion, Cutoff: cutoff, Candidates: []Candidate{{RootID: "sing_box_staging", Name: "../../etc/passwd"}}}
	if _, err := cleaner.Execute(OperationCleanup, request); err == nil {
		t.Fatal("non-canonical candidate was accepted")
	}
}

func TestDecodeRequestRejectsUnknownFields(t *testing.T) {
	payload := json.RawMessage(`{"schemaVersion":1,"cutoff":"2026-01-01T00:00:00Z","path":"/tmp"}`)
	if _, err := DecodeRequest(payload, OperationCleanup); err == nil {
		t.Fatal("arbitrary path field was accepted")
	}
}

func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
