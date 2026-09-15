package maintenance

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFilesystemProviderDeletesOnlyPreviewedOldCanonicalPartials(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	provider := filesystemProvider{
		category: Category{ID: "manager_update_partials", Scope: "manager", Recommended: true, Selectable: true, RetentionDays: 1},
		root:     root, ownerUID: os.Geteuid(),
	}
	oldName := "11111111-1111-4111-8111-111111111111.partial"
	newName := "22222222-2222-4222-8222-222222222222.partial"
	oldPath := filepath.Join(root, oldName)
	newPath := filepath.Join(root, newName)
	if err := os.Mkdir(oldPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldPath, "release.tar.gz"), []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(newPath, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	symlinkName := "33333333-3333-4333-8333-333333333333.partial"
	if err := os.Symlink(outside, filepath.Join(root, symlinkName)); err != nil {
		t.Fatal(err)
	}
	cutoff := time.Now().Add(-24 * time.Hour)
	oldTime := cutoff.Add(-time.Hour)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	item, err := provider.Analyze(context.Background(), cutoff)
	if err != nil {
		t.Fatal(err)
	}
	if item.CandidateCount != 1 || item.EstimatedBytes != 5 {
		t.Fatalf("analysis=%#v", item)
	}
	encoded, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	var persisted PlanItem
	if err := json.Unmarshal(encoded, &persisted); err != nil {
		t.Fatal(err)
	}
	result, err := provider.Cleanup(context.Background(), persisted)
	if err != nil {
		t.Fatal(err)
	}
	if result.DeletedCount != 1 || result.ReclaimedBytes != 5 {
		t.Fatalf("result=%#v", result)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old candidate still exists: %v", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("new candidate was touched: %v", err)
	}
	if target, err := os.Readlink(filepath.Join(root, symlinkName)); err != nil || target != outside {
		t.Fatalf("symlink candidate was touched: target=%q err=%v", target, err)
	}
}

func TestFilesystemProviderFailsClosedOnUnsafeRootPermissions(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	provider := filesystemProvider{category: Category{ID: "test", Selectable: true}, root: root, ownerUID: os.Geteuid()}
	_, err := provider.Analyze(context.Background(), time.Now())
	if !errors.Is(err, ErrUnsafeOwnership) {
		t.Fatalf("error=%v", err)
	}
}
