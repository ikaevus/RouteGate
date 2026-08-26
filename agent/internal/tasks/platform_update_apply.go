package tasks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
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
// used to detach a VPN update from routegate-agent.service. The caller may
// supply only one canonical task UUID; every privileged selector is rebuilt by
// the root worker from fixed RouteGate-owned policy.
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

// RunPlatformUpdateWorker is intentionally local-only. It reconstructs every
// privileged path from one canonical task UUID, persists monotonic root-owned
// receipt state, starts only the fixed verified updater with --role vpn, and
// remains alive long enough to persist a bounded terminal outcome. INT/TERM are
// forwarded to the updater so the existing transaction rollback traps remain
// authoritative.
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
	targetVersion, err := platformUpdateVersionFromBundle(bundle)
	if err != nil {
		return err
	}

	store := fixedPlatformUpdateReceiptStore()
	if err := rejectOrReconcileExistingPlatformUpdateReceipt(store, taskID); err != nil {
		return err
	}
	if _, err := store.CreatePrepared(taskID, targetVersion); err != nil {
		return fmt.Errorf("create platform update receipt: %w", err)
	}
	if _, err := store.MarkMutationStarted(taskID); err != nil {
		return fmt.Errorf("persist platform update mutation start: %w", err)
	}

	argv := []string{
		"apply",
		"--manifest", filepath.Join(stageDir, "release-manifest.json"),
		"--manifest-attestation", filepath.Join(stageDir, "release-manifest.attestation.json"),
		"--checksums", filepath.Join(stageDir, "SHA256SUMS"),
		"--bundle", bundle,
		"--bundle-attestation", filepath.Join(stageDir, "release-bundles.attestation.json"),
		"--role", "vpn",
	}
	cmd := exec.Command(platformUpdateVerifiedUpdater, argv...)
	cmd.Env = platformUpdateWorkerEnvironment
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		_, receiptErr := store.MarkFailed(taskID, "updater_start_failed")
		if receiptErr != nil {
			return fmt.Errorf("start verified platform updater: %v; persist failure receipt: %w", err, receiptErr)
		}
		return fmt.Errorf("start verified platform updater: %w", err)
	}

	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	for {
		select {
		case sig := <-signals:
			if cmd.Process != nil {
				_ = cmd.Process.Signal(sig)
			}
		case waitErr := <-done:
			if waitErr == nil {
				if _, err := store.MarkSucceeded(taskID); err != nil {
					return fmt.Errorf("persist platform update success receipt: %w", err)
				}
				return nil
			}
			if _, err := store.MarkFailed(taskID, "verified_updater_failed"); err != nil {
				return fmt.Errorf("verified platform updater failed: %v; persist failure receipt: %w", waitErr, err)
			}
			return fmt.Errorf("verified platform updater failed: %w", waitErr)
		}
	}
}

// rejectOrReconcileExistingPlatformUpdateReceipt is called only from a newly
// created task-specific transient worker. systemd refuses a second unit with the
// same fixed name while the original worker is still active, so reaching this
// function with mutation_started means the previous unit is no longer alive and
// its terminal outcome cannot be proven. Ordinary routegate-agent.service startup
// must never call this helper because the detached worker may legitimately still
// be running while the updated Agent becomes healthy.
func rejectOrReconcileExistingPlatformUpdateReceipt(store platformUpdateReceiptStore, taskID string) error {
	receipt, err := store.Read(taskID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("inspect existing platform update receipt: %w", err)
	}
	if receipt.Phase == PlatformUpdateReceiptMutationStarted {
		reconciled, reconcileErr := store.ReconcileInterrupted(taskID)
		if reconcileErr != nil {
			return fmt.Errorf("reconcile orphaned platform update receipt: %w", reconcileErr)
		}
		return fmt.Errorf("platform update task has unknown prior outcome: %s", reconciled.Code)
	}
	return fmt.Errorf("platform update task receipt already exists in terminal or non-runnable phase %s", receipt.Phase)
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
	if filepath.Base(name) != name {
		return false
	}
	_, err := platformUpdateVersionFromBundle(name)
	return err == nil
}

func platformUpdateVersionFromBundle(path string) (string, error) {
	name := filepath.Base(path)
	const prefix = "routegate-"
	if !strings.HasPrefix(name, prefix) {
		return "", fmt.Errorf("platform update bundle name is not canonical")
	}
	for _, arch := range []string{"amd64", "arm64"} {
		suffix := "-linux-" + arch + ".tar.gz"
		if !strings.HasSuffix(name, suffix) {
			continue
		}
		version := strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix)
		if routeGateReleaseVersionPattern.MatchString(version) {
			return version, nil
		}
	}
	return "", fmt.Errorf("platform update bundle name is not canonical")
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
