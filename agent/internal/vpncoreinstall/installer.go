package vpncoreinstall

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	TaskKind            = "vpn_core_install"
	OperationInstall    = "install_sing_box"
	DefaultServiceName  = "sing-box.service"
	defaultOSRelease    = "/etc/os-release"
	defaultAPTGet       = "/usr/bin/apt-get"
	defaultSystemctl    = "/usr/bin/systemctl"
	defaultSystemdRun   = "/run/systemd/system"
	defaultSingBoxPath  = "/usr/bin/sing-box"
	defaultKeyPath      = "/etc/apt/keyrings/sagernet.asc"
	defaultSourcePath   = "/etc/apt/sources.list.d/sagernet.sources"
	signingKeyURL       = "https://sing-box.app/gpg.key"
	maxSigningKeyBytes  = 1 << 20
	commandTimeout      = 10 * time.Minute
	verificationTimeout = 15 * time.Second
)

const repositorySource = `Types: deb
URIs: https://deb.sagernet.org/
Suites: *
Components: *
Enabled: yes
Signed-By: /etc/apt/keyrings/sagernet.asc
`

var orderedStages = []string{
	"detect_platform",
	"check_existing_installation",
	"configure_repository",
	"refresh_package_index",
	"install_package",
	"verify_binary",
	"verify_service",
	"complete",
}

type Platform struct {
	ID           string `json:"id"`
	Version      string `json:"version"`
	Architecture string `json:"architecture"`
}

type StageResult struct {
	Stage  string `json:"stage"`
	Status string `json:"status"`
	Code   string `json:"code,omitempty"`
}

type Report struct {
	Kind           string        `json:"kind"`
	Operation      string        `json:"operation"`
	Status         string        `json:"status"`
	Platform       Platform      `json:"platform"`
	SingBoxVersion string        `json:"singBoxVersion,omitempty"`
	BinaryPath     string        `json:"binaryPath,omitempty"`
	ServiceName    string        `json:"serviceName"`
	Stages         []StageResult `json:"stages"`
}

type InstallError struct {
	Stage string
	Code  string
}

func (e *InstallError) Error() string {
	return e.Code
}

type commandRunner func(context.Context, string, ...string) ([]byte, error)
type downloader func(context.Context, string) ([]byte, error)

type Installer struct {
	osReleasePath     string
	architecture      string
	aptGetPath        string
	systemctlPath     string
	systemdRunPath    string
	packageBinaryPath string
	keyPath           string
	sourcePath        string
	serviceName       string
	run               commandRunner
	download          downloader
}

func New() Installer {
	return Installer{
		osReleasePath:     defaultOSRelease,
		architecture:      runtime.GOARCH,
		aptGetPath:        defaultAPTGet,
		systemctlPath:     defaultSystemctl,
		systemdRunPath:    defaultSystemdRun,
		packageBinaryPath: defaultSingBoxPath,
		keyPath:           defaultKeyPath,
		sourcePath:        defaultSourcePath,
		serviceName:       DefaultServiceName,
		run:               runCommand,
		download:          downloadSigningKey,
	}
}

