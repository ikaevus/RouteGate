package tasks

import (
	"os"
	"path/filepath"
	"testing"
)

func readinessFixtureRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp(".", ".platform-update-readiness-")
	if err != nil {
		t.Fatal(err)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(abs) })
	return abs
}

func TestPlatformUpdateRuntimeReadyRequiresFixedSafeExecutables(t *testing.T) {
	root := readinessFixtureRoot(t)
	uid := uint32(os.Geteuid())
	paths := make([]string, 0, 4)
	for _, name := range []string{"systemd-run", "systemctl", "routegate-agent", "routegate-update-verified.sh"} {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte("fixture\n"), 0755); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	if !platformUpdateRuntimeReady(0, "amd64", uid, paths) {
		t.Fatal("safe fixed readiness fixture was not ready")
	}
	if !platformUpdateRuntimeReady(0, "arm64", uid, paths) {
		t.Fatal("arm64 readiness fixture was not ready")
	}
}

func TestPlatformUpdateRuntimeReadyFailsClosed(t *testing.T) {
	root := readinessFixtureRoot(t)
	uid := uint32(os.Geteuid())
	makePaths := func() []string {
		t.Helper()
		paths := make([]string, 0, 4)
		for _, name := range []string{"systemd-run", "systemctl", "routegate-agent", "routegate-update-verified.sh"} {
			path := filepath.Join(root, name)
			if err := os.WriteFile(path, []byte("fixture\n"), 0755); err != nil {
				t.Fatal(err)
			}
			paths = append(paths, path)
		}
		return paths
	}

	paths := makePaths()
	if platformUpdateRuntimeReady(1000, "amd64", uid, paths) {
		t.Fatal("non-root Agent was marked update-ready")
	}
	if platformUpdateRuntimeReady(0, "386", uid, paths) {
		t.Fatal("unsupported architecture was marked update-ready")
	}
	if platformUpdateRuntimeReady(0, "amd64", uid, paths[:3]) {
		t.Fatal("incomplete fixed executable set was marked update-ready")
	}

	if err := os.Chmod(paths[0], 0775); err != nil {
		t.Fatal(err)
	}
	if platformUpdateRuntimeReady(0, "amd64", uid, paths) {
		t.Fatal("group-writable executable was marked update-ready")
	}
	if err := os.Chmod(paths[0], 0644); err != nil {
		t.Fatal(err)
	}
	if platformUpdateRuntimeReady(0, "amd64", uid, paths) {
		t.Fatal("non-executable readiness path was accepted")
	}
}

func TestPlatformUpdateReadinessRejectsSymlink(t *testing.T) {
	root := readinessFixtureRoot(t)
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("fixture\n"), 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := validatePlatformUpdateReadinessExecutable(link, uint32(os.Geteuid())); err == nil {
		t.Fatal("symlink readiness executable was accepted")
	}
}

func TestPlatformUpdateReadinessRejectsWritableParent(t *testing.T) {
	root := readinessFixtureRoot(t)
	unsafe := filepath.Join(root, "unsafe")
	if err := os.Mkdir(unsafe, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(unsafe, "routegate-agent")
	if err := os.WriteFile(path, []byte("fixture\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unsafe, 0775); err != nil {
		t.Fatal(err)
	}
	if err := validatePlatformUpdateReadinessExecutable(path, uint32(os.Geteuid())); err == nil {
		t.Fatal("executable below group-writable parent was accepted")
	}
}
