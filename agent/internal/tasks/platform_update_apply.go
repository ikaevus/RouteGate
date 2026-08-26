package tasks

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	platformUpdateVerifiedUpdater = "/usr/local/lib/routegate/update/routegate-update-verified.sh"
	platformUpdateAgentBinary     = "/usr/local/bin/routegate-agent"
	platformUpdateSystemdRun      = "/usr/bin/systemd-run"
)

var platformUpdateWorkerEnvironment = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin"}

// DetachedPlatformUpdateCommand returns the fixed transient-systemd invocation
// used by E2d to detach a VPN update from routegate-agent.service. E2c exposes
// the primitive but does not wire it to remote tasks yet.
func DetachedPlatformUpdateCommand(taskID string) (string, []string, error) {
	if !canonicalTaskIDPattern.MatchString(taskID) {
		return "", nil, fmt.Errorf("platform update task id must be canonical UUIDv4")
	}
	unit := "routegate-vpn-update-" + taskID
	return platformUpdateSystemdRun, []string{
		platformUpdateSystemdRun,
		"--unit=" + unit,
		"--collect",
		"--no-block",
		"--property=UMask=0077",
		"--property=NoNewPrivileges=yes",
		platformUpdateAgentBinary,
		"--platform-update-worker-task=" + taskID,
	}, nil
}

func StartDetachedPlatformUpdate(ctx context.Context, taskID string) error {
	path, argv, err := DetachedPlatformUpdateCommand(taskID)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, path, argv[1:]...)
	cmd.Env = platformUpdateWorkerEnvironment
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("start detached platform update: %w: %s", err, boundedPlatformUpdateOutput(output))
	}
	return nil
}

// RunPlatformUpdateWorker is intentionally local-only. The worker reconstructs
// every privileged path from one canonical task UUID and replaces itself with
// the fixed trusted updater so transaction signals reach the rollback trap.
func RunPlatformUpdateWorker(taskID string) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("platform update worker must run as root")
	}
	stageDir, bundle, err := validatedPlatformUpdateStage(taskID)
	if err != nil {
		return err
	}
	if err := validateRootOwnedNonWritableRegular(platformUpdateVerifiedUpdater); err != nil {
		return fmt.Errorf("trusted verified updater is unsafe: %w", err)
	}
	argv := []string{
		platformUpdateVerifiedUpdater,
		"apply",
		"--manifest", filepath.Join(stageDir, "release-manifest.json"),
		"--manifest-attestation", filepath.Join(stageDir, "release-manifest.attestation.json"),
		"--checksums", filepath.Join(stageDir, "SHA256SUMS"),
		"--bundle", bundle,
		"--bundle-attestation", filepath.Join(stageDir, "release-bundles.attestation.json"),
		"--role", "vpn",
	}
	return syscall.Exec(platformUpdateVerifiedUpdater, argv, platformUpdateWorkerEnvironment)
}

func validatedPlatformUpdateStage(taskID string) (string, string, error) {
	if !canonicalTaskIDPattern.MatchString(taskID) {
		return "", "", fmt.Errorf("platform update task id must be canonical UUIDv4")
	}
	if err := validateRootOwnedNonWritableDir(platformUpdateStagingRoot); err != nil {
		return "", "", fmt.Errorf("platform update staging root is unsafe: %w", err)
	}
	stageDir := filepath.Join(platformUpdateStagingRoot, taskID)
	if err := validateRootOwnedNonWritableDir(stageDir); err != nil {
		return "", "", fmt.Errorf("platform update staged candidate is unsafe: %w", err)
	}

	required := map[string]struct{}{
		"release-manifest.json":             {},
		"release-manifest.attestation.json": {},
		"SHA256SUMS":                        {},
		"release-bundles.attestation.json":  {},
	}
	entries, err := os.ReadDir(stageDir)
	if err != nil {
		return "", "", fmt.Errorf("read platform update staged candidate: %w", err)
	}
	if len(entries) != 5 {
		return "", "", fmt.Errorf("platform update staged candidate must contain exactly five files")
	}

	bundle := ""
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(stageDir, name)
		if _, ok := required[name]; ok {
			if err := validateRootOwnedNonWritableRegular(path); err != nil {
				return "", "", fmt.Errorf("unsafe staged platform update file %s: %w", name, err)
			}
			continue
		}
		if bundle != "" || !isCanonicalPlatformUpdateBundleName(name) {
			return "", "", fmt.Errorf("platform update staged candidate contains unexpected or duplicate bundle entry")
		}
		if err := validateRootOwnedNonWritableRegular(path); err != nil {
			return "", "", fmt.Errorf("unsafe staged platform update bundle: %w", err)
		}
		bundle = path
	}
	if bundle == "" {
		return "", "", fmt.Errorf("platform update staged candidate bundle is missing")
	}
	return stageDir, bundle, nil
}

func isCanonicalPlatformUpdateBundleName(name string) bool {
	const prefix = "routegate-"
	for _, arch := range []string{"amd64", "arm64"} {
		suffix := "-linux-" + arch + ".tar.gz"
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
			continue
		}
		version := strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix)
		return routeGateReleaseVersionPattern.MatchString(version)
	}
	return false
}

func validateRootOwnedNonWritableDir(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("not a regular directory")
	}
	return validateRootOwnedNonWritable(info)
}

func validateRootOwnedNonWritableRegular(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file")
	}
	return validateRootOwnedNonWritable(info)
}

func validateRootOwnedNonWritable(info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("filesystem ownership metadata unavailable")
	}
	if stat.Uid != 0 {
		return fmt.Errorf("not root-owned")
	}
	if info.Mode().Perm()&0022 != 0 {
		return fmt.Errorf("group/world writable")
	}
	return nil
}

func boundedPlatformUpdateOutput(output []byte) string {
	const limit = 512
	if len(output) > limit {
		output = output[:limit]
	}
	return string(output)
}