func (i Installer) Execute(ctx context.Context, operation string) (Report, error) {
	report := Report{
		Kind:        TaskKind,
		Operation:   strings.TrimSpace(operation),
		Status:      "failed",
		ServiceName: i.serviceName,
		Stages:      make([]StageResult, 0, 8),
	}
	if report.Operation != OperationInstall {
		return i.fail(report, "detect_platform", "unsupported_installation_operation")
	}

	platform, err := DetectPlatform(i.osReleasePath, i.architecture)
	report.Platform = platform
	if err != nil {
		return i.fail(report, "detect_platform", errorCode(err, "platform_detection_failed"))
	}
	if !platform.Supported() || !regularExecutable(i.aptGetPath) ||
		!regularExecutable(i.systemctlPath) || !directoryExists(i.systemdRunPath) {
		return i.fail(report, "detect_platform", "unsupported_platform")
	}
	report.Stages = append(report.Stages, StageResult{Stage: "detect_platform", Status: "succeeded"})

	binaryPath := i.packageBinaryPath
	alreadyInstalled := regularExecutable(binaryPath)
	checkCode := "not_installed"
	if alreadyInstalled {
		checkCode = "already_installed"
	}
	report.Stages = append(report.Stages, StageResult{Stage: "check_existing_installation", Status: "succeeded", Code: checkCode})

	if alreadyInstalled {
		report.Stages = append(report.Stages,
			StageResult{Stage: "configure_repository", Status: "skipped", Code: "already_installed"},
			StageResult{Stage: "refresh_package_index", Status: "skipped", Code: "already_installed"},
			StageResult{Stage: "install_package", Status: "skipped", Code: "already_installed"},
		)
	} else {
		if err := i.configureRepository(ctx); err != nil {
			return i.fail(report, "configure_repository", "repository_configuration_failed")
		}
		report.Stages = append(report.Stages, StageResult{Stage: "configure_repository", Status: "succeeded"})

		if _, err := i.runWithTimeout(ctx, commandTimeout, i.aptGetPath, "update"); err != nil {
			return i.fail(report, "refresh_package_index", "package_index_refresh_failed")
		}
		report.Stages = append(report.Stages, StageResult{Stage: "refresh_package_index", Status: "succeeded"})

		if _, err := i.runWithTimeout(ctx, verificationTimeout, i.systemctlPath,
			"mask", "--runtime", "--quiet", i.serviceName); err != nil {
			return i.fail(report, "install_package", "service_start_guard_failed")
		}
		installArgs := []string{
			"install",
			"--yes",
			"--no-install-recommends",
			"-o", "Dpkg::Options::=--force-confold",
			"sing-box",
		}
		_, installErr := i.runWithTimeout(ctx, commandTimeout, i.aptGetPath, installArgs...)
		_, unmaskErr := i.runWithTimeout(ctx, verificationTimeout, i.systemctlPath,
			"unmask", "--runtime", "--quiet", i.serviceName)
		if installErr != nil {
			return i.fail(report, "install_package", "package_installation_failed")
		}
		if unmaskErr != nil {
			return i.fail(report, "install_package", "service_start_guard_cleanup_failed")
		}
		report.Stages = append(report.Stages, StageResult{Stage: "install_package", Status: "succeeded"})

		binaryPath = i.packageBinaryPath
		if !regularExecutable(binaryPath) {
			return i.fail(report, "verify_binary", "installed_binary_not_found")
		}
	}

	versionOutput, err := i.runWithTimeout(ctx, verificationTimeout, binaryPath, "version")
	if err != nil {
		return i.fail(report, "verify_binary", "binary_verification_failed")
	}
	report.BinaryPath = binaryPath
	report.SingBoxVersion = truncateString(firstNonEmptyLine(string(versionOutput)), 256)
	if report.SingBoxVersion == "" {
		return i.fail(report, "verify_binary", "binary_version_unavailable")
	}
	report.Stages = append(report.Stages, StageResult{Stage: "verify_binary", Status: "succeeded"})

	serviceOutput, err := i.runWithTimeout(ctx, verificationTimeout, i.systemctlPath,
		"show", "--property", "LoadState", "--value", i.serviceName)
	if err != nil || strings.TrimSpace(string(serviceOutput)) != "loaded" {
		return i.fail(report, "verify_service", "service_verification_failed")
	}
	report.Stages = append(report.Stages, StageResult{Stage: "verify_service", Status: "succeeded"})

	report.Status = "succeeded"
	report.Stages = append(report.Stages, StageResult{Stage: "complete", Status: "succeeded"})
	return report, nil
}

func (i Installer) fail(report Report, stage, code string) (Report, error) {
	report.Status = "failed"
	report.Stages = append(report.Stages, StageResult{Stage: stage, Status: "failed", Code: code})
	stageIndex := -1
	for index, name := range orderedStages {
		if name == stage {
			stageIndex = index
			break
		}
	}
	if stageIndex >= 0 {
		for _, name := range orderedStages[stageIndex+1:] {
			if name == "complete" {
				report.Stages = append(report.Stages, StageResult{Stage: name, Status: "failed", Code: code})
			} else {
				report.Stages = append(report.Stages, StageResult{Stage: name, Status: "skipped", Code: "previous_stage_failed"})
			}
		}
	}
	return report, &InstallError{Stage: stage, Code: code}
}

func (i Installer) runWithTimeout(ctx context.Context, timeout time.Duration, name string, args ...string) ([]byte, error) {
	run := i.run
	if run == nil {
		run = runCommand
	}
	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return run(commandCtx, name, args...)
}

