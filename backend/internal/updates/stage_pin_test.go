package updates

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStageCleanupRefusesPinnedCandidate(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(root, testJobID)
	if err := os.Mkdir(candidate, 0o700); err != nil {
		t.Fatal(err)
	}

	pinner := newStageApplyPinner(root)
	release, err := pinner.Pin(testJobID)
	if err != nil {
		t.Fatal(err)
	}

	stager := &releaseArtifactStager{stagingRoot: root}
	if err := stager.Cleanup(testJobID); !errors.Is(err, ErrStageCandidatePinned) {
		t.Fatalf("cleanup error = %v, want ErrStageCandidatePinned", err)
	}
	if _, err := os.Stat(candidate); err != nil {
		t.Fatalf("pinned candidate was removed: %v", err)
	}

	if err := release(); err != nil {
		t.Fatal(err)
	}
	if err := stager.Cleanup(testJobID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(candidate); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("candidate still exists after pin release and cleanup: %v", err)
	}
}

func TestStageCandidatePinRejectsUnsafePinFile(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	pinRoot := filepath.Join(root, ".apply-pins")
	if err := os.Mkdir(pinRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	pinPath := filepath.Join(pinRoot, testJobID)
	if err := os.WriteFile(pinPath, []byte("unsafe\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(pinPath, 0o666); err != nil {
		t.Fatal(err)
	}
	if _, err := stageCandidatePinned(root, testJobID); err == nil {
		t.Fatal("unsafe pin file was accepted")
	}
}
