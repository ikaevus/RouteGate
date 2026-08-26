package tasks

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

const (
	platformUpdateSystemctlPath = "/usr/bin/systemctl"

	platformUpdateToolchainDir = "/usr/local/lib/routegate/update"
	platformUpdateEntrypoint   = "/usr/local/sbin/routegate-update"

	platformUpdateVerifierVersion  = "2.97.0"
	platformUpdateVerifierDir      = "/usr/local/lib/routegate/verifier"
	platformUpdateVerifierBinary   = platformUpdateVerifierDir + "/gh"
	platformUpdateVerifierMetadata = platformUpdateVerifierDir + "/runtime.env"

	platformUpdateVerifierArchiveSHAAMD64 = "a2c9b8497e1f85b1ad0dfcb78b5a622e098801b8e461e459e88e1ee12f018112"
	platformUpdateVerifierArchiveSHAARM64 = "73ea440ecad9c9e284429997ee6f93577bc6f7bc6fba357ef62c53ad8fb641a5"

	platformUpdateVerifierProbeTimeout  = 5 * time.Second
	platformUpdateVerifierProbeMaxBytes = 128 * 1024
)

var fixedPlatformUpdateReadinessExecutables = []string{
	platformUpdateSystemdRun,
	platformUpdateSystemctlPath,
	platformUpdateAgentBinary,
	platformUpdateVerifiedUpdater,
}

type platformUpdateTrustedFile struct {
	name       string
	executable bool
}

var platformUpdateTrustedToolchainFiles = []platformUpdateTrustedFile{
	{name: "release_manifest.py", executable: true},
	{name: "routegate-update-core.sh"},
	{name: "routegate-update-role.sh"},
	{name: "routegate-update-transaction.sh", executable: true},
	{name: "routegate-update-verified.sh", executable: true},
	{name: "routegate-update-dispatch.py", executable: true},
}

type platformUpdateRuntimePolicy struct {
	toolchainDir       string
	entrypoint         string
	verifierDir        string
	verifierBinary     string
	verifierMetadata   string
	verifierVersion    string
	verifierArchiveSHA string
	verifierSourceURL  string
}

func PlatformUpdateRuntimeReady() bool {
	policy, ok := fixedPlatformUpdateRuntimePolicy(runtime.GOARCH)
	if !ok {
		return false
	}
	return platformUpdateRuntimeReady(os.Geteuid(), runtime.GOARCH, 0, fixedPlatformUpdateReadinessExecutables, policy)
}

func fixedPlatformUpdateRuntimePolicy(arch string) (platformUpdateRuntimePolicy, bool) {
	archiveSHA := ""
	switch arch {
	case "amd64":
		archiveSHA = platformUpdateVerifierArchiveSHAAMD64
	case "arm64":
		archiveSHA = platformUpdateVerifierArchiveSHAARM64
	default:
		return platformUpdateRuntimePolicy{}, false
	}
	return platformUpdateRuntimePolicy{
		toolchainDir:       platformUpdateToolchainDir,
		entrypoint:         platformUpdateEntrypoint,
		verifierDir:        platformUpdateVerifierDir,
		verifierBinary:     platformUpdateVerifierBinary,
		verifierMetadata:   platformUpdateVerifierMetadata,
		verifierVersion:    platformUpdateVerifierVersion,
		verifierArchiveSHA: archiveSHA,
		verifierSourceURL: fmt.Sprintf(
			"https://github.com/cli/cli/releases/download/v%s/gh_%s_linux_%s.tar.gz",
			platformUpdateVerifierVersion,
			platformUpdateVerifierVersion,
			arch,
		),
	}, true
}

func platformUpdateRuntimeReady(euid int, arch string, expectedUID uint32, executables []string, policy platformUpdateRuntimePolicy) bool {
	if euid != 0 || (arch != "amd64" && arch != "arm64") || len(executables) != 4 {
		return false
	}
	for _, path := range executables {
		if err := validatePlatformUpdateReadinessExecutable(path, expectedUID); err != nil {
			return false
		}
	}
	if err := validatePlatformUpdateToolchainRuntime(policy, expectedUID); err != nil {
		return false
	}
	return validatePlatformUpdateVerifierRuntime(policy, arch, expectedUID) == nil
}

