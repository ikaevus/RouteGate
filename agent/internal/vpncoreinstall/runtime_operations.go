package vpncoreinstall

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	OperationInstallWireGuard = "install_wireguard"
	OperationInstallHysteria2 = "install_hysteria2"
	OperationInstallMTG       = "install_mtg"

	defaultHysteria2Version = "2.12.1"
	defaultMTGVersion       = "2.2.8"

	defaultHysteria2Path    = "/usr/local/bin/hysteria"
	defaultHysteria2Service = "/etc/systemd/system/hysteria-server.service"
	defaultMTGPath          = "/usr/local/bin/mtg"
	defaultMTGService       = "/etc/systemd/system/routegate-mtproto.service"
	maxRuntimeDownloadBytes = 128 << 20
)

const hysteria2ServiceUnit = `[Unit]
Description=RouteGate managed Hysteria2 server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/hysteria server -c /etc/hysteria/config.json
Environment=HYSTERIA_DISABLE_UPDATE_CHECK=1
Restart=on-failure
RestartSec=3s
UMask=0077
NoNewPrivileges=true
PrivateTmp=true
PrivateDevices=true
ProtectSystem=strict
ProtectHome=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictRealtime=true
RestrictSUIDSGID=true
LockPersonality=true
ReadOnlyPaths=/etc/hysteria
ReadWritePaths=/var/lib/hysteria

[Install]
WantedBy=multi-user.target
`

const mtgServiceUnit = `[Unit]
Description=RouteGate managed MTProto proxy
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/mtg run /etc/routegate-mtproto/config.toml
Restart=on-failure
RestartSec=3s
UMask=0077
DynamicUser=true
NoNewPrivileges=true
PrivateTmp=true
PrivateDevices=true
ProtectSystem=strict
ProtectHome=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictRealtime=true
RestrictSUIDSGID=true
LockPersonality=true
ReadOnlyPaths=/etc/routegate-mtproto

[Install]
WantedBy=multi-user.target
`

// SupportedOperations returns every runtime capability this Agent can install
// through the existing vpn_core_install task channel. The task kind remains for
// wire compatibility; operations identify concrete runtimes.
func SupportedOperations() []string {
	if runtime.GOOS != "linux" || !regularExecutable(defaultAPTGet) ||
		!regularExecutable(defaultSystemctl) || !directoryExists(defaultSystemdRun) {
		return nil
	}
	platform, err := DetectPlatform(defaultOSRelease, runtime.GOARCH)
	if err != nil || !platform.Supported() {
		return nil
	}
	return []string{OperationInstall, OperationInstallWireGuard, OperationInstallHysteria2, OperationInstallMTG}
}

// Execute dispatches runtime installation while preserving the mature sing-box
// installer and its existing report contract.
func Execute(ctx context.Context, operation string) (Report, error) {
	operation = strings.TrimSpace(operation)
	if operation == OperationInstall {
		return New().Execute(ctx, operation)
	}
	installer := runtimeInstaller{
		osReleasePath:  defaultOSRelease,
		architecture:   runtime.GOARCH,
		aptGetPath:     defaultAPTGet,
		systemctlPath:  defaultSystemctl,
		systemdRunPath: defaultSystemdRun,
		run:            runCommand,
		download:       downloadRuntimeAsset,
	}
	return installer.Execute(ctx, operation)
}

type runtimeInstaller struct {
	osReleasePath  string
	architecture   string
	aptGetPath     string
	systemctlPath  string
	systemdRunPath string
	run            commandRunner
	download       downloader
}

func (i runtimeInstaller) Execute(ctx context.Context, operation string) (Report, error) {
	report := Report{Kind: TaskKind, Operation: strings.TrimSpace(operation), Status: "failed", Stages: make([]StageResult, 0, 8)}
	if report.Operation != OperationInstallWireGuard && report.Operation != OperationInstallHysteria2 && report.Operation != OperationInstallMTG {
		return i.fail(report, "detect_platform", "unsupported_installation_operation")
	}

	platform, err := DetectPlatform(i.osReleasePath, i.architecture)
	report.Platform = platform
	if err != nil {
		return i.fail(report, "detect_platform", errorCode(err, "platform_detection_failed"))
	}
	if !platform.Supported() || !regularExecutable(i.aptGetPath) || !regularExecutable(i.systemctlPath) || !directoryExists(i.systemdRunPath) {
		return i.fail(report, "detect_platform", "unsupported_platform")
	}
	report.Stages = append(report.Stages, StageResult{Stage: "detect_platform", Status: "succeeded"})

	switch report.Operation {
	case OperationInstallWireGuard:
		err = i.installWireGuard(ctx, &report)
	case OperationInstallHysteria2:
		err = i.installHysteria2(ctx, &report)
	case OperationInstallMTG:
		err = i.installMTG(ctx, &report)
	}
	if err != nil {
		var installErr *InstallError
		if errors.As(err, &installErr) {
			return i.fail(report, installErr.Stage, installErr.Code)
		}
		return i.fail(report, "install_package", "runtime_installation_failed")
	}

	report.Status = "succeeded"
	report.Stages = append(report.Stages, StageResult{Stage: "complete", Status: "succeeded"})
	return report, nil
}

