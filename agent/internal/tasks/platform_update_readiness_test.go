package tasks

import (
	"crypto/sha256"
	"fmt"
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

func completePlatformUpdateRuntimeFixture(t *testing.T, arch string) ([]string, platformUpdateRuntimePolicy) {
	t.Helper()
	root := readinessFixtureRoot(t)
	uid := uint32(os.Geteuid())
	policy, ok := fixedPlatformUpdateRuntimePolicy(arch)
	if !ok {
		t.Fatalf("unsupported fixture architecture %q", arch)
	}

	policy.toolchainDir = filepath.Join(root, "update")
	policy.entrypoint = filepath.Join(root, "routegate-update")
	policy.verifierDir = filepath.Join(root, "verifier")
	policy.verifierBinary = filepath.Join(policy.verifierDir, "gh")
	policy.verifierMetadata = filepath.Join(policy.verifierDir, "runtime.env")

	if err := os.Mkdir(policy.toolchainDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, requirement := range platformUpdateTrustedToolchainFiles {
		mode := os.FileMode(0644)
		if requirement.executable {
			mode = 0755
		}
		if err := os.WriteFile(filepath.Join(policy.toolchainDir, requirement.name), []byte("fixture\n"), mode); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(policy.entrypoint, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.Mkdir(policy.verifierDir, 0755); err != nil {
		t.Fatal(err)
	}
	verifier := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "--version" ]; then
  echo "gh version %s (fixture)"
  exit 0
fi
if [ "$1" = "attestation" ] && [ "$2" = "verify" ] && [ "$3" = "--help" ]; then
  echo "      --predicate-type string"
  exit 0
fi
exit 1
`, policy.verifierVersion)
	if err := os.WriteFile(policy.verifierBinary, []byte(verifier), 0755); err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(policy.verifierBinary)
	if err != nil {
		t.Fatal(err)
	}
	binarySHA := fmt.Sprintf("%x", sha256.Sum256(binary))
	metadata := fmt.Sprintf(
		"FORMAT_VERSION=1\nVERSION=%s\nARCH=%s\nARCHIVE_SHA256=%s\nBINARY_SHA256=%s\nSOURCE_URL=%s\n",
		policy.verifierVersion,
		arch,
		policy.verifierArchiveSHA,
		binarySHA,
		policy.verifierSourceURL,
	)
	if err := os.WriteFile(policy.verifierMetadata, []byte(metadata), 0644); err != nil {
		t.Fatal(err)
	}

	executables := []string{
		filepath.Join(root, "systemd-run"),
		filepath.Join(root, "systemctl"),
		filepath.Join(root, "routegate-agent"),
		filepath.Join(policy.toolchainDir, "routegate-update-verified.sh"),
	}
	for _, path := range executables[:3] {
		if err := os.WriteFile(path, []byte("fixture\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if !platformUpdateRuntimeReady(0, arch, uid, executables, policy) {
		t.Fatal("complete safe runtime fixture was not ready")
	}
	return executables, policy
}

func TestPlatformUpdateRuntimeReadyRequiresCompleteTrustedRuntime(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		t.Run(arch, func(t *testing.T) {
			completePlatformUpdateRuntimeFixture(t, arch)
		})
	}
}

func TestPlatformUpdateRuntimeReadyFailsClosed(t *testing.T) {
	executables, policy := completePlatformUpdateRuntimeFixture(t, "amd64")
	uid := uint32(os.Geteuid())

	if platformUpdateRuntimeReady(1000, "amd64", uid, executables, policy) {
		t.Fatal("non-root Agent was marked update-ready")
	}
	if platformUpdateRuntimeReady(0, "386", uid, executables, policy) {
		t.Fatal("unsupported architecture was marked update-ready")
	}
	if platformUpdateRuntimeReady(0, "amd64", uid, executables[:3], policy) {
		t.Fatal("incomplete fixed executable set was marked update-ready")
	}

	if err := os.Chmod(executables[0], 0775); err != nil {
		t.Fatal(err)
	}
	if platformUpdateRuntimeReady(0, "amd64", uid, executables, policy) {
		t.Fatal("group-writable executable was marked update-ready")
	}
	if err := os.Chmod(executables[0], 0644); err != nil {
		t.Fatal(err)
	}
	if platformUpdateRuntimeReady(0, "amd64", uid, executables, policy) {
		t.Fatal("non-executable readiness path was accepted")
	}
}

func TestPlatformUpdateRuntimeReadyRejectsPartialBootstrapWithoutVerifier(t *testing.T) {
	executables, policy := completePlatformUpdateRuntimeFixture(t, "amd64")
	if err := os.RemoveAll(policy.verifierDir); err != nil {
		t.Fatal(err)
	}
	if platformUpdateRuntimeReady(0, "amd64", uint32(os.Geteuid()), executables, policy) {
		t.Fatal("runtime without pinned attestation verifier was marked ready")
	}
}

func TestPlatformUpdateRuntimeReadyRejectsIncompleteToolchain(t *testing.T) {
	executables, policy := completePlatformUpdateRuntimeFixture(t, "amd64")
	if err := os.Remove(filepath.Join(policy.toolchainDir, "routegate-update-transaction.sh")); err != nil {
		t.Fatal(err)
	}
	if platformUpdateRuntimeReady(0, "amd64", uint32(os.Geteuid()), executables, policy) {
		t.Fatal("runtime with incomplete trusted updater toolchain was marked ready")
	}
}

func TestPlatformUpdateRuntimeReadyRejectsTamperedVerifierMetadata(t *testing.T) {
	executables, policy := completePlatformUpdateRuntimeFixture(t, "amd64")
	metadata, err := os.ReadFile(policy.verifierMetadata)
	if err != nil {
		t.Fatal(err)
	}
	metadata = []byte(string(metadata) + "UNEXPECTED=value\n")
	if err := os.WriteFile(policy.verifierMetadata, metadata, 0644); err != nil {
		t.Fatal(err)
	}
	if platformUpdateRuntimeReady(0, "amd64", uint32(os.Geteuid()), executables, policy) {
		t.Fatal("runtime with tampered verifier metadata was marked ready")
	}
}

func TestPlatformUpdateRuntimeReadyRejectsVerifierDigestMismatch(t *testing.T) {
	executables, policy := completePlatformUpdateRuntimeFixture(t, "amd64")
	if err := os.WriteFile(policy.verifierBinary, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if platformUpdateRuntimeReady(0, "amd64", uint32(os.Geteuid()), executables, policy) {
		t.Fatal("runtime with verifier binary digest mismatch was marked ready")
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