func validatePlatformUpdateToolchainRuntime(policy platformUpdateRuntimePolicy, expectedUID uint32) error {
	if err := validatePlatformUpdateTrustedDirectory(policy.toolchainDir, expectedUID); err != nil {
		return fmt.Errorf("trusted updater directory: %w", err)
	}
	entries, err := os.ReadDir(policy.toolchainDir)
	if err != nil {
		return err
	}
	if len(entries) != len(platformUpdateTrustedToolchainFiles) {
		return fmt.Errorf("trusted updater directory has unexpected entry count")
	}
	expected := make(map[string]platformUpdateTrustedFile, len(platformUpdateTrustedToolchainFiles))
	for _, requirement := range platformUpdateTrustedToolchainFiles {
		expected[requirement.name] = requirement
		path := filepath.Join(policy.toolchainDir, requirement.name)
		if err := validatePlatformUpdateReadinessFile(path, expectedUID, requirement.executable); err != nil {
			return fmt.Errorf("trusted updater component %s: %w", requirement.name, err)
		}
	}
	for _, entry := range entries {
		if _, ok := expected[entry.Name()]; !ok {
			return fmt.Errorf("trusted updater directory contains unexpected entry %s", entry.Name())
		}
	}
	if err := validatePlatformUpdateReadinessFile(policy.entrypoint, expectedUID, true); err != nil {
		return fmt.Errorf("trusted updater entrypoint: %w", err)
	}
	return nil
}

func validatePlatformUpdateVerifierRuntime(policy platformUpdateRuntimePolicy, arch string, expectedUID uint32) error {
	if err := validatePlatformUpdateTrustedDirectory(policy.verifierDir, expectedUID); err != nil {
		return fmt.Errorf("verifier directory: %w", err)
	}
	entries, err := os.ReadDir(policy.verifierDir)
	if err != nil {
		return err
	}
	if len(entries) != 2 {
		return fmt.Errorf("verifier directory has unexpected entry count")
	}
	for _, entry := range entries {
		if entry.Name() != filepath.Base(policy.verifierBinary) && entry.Name() != filepath.Base(policy.verifierMetadata) {
			return fmt.Errorf("verifier directory contains unexpected entry %s", entry.Name())
		}
	}
	if err := validatePlatformUpdateReadinessFile(policy.verifierBinary, expectedUID, true); err != nil {
		return fmt.Errorf("pinned verifier binary: %w", err)
	}
	if err := validatePlatformUpdateReadinessFile(policy.verifierMetadata, expectedUID, false); err != nil {
		return fmt.Errorf("pinned verifier metadata: %w", err)
	}

	metadataBytes, err := os.ReadFile(policy.verifierMetadata)
	if err != nil {
		return err
	}
	metadata, err := parsePlatformUpdateVerifierMetadata(string(metadataBytes))
	if err != nil {
		return err
	}
	if metadata["FORMAT_VERSION"] != "1" ||
		metadata["VERSION"] != policy.verifierVersion ||
		metadata["ARCH"] != arch ||
		metadata["ARCHIVE_SHA256"] != policy.verifierArchiveSHA ||
		metadata["SOURCE_URL"] != policy.verifierSourceURL {
		return fmt.Errorf("pinned verifier metadata does not match fixed policy")
	}
	binarySHA := metadata["BINARY_SHA256"]
	if !isLowerHex(binarySHA, 64) {
		return fmt.Errorf("pinned verifier binary digest is invalid")
	}
	actualSHA, err := sha256File(policy.verifierBinary)
	if err != nil {
		return err
	}
	if actualSHA != binarySHA {
		return fmt.Errorf("pinned verifier binary digest mismatch")
	}
	return probePlatformUpdateVerifier(policy.verifierBinary, policy.verifierVersion)
}