func (i runtimeInstaller) installWireGuard(ctx context.Context, report *Report) error {
	wgPath, wgErr := exec.LookPath("wg")
	_, quickErr := exec.LookPath("wg-quick")
	alreadyInstalled := wgErr == nil && quickErr == nil
	report.Stages = append(report.Stages, StageResult{Stage: "check_existing_installation", Status: "succeeded", Code: installedCode(alreadyInstalled)})
	if !alreadyInstalled {
		if _, err := i.runWithTimeout(ctx, commandTimeout, i.aptGetPath, "update"); err != nil {
			return &InstallError{Stage: "refresh_package_index", Code: "package_index_refresh_failed"}
		}
		report.Stages = append(report.Stages, StageResult{Stage: "refresh_package_index", Status: "succeeded"})
		if _, err := i.runWithTimeout(ctx, commandTimeout, i.aptGetPath, "install", "--yes", "--no-install-recommends", "wireguard-tools"); err != nil {
			return &InstallError{Stage: "install_package", Code: "package_installation_failed"}
		}
		report.Stages = append(report.Stages, StageResult{Stage: "install_package", Status: "succeeded"})
		wgPath, wgErr = exec.LookPath("wg")
		_, quickErr = exec.LookPath("wg-quick")
	} else {
		report.Stages = append(report.Stages,
			StageResult{Stage: "refresh_package_index", Status: "skipped", Code: "already_installed"},
			StageResult{Stage: "install_package", Status: "skipped", Code: "already_installed"},
		)
	}
	if wgErr != nil || quickErr != nil {
		return &InstallError{Stage: "verify_binary", Code: "installed_binary_not_found"}
	}
	output, err := i.runWithTimeout(ctx, verificationTimeout, wgPath, "--version")
	if err != nil || firstNonEmptyLine(string(output)) == "" {
		return &InstallError{Stage: "verify_binary", Code: "binary_verification_failed"}
	}
	report.BinaryPath = wgPath
	report.Stages = append(report.Stages, StageResult{Stage: "verify_binary", Status: "succeeded"})
	return nil
}

func (i runtimeInstaller) installHysteria2(ctx context.Context, report *Report) error {
	report.ServiceName = "hysteria-server.service"
	managed, installed, err := inspectManagedRuntime(defaultHysteria2Path, defaultHysteria2Service, hysteria2ServiceUnit)
	if err != nil {
		return err
	}
	if installed && !managed {
		return &InstallError{Stage: "check_existing_installation", Code: "unmanaged_runtime_conflict"}
	}
	report.Stages = append(report.Stages, StageResult{Stage: "check_existing_installation", Status: "succeeded", Code: installedCode(installed)})
	if !installed {
		asset := "hysteria-linux-" + normalizeArchitecture(i.architecture)
		base := "https://github.com/apernet/hysteria/releases/download/app%2Fv" + defaultHysteria2Version
		binary, err := i.downloadWithTimeout(ctx, base+"/"+asset)
		if err != nil {
			return &InstallError{Stage: "download_runtime", Code: "runtime_download_failed"}
		}
		hashes, err := i.downloadWithTimeout(ctx, base+"/hashes.txt")
		if err != nil {
			return &InstallError{Stage: "verify_checksum", Code: "checksum_download_failed"}
		}
		if err := verifyNamedSHA256(binary, hashes, asset); err != nil {
			return &InstallError{Stage: "verify_checksum", Code: "checksum_verification_failed"}
		}
		if err := installManagedBinary(defaultHysteria2Path, binary); err != nil {
			return &InstallError{Stage: "install_package", Code: "runtime_binary_installation_failed"}
		}
		if err := ensureManagedTextFile(defaultHysteria2Service, hysteria2ServiceUnit, 0o644); err != nil {
			return &InstallError{Stage: "install_package", Code: "service_unit_installation_failed"}
		}
		for _, dir := range []string{"/etc/hysteria", "/var/lib/hysteria", "/var/lib/hysteria/acme"} {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return &InstallError{Stage: "install_package", Code: "runtime_directory_creation_failed"}
			}
		}
		if err := i.reloadAndLeaveInactive(ctx, report.ServiceName); err != nil {
			return err
		}
		report.Stages = append(report.Stages, StageResult{Stage: "install_package", Status: "succeeded", Code: "installed_unconfigured"})
	} else {
		report.Stages = append(report.Stages, StageResult{Stage: "install_package", Status: "skipped", Code: "already_installed"})
	}
	return i.verifyManagedServiceRuntime(ctx, report, defaultHysteria2Path, "version")
}

