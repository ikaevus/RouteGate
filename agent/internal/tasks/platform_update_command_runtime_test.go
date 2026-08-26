package tasks

import (
	"os"
	"path/filepath"
	"testing"
)

func commandRuntimeFixtureRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp(".", ".platform-update-command-runtime-")
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

func writeExecutable(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), mode); err != nil {
		t.Fatal(err)
	}
}

func TestValidatePlatformUpdateCommandRuntimeAcceptsTrustedDirectExecutable(t *testing.T) {
	root := commandRuntimeFixtureRoot(t)
	uid := uint32(os.Geteuid())
	writeExecutable(t, filepath.Join(root, "python3"), 0755)

	if err := validatePlatformUpdateCommandRuntime([]string{root}, []string{"python3"}, uid); err != nil {
		t.Fatalf("trusted command runtime rejected: %v", err)
	}
}

func TestValidatePlatformUpdateCommandRuntimeRejectsMissingWritableAndNonExecutableCommands(t *testing.T) {
	uid := uint32(os.Geteuid())

	t.Run("missing", func(t *testing.T) {
		root := commandRuntimeFixtureRoot(t)
		if err := validatePlatformUpdateCommandRuntime([]string{root}, []string{"python3"}, uid); err == nil {
			t.Fatal("missing updater dependency was accepted")
		}
	})

	t.Run("group writable", func(t *testing.T) {
		root := commandRuntimeFixtureRoot(t)
		path := filepath.Join(root, "python3")
		writeExecutable(t, path, 0755)
		if err := os.Chmod(path, 0775); err != nil {
			t.Fatal(err)
		}
		if err := validatePlatformUpdateCommandRuntime([]string{root}, []string{"python3"}, uid); err == nil {
			t.Fatal("group-writable updater dependency was accepted")
		}
	})

	t.Run("non executable", func(t *testing.T) {
		root := commandRuntimeFixtureRoot(t)
		writeExecutable(t, filepath.Join(root, "python3"), 0644)
		if err := validatePlatformUpdateCommandRuntime([]string{root}, []string{"python3"}, uid); err == nil {
			t.Fatal("non-executable updater dependency was accepted")
		}
	})
}

func TestValidatePlatformUpdateCommandRuntimeAcceptsTrustedSymlinkChain(t *testing.T) {
	root := commandRuntimeFixtureRoot(t)
	uid := uint32(os.Geteuid())
	binDir := filepath.Join(root, "bin")
	targetDir := filepath.Join(root, "targets")
	if err := os.Mkdir(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(targetDir, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(targetDir, "python3.12")
	writeExecutable(t, target, 0755)
	if err := os.Symlink(filepath.Join("..", "targets", "python3.12"), filepath.Join(binDir, "python3")); err != nil {
		t.Fatal(err)
	}

	if err := validatePlatformUpdateCommandRuntime([]string{binDir}, []string{"python3"}, uid); err != nil {
		t.Fatalf("trusted alternatives-style symlink was rejected: %v", err)
	}
}

func TestValidatePlatformUpdateCommandRuntimeRejectsSymlinkThroughWritableParent(t *testing.T) {
	root := commandRuntimeFixtureRoot(t)
	uid := uint32(os.Geteuid())
	binDir := filepath.Join(root, "bin")
	writableDir := filepath.Join(root, "writable")
	if err := os.Mkdir(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(writableDir, 0755); err != nil {
		t.Fatal(err)
	}
	// os.Mkdir respects the process umask. Set the unsafe mode explicitly so
	// this regression proves the validator rejects a writable resolved parent.
	if err := os.Chmod(writableDir, 0775); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(writableDir, "python3")
	writeExecutable(t, target, 0755)
	if err := os.Symlink(filepath.Join("..", "writable", "python3"), filepath.Join(binDir, "python3")); err != nil {
		t.Fatal(err)
	}

	if err := validatePlatformUpdateCommandRuntime([]string{binDir}, []string{"python3"}, uid); err == nil {
		t.Fatal("updater dependency through group-writable parent was accepted")
	}
}

func TestValidatePlatformUpdateCommandRuntimeRejectsSymlinkLoop(t *testing.T) {
	root := commandRuntimeFixtureRoot(t)
	uid := uint32(os.Geteuid())
	if err := os.Symlink("python3-next", filepath.Join(root, "python3")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("python3", filepath.Join(root, "python3-next")); err != nil {
		t.Fatal(err)
	}

	if err := validatePlatformUpdateCommandRuntime([]string{root}, []string{"python3"}, uid); err == nil {
		t.Fatal("cyclic updater dependency symlink was accepted")
	}
}