func parsePlatformUpdateVerifierMetadata(raw string) (map[string]string, error) {
	allowed := map[string]struct{}{
		"FORMAT_VERSION": {},
		"VERSION":        {},
		"ARCH":           {},
		"ARCHIVE_SHA256": {},
		"BINARY_SHA256":  {},
		"SOURCE_URL":     {},
	}
	values := make(map[string]string, len(allowed))
	for _, line := range strings.Split(raw, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid verifier metadata line")
		}
		if _, ok := allowed[parts[0]]; !ok || parts[1] == "" {
			return nil, fmt.Errorf("unsupported or empty verifier metadata field")
		}
		if _, duplicate := values[parts[0]]; duplicate {
			return nil, fmt.Errorf("duplicate verifier metadata field")
		}
		values[parts[0]] = parts[1]
	}
	if len(values) != len(allowed) {
		return nil, fmt.Errorf("verifier metadata is incomplete")
	}
	return values, nil
}

func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func probePlatformUpdateVerifier(path, version string) error {
	ctx, cancel := context.WithTimeout(context.Background(), platformUpdateVerifierProbeTimeout)
	defer cancel()
	versionOutput, err := fixedPlatformUpdateVerifierOutput(ctx, path, "--version")
	if err != nil {
		return fmt.Errorf("read pinned verifier version: %w", err)
	}
	firstLine := strings.SplitN(strings.TrimSpace(versionOutput), "\n", 2)[0]
	if !strings.HasPrefix(firstLine, "gh version "+version+" ") {
		return fmt.Errorf("pinned verifier version mismatch")
	}
	helpOutput, err := fixedPlatformUpdateVerifierOutput(ctx, path, "attestation", "verify", "--help")
	if err != nil {
		return fmt.Errorf("read pinned verifier policy capability: %w", err)
	}
	if !strings.Contains(helpOutput, "--predicate-type") {
		return fmt.Errorf("pinned verifier lacks predicate policy capability")
	}
	return nil
}

func fixedPlatformUpdateVerifierOutput(ctx context.Context, path string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = platformUpdateWorkerEnvironment
	output, err := cmd.Output()
	if len(output) > platformUpdateVerifierProbeMaxBytes {
		return "", fmt.Errorf("pinned verifier output exceeded bounded size")
	}
	if err != nil {
		return "", err
	}
	return string(output), nil
}

func isLowerHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

// validatePlatformUpdateReadinessExecutable validates both the executable and
// every directory used to reach it. A writable or symlinked parent would let a
// different principal replace a previously checked binary by rename.
func validatePlatformUpdateReadinessExecutable(path string, expectedUID uint32) error {
	return validatePlatformUpdateReadinessFile(path, expectedUID, true)
}

func validatePlatformUpdateReadinessFile(path string, expectedUID uint32, executable bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file")
	}
	if err := validatePlatformUpdateOwnedNonWritable(info, expectedUID); err != nil {
		return err
	}
	if executable && info.Mode().Perm()&0111 == 0 {
		return fmt.Errorf("not executable")
	}
	return validatePlatformUpdateParentChain(path, expectedUID)
}

func validatePlatformUpdateTrustedDirectory(path string, expectedUID uint32) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("not a trusted directory")
	}
	if err := validatePlatformUpdateOwnedNonWritable(info, expectedUID); err != nil {
		return err
	}
	return validatePlatformUpdateParentChain(filepath.Join(path, ".runtime-check"), expectedUID)
}

func validatePlatformUpdateParentChain(path string, expectedUID uint32) error {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		return fmt.Errorf("path is not absolute")
	}
	for dir := filepath.Dir(clean); ; dir = filepath.Dir(dir) {
		info, err := os.Lstat(dir)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("unsafe parent directory %s", dir)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("unsafe parent directory %s: filesystem ownership metadata unavailable", dir)
		}
		// Production passes expectedUID=0. Allowing root ancestors as well as the
		// fixture owner keeps this helper testable as an unprivileged user without
		// weakening the production root-only policy.
		if stat.Uid != 0 && stat.Uid != expectedUID {
			return fmt.Errorf("unsafe parent directory %s: unexpected owner", dir)
		}
		if info.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("unsafe parent directory %s: group/world writable", dir)
		}
		if dir == string(filepath.Separator) {
			break
		}
	}
	return nil
}

func validatePlatformUpdateOwnedNonWritable(info os.FileInfo, expectedUID uint32) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("filesystem ownership metadata unavailable")
	}
	if stat.Uid != expectedUID {
		return fmt.Errorf("unexpected owner")
	}
	if info.Mode().Perm()&0022 != 0 {
		return fmt.Errorf("group/world writable")
	}
	return nil
}