func (i runtimeInstaller) installMTG(ctx context.Context, report *Report) error {
	report.ServiceName = "routegate-mtproto.service"
	managed, installed, err := inspectManagedRuntime(defaultMTGPath, defaultMTGService, mtgServiceUnit)
	if err != nil {
		return err
	}
	if installed && !managed {
		return &InstallError{Stage: "check_existing_installation", Code: "unmanaged_runtime_conflict"}
	}
	report.Stages = append(report.Stages, StageResult{Stage: "check_existing_installation", Status: "succeeded", Code: installedCode(installed)})
	if !installed {
		arch := normalizeArchitecture(i.architecture)
		asset := fmt.Sprintf("mtg-%s-linux-%s.tar.gz", defaultMTGVersion, arch)
		base := "https://github.com/9seconds/mtg/releases/download/v" + defaultMTGVersion
		archive, err := i.downloadWithTimeout(ctx, base+"/"+asset)
		if err != nil {
			return &InstallError{Stage: "download_runtime", Code: "runtime_download_failed"}
		}
		checksums, err := i.downloadWithTimeout(ctx, base+"/mtg-"+defaultMTGVersion+"-checksums.txt")
		if err != nil {
			return &InstallError{Stage: "verify_checksum", Code: "checksum_download_failed"}
		}
		if err := verifyNamedSHA256(archive, checksums, asset); err != nil {
			return &InstallError{Stage: "verify_checksum", Code: "checksum_verification_failed"}
		}
		expectedPath := fmt.Sprintf("mtg-%s-linux-%s/mtg", defaultMTGVersion, arch)
		binary, err := extractSingleRegularTarGzipFile(archive, expectedPath)
		if err != nil {
			return &InstallError{Stage: "verify_archive", Code: "unsafe_or_invalid_runtime_archive"}
		}
		if err := installManagedBinary(defaultMTGPath, binary); err != nil {
			return &InstallError{Stage: "install_package", Code: "runtime_binary_installation_failed"}
		}
		if err := ensureManagedTextFile(defaultMTGService, mtgServiceUnit, 0o644); err != nil {
			return &InstallError{Stage: "install_package", Code: "service_unit_installation_failed"}
		}
		if err := os.MkdirAll("/etc/routegate-mtproto", 0o700); err != nil {
			return &InstallError{Stage: "install_package", Code: "runtime_directory_creation_failed"}
		}
		if err := i.reloadAndLeaveInactive(ctx, report.ServiceName); err != nil {
			return err
		}
		report.Stages = append(report.Stages, StageResult{Stage: "install_package", Status: "succeeded", Code: "installed_unconfigured"})
	} else {
		report.Stages = append(report.Stages, StageResult{Stage: "install_package", Status: "skipped", Code: "already_installed"})
	}
	return i.verifyManagedServiceRuntime(ctx, report, defaultMTGPath, "--version")
}

func (i runtimeInstaller) verifyManagedServiceRuntime(ctx context.Context, report *Report, binaryPath, versionArg string) error {
	if !regularExecutable(binaryPath) {
		return &InstallError{Stage: "verify_binary", Code: "installed_binary_not_found"}
	}
	output, err := i.runWithTimeout(ctx, verificationTimeout, binaryPath, versionArg)
	if err != nil || firstNonEmptyLine(string(output)) == "" {
		return &InstallError{Stage: "verify_binary", Code: "binary_verification_failed"}
	}
	report.BinaryPath = binaryPath
	report.Stages = append(report.Stages, StageResult{Stage: "verify_binary", Status: "succeeded"})
	serviceOutput, err := i.runWithTimeout(ctx, verificationTimeout, i.systemctlPath, "show", "--property", "LoadState", "--value", report.ServiceName)
	if err != nil || strings.TrimSpace(string(serviceOutput)) != "loaded" {
		return &InstallError{Stage: "verify_service", Code: "service_verification_failed"}
	}
	report.Stages = append(report.Stages, StageResult{Stage: "verify_service", Status: "succeeded"})
	return nil
}