func (i Installer) configureRepository(ctx context.Context) error {
	if _, err := os.Stat(i.sourcePath); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(i.keyPath), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(i.sourcePath), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(i.keyPath); errors.Is(err, os.ErrNotExist) {
		key, downloadErr := i.download(ctx, signingKeyURL)
		if downloadErr != nil {
			return downloadErr
		}
		if err := writeNewAtomic(i.keyPath, key, 0o644); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
	} else if err != nil {
		return err
	}
	if err := writeNewAtomic(i.sourcePath, []byte(repositorySource), 0o644); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	return nil
}

func writeNewAtomic(path string, data []byte, mode os.FileMode) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".routegate-*")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)
	if err := file.Chmod(mode); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	// A hard link publishes the fully written file without replacing an
	// existing repository file if another process won the race.
	return os.Link(tempPath, path)
}

func downloadSigningKey(ctx context.Context, url string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("signing key request returned status %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxSigningKeyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > maxSigningKeyBytes {
		return nil, fmt.Errorf("signing key response has an invalid size")
	}
	return data, nil
}

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func DetectPlatform(osReleasePath, architecture string) (Platform, error) {
	file, err := os.Open(osReleasePath)
	if err != nil {
		return Platform{Architecture: normalizeArchitecture(architecture)}, fmt.Errorf("read os-release: %w", err)
	}
	defer file.Close()

	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		value = strings.TrimSpace(value)
		if unquoted, unquoteErr := strconv.Unquote(value); unquoteErr == nil {
			value = unquoted
		}
		values[strings.TrimSpace(key)] = value
	}
	if err := scanner.Err(); err != nil {
		return Platform{}, fmt.Errorf("parse os-release: %w", err)
	}

	platform := Platform{
		ID:           strings.ToLower(strings.TrimSpace(values["ID"])),
		Version:      strings.TrimSpace(values["VERSION_ID"]),
		Architecture: normalizeArchitecture(architecture),
	}
	if platform.ID == "" {
		return platform, fmt.Errorf("platform id is missing")
	}
	if platform.ID != "debian" && platform.ID != "ubuntu" &&
		!containsWord(strings.ToLower(values["ID_LIKE"]), "debian") {
		return platform, &InstallError{Stage: "detect_platform", Code: "unsupported_distribution"}
	}
	if platform.ID == "ubuntu" && !isUbuntuLTS(platform.Version) {
		return platform, &InstallError{Stage: "detect_platform", Code: "unsupported_distribution"}
	}
	if platform.Architecture != "amd64" && platform.Architecture != "arm64" {
		return platform, &InstallError{Stage: "detect_platform", Code: "unsupported_architecture"}
	}
	return platform, nil
}

func (p Platform) Supported() bool {
	return p.ID != "" && (p.Architecture == "amd64" || p.Architecture == "arm64")
}

func SupportsCurrentPlatform() bool {
	return len(SupportedOperationsForEnvironment(
		defaultOSRelease, runtime.GOARCH, runtime.GOOS, defaultAPTGet, defaultSystemctl, defaultSystemdRun,
	)) > 0
}

func SupportedOperationsForEnvironment(
	osReleasePath, architecture, goos, aptGetPath, systemctlPath, systemdRunPath string,
) []string {
	if goos != "linux" || !regularExecutable(aptGetPath) ||
		!regularExecutable(systemctlPath) || !directoryExists(systemdRunPath) {
		return nil
	}
	platform, err := DetectPlatform(osReleasePath, architecture)
	if err != nil || !platform.Supported() {
		return nil
	}
	return []string{OperationInstall}
}

func regularExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	return runtime.GOOS == "windows" || info.Mode().Perm()&0o111 != 0
}

func directoryExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func normalizeArchitecture(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "x86_64", "amd64":
		return "amd64"
	case "aarch64", "arm64":
		return "arm64"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func isUbuntuLTS(version string) bool {
	majorText, minorText, found := strings.Cut(strings.TrimSpace(version), ".")
	if !found || minorText != "04" {
		return false
	}
	major, err := strconv.Atoi(majorText)
	return err == nil && major >= 20 && major%2 == 0
}

func containsWord(value, expected string) bool {
	for _, word := range strings.Fields(value) {
		if word == expected {
			return true
		}
	}
	return false
}

func firstNonEmptyLine(value string) string {
	scanner := bufio.NewScanner(bytes.NewBufferString(value))
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			return line
		}
	}
	return ""
}

func truncateString(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func errorCode(err error, fallback string) string {
	var installErr *InstallError
	if errors.As(err, &installErr) && installErr.Code != "" {
		return installErr.Code
	}
	return fallback
}