func (i runtimeInstaller) reloadAndLeaveInactive(ctx context.Context, serviceName string) error {
	if _, err := i.runWithTimeout(ctx, verificationTimeout, i.systemctlPath, "daemon-reload"); err != nil {
		return &InstallError{Stage: "install_package", Code: "systemd_reload_failed"}
	}
	if _, err := i.runWithTimeout(ctx, verificationTimeout, i.systemctlPath, "disable", "--now", "--quiet", serviceName); err != nil {
		return &InstallError{Stage: "install_package", Code: "service_stop_disable_failed"}
	}
	return nil
}

func (i runtimeInstaller) runWithTimeout(ctx context.Context, timeout time.Duration, name string, args ...string) ([]byte, error) {
	run := i.run
	if run == nil {
		run = runCommand
	}
	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return run(commandCtx, name, args...)
}

func (i runtimeInstaller) downloadWithTimeout(ctx context.Context, rawURL string) ([]byte, error) {
	download := i.download
	if download == nil {
		download = downloadRuntimeAsset
	}
	downloadCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	return download(downloadCtx, rawURL)
}

func (i runtimeInstaller) fail(report Report, stage, code string) (Report, error) {
	report.Status = "failed"
	report.Stages = append(report.Stages, StageResult{Stage: stage, Status: "failed", Code: code})
	report.Stages = append(report.Stages, StageResult{Stage: "complete", Status: "failed", Code: code})
	return report, &InstallError{Stage: stage, Code: code}
}

func installedCode(installed bool) string {
	if installed {
		return "already_installed"
	}
	return "not_installed"
}

func inspectManagedRuntime(binaryPath, servicePath, expectedUnit string) (managed, installed bool, err error) {
	binaryExists := regularExecutable(binaryPath)
	unit, unitErr := os.ReadFile(servicePath)
	unitExists := unitErr == nil
	if unitErr != nil && !errors.Is(unitErr, os.ErrNotExist) {
		return false, binaryExists, unitErr
	}
	installed = binaryExists || unitExists
	if !installed {
		return true, false, nil
	}
	return binaryExists && unitExists && string(unit) == expectedUnit, true, nil
}

func ensureManagedTextFile(path, expected string, mode os.FileMode) error {
	if data, err := os.ReadFile(path); err == nil {
		if string(data) != expected {
			return fmt.Errorf("managed file conflict")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeNewAtomic(path, []byte(expected), mode)
}

func installManagedBinary(path string, payload []byte) error {
	if len(payload) == 0 {
		return fmt.Errorf("empty runtime binary")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("runtime binary already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".routegate-runtime-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(payload); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func verifyNamedSHA256(payload, checksumFile []byte, asset string) error {
	var expected string
	for _, line := range strings.Split(string(checksumFile), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(fields[len(fields)-1], "*")
		if name == asset {
			expected = strings.ToLower(fields[0])
			break
		}
	}
	if len(expected) != sha256.Size*2 {
		return fmt.Errorf("checksum entry is missing")
	}
	if _, err := hex.DecodeString(expected); err != nil {
		return fmt.Errorf("checksum entry is invalid")
	}
	actual := sha256.Sum256(payload)
	if hex.EncodeToString(actual[:]) != expected {
		return fmt.Errorf("checksum mismatch")
	}
	return nil
}

func extractSingleRegularTarGzipFile(archive []byte, expectedPath string) ([]byte, error) {
	gzipReader, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	var result []byte
	found := 0
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		clean := filepath.Clean(header.Name)
		if filepath.IsAbs(header.Name) || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
			return nil, fmt.Errorf("unsafe archive path")
		}
		if header.Typeflag == tar.TypeSymlink || header.Typeflag == tar.TypeLink {
			return nil, fmt.Errorf("archive links are not allowed")
		}
		if clean != expectedPath {
			continue
		}
		if header.Typeflag != tar.TypeReg || header.Size <= 0 || header.Size > maxRuntimeDownloadBytes {
			return nil, fmt.Errorf("invalid runtime binary entry")
		}
		payload, err := io.ReadAll(io.LimitReader(tarReader, maxRuntimeDownloadBytes+1))
		if err != nil || int64(len(payload)) != header.Size {
			return nil, fmt.Errorf("read runtime binary")
		}
		result = payload
		found++
	}
	if found != 1 || len(result) == 0 {
		return nil, fmt.Errorf("expected runtime binary was not found exactly once")
	}
	return result, nil
}

func downloadRuntimeAsset(ctx context.Context, rawURL string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	response, err := (&http.Client{Timeout: 5 * time.Minute}).Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("runtime download returned status %d", response.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxRuntimeDownloadBytes+1))
	if err != nil {
		return nil, err
	}
	if len(payload) == 0 || len(payload) > maxRuntimeDownloadBytes {
		return nil, fmt.Errorf("runtime download has invalid size")
	}
	return payload, nil
}
