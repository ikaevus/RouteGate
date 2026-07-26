package vpncoreinstall

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDetectPlatformSupportsDebianAndUbuntuLTS(t *testing.T) {
	tests := []struct {
		name    string
		content string
		arch    string
		want    Platform
	}{
		{"debian", "ID=debian\nVERSION_ID=\"12\"\n", "x86_64", Platform{ID: "debian", Version: "12", Architecture: "amd64"}},
		{"ubuntu-lts", "ID=ubuntu\nVERSION_ID=\"24.04\"\n", "aarch64", Platform{ID: "ubuntu", Version: "24.04", Architecture: "arm64"}},
		{"debian-compatible", "ID=linuxmint\nID_LIKE=\"ubuntu debian\"\nVERSION_ID=22\n", "amd64", Platform{ID: "linuxmint", Version: "22", Architecture: "amd64"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "os-release")
			if err := os.WriteFile(path, []byte(test.content), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := DetectPlatform(path, test.arch)
			if err != nil {
				t.Fatalf("DetectPlatform: %v", err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("platform = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestDetectPlatformRejectsUnsupportedDistributionAndArchitecture(t *testing.T) {
	tests := []struct {
		content string
		arch    string
		code    string
	}{
		{"ID=fedora\nVERSION_ID=42\n", "amd64", "unsupported_distribution"},
		{"ID=ubuntu\nVERSION_ID=25.10\n", "amd64", "unsupported_distribution"},
		{"ID=debian\nVERSION_ID=12\n", "riscv64", "unsupported_architecture"},
	}
	for _, test := range tests {
		path := filepath.Join(t.TempDir(), "os-release")
		if err := os.WriteFile(path, []byte(test.content), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := DetectPlatform(path, test.arch)
		var installErr *InstallError
		if !errors.As(err, &installErr) || installErr.Code != test.code {
			t.Fatalf("error = %v, want code %q", err, test.code)
		}
	}
}

func TestInstallerAlreadyInstalledIsIdempotent(t *testing.T) {
	installer, calls := testInstaller(t)
	if err := os.WriteFile(installer.packageBinaryPath, []byte("test"), 0o755); err != nil {
		t.Fatal(err)
	}

	report, err := installer.Execute(context.Background(), OperationInstall)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if report.Status != "succeeded" || report.BinaryPath != installer.packageBinaryPath {
		t.Fatalf("unexpected report: %#v", report)
	}
	if len(report.Stages) != len(orderedStages) {
		t.Fatalf("stages = %#v", report.Stages)
	}
	if len(*calls) != 2 {
		t.Fatalf("commands = %#v", *calls)
	}
	for _, call := range *calls {
		if strings.Contains(call, "apt-get") {
			t.Fatalf("idempotent path executed package manager: %q", call)
		}
	}
}

func TestInstallerFailureReturnsEveryStructuredStage(t *testing.T) {
	installer, calls := testInstaller(t)
	if err := os.WriteFile(installer.osReleasePath, []byte("ID=fedora\nVERSION_ID=42\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := installer.Execute(context.Background(), OperationInstall)
	if err == nil {
		t.Fatal("expected unsupported platform error")
	}
	if len(report.Stages) != len(orderedStages) {
		t.Fatalf("stages = %#v", report.Stages)
	}
	for index, stage := range report.Stages {
		if stage.Stage != orderedStages[index] {
			t.Fatalf("stage %d = %#v", index, stage)
		}
	}
	if report.Stages[len(report.Stages)-1].Status != "failed" || report.Status != "failed" {
		t.Fatalf("final failure status missing: %#v", report)
	}
	if len(*calls) != 0 {
		t.Fatalf("unsupported platform executed commands: %#v", *calls)
	}
}

func TestInstallerExecutesOnlyExactAllowListedCommands(t *testing.T) {
	installer, calls := testInstaller(t)

	report, err := installer.Execute(context.Background(), OperationInstall)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := []string{
		installer.aptGetPath + "\x00update",
		installer.systemctlPath + "\x00mask\x00--runtime\x00--quiet\x00sing-box.service",
		installer.aptGetPath + "\x00install\x00--yes\x00--no-install-recommends\x00-o\x00Dpkg::Options::=--force-confold\x00sing-box",
		installer.systemctlPath + "\x00unmask\x00--runtime\x00--quiet\x00sing-box.service",
		installer.packageBinaryPath + "\x00version",
		installer.systemctlPath + "\x00show\x00--property\x00LoadState\x00--value\x00sing-box.service",
	}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("commands = %#v, want %#v", *calls, want)
	}
	for _, call := range *calls {
		name := strings.Split(call, "\x00")[0]
		if name == "sh" || name == "bash" || strings.HasSuffix(name, "/sh") || strings.HasSuffix(name, "/bash") {
			t.Fatalf("shell-string execution detected: %q", call)
		}
	}
	if report.Status != "succeeded" || len(report.Stages) != 8 {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func TestInstallerSigningKeyDownloadTimeoutReturnsStructuredFailure(t *testing.T) {
	installer, calls := testInstaller(t)
	installer.downloadTimeout = 20 * time.Millisecond
	installer.download = func(ctx context.Context, _ string) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	started := time.Now()
	report, err := installer.Execute(context.Background(), OperationInstall)
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("installer remained blocked for %s", elapsed)
	}
	assertInstallErrorCode(t, err, "signing_key_download_timeout")
	assertStageCode(t, report, "configure_repository", "signing_key_download_timeout")
	if len(*calls) != 0 {
		t.Fatalf("commands executed after download timeout: %#v", *calls)
	}
}

func TestSigningKeyHTTPDownloadHonorsContextDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := downloadSigningKey(ctx, server.URL)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("download error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("HTTP download remained blocked for %s", elapsed)
	}
}

func TestInstallerSigningKeyDownloadFailureReturnsStructuredFailure(t *testing.T) {
	installer, calls := testInstaller(t)
	installer.download = func(context.Context, string) ([]byte, error) {
		return nil, errors.New("network unavailable")
	}

	report, err := installer.Execute(context.Background(), OperationInstall)
	assertInstallErrorCode(t, err, "signing_key_download_failed")
	assertStageCode(t, report, "configure_repository", "signing_key_download_failed")
	if len(*calls) != 0 {
		t.Fatalf("commands executed after download failure: %#v", *calls)
	}
}

func TestInstallerRepairsSourcePresentKeyMissing(t *testing.T) {
	installer, _ := testInstaller(t)
	if err := os.MkdirAll(filepath.Dir(installer.sourcePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installer.sourcePath, []byte(repositorySource), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := installer.Execute(context.Background(), OperationInstall); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got, err := os.ReadFile(installer.keyPath)
	if err != nil {
		t.Fatalf("read repaired key: %v", err)
	}
	if string(got) != "test-signing-key" {
		t.Fatalf("key = %q", got)
	}
}

func TestInstallerRepairsKeyPresentSourceMissing(t *testing.T) {
	installer, _ := testInstaller(t)
	if err := os.MkdirAll(filepath.Dir(installer.keyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installer.keyPath, []byte("test-signing-key"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := installer.Execute(context.Background(), OperationInstall); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got, err := os.ReadFile(installer.sourcePath)
	if err != nil {
		t.Fatalf("read repaired source: %v", err)
	}
	if string(got) != repositorySource {
		t.Fatalf("source = %q", got)
	}
}

func TestInstallerRejectsConflictingRepositoryFilesWithoutOverwrite(t *testing.T) {
	tests := []struct {
		name         string
		code         string
		path         func(Installer) string
		content      string
		wantDownload bool
	}{
		{
			name: "source",
			code: "repository_source_conflict",
			path: func(installer Installer) string {
				return installer.sourcePath
			},
			content: "user-managed repository\n",
		},
		{
			name: "key",
			code: "signing_key_conflict",
			path: func(installer Installer) string {
				return installer.keyPath
			},
			content:      "user-managed signing key\n",
			wantDownload: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			installer, calls := testInstaller(t)
			path := test.path(installer)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}
			downloaded := false
			originalDownload := installer.download
			installer.download = func(ctx context.Context, url string) ([]byte, error) {
				downloaded = true
				return originalDownload(ctx, url)
			}

			report, err := installer.Execute(context.Background(), OperationInstall)
			assertInstallErrorCode(t, err, test.code)
			assertStageCode(t, report, "configure_repository", test.code)
			if downloaded != test.wantDownload {
				t.Fatalf("downloaded = %v, want %v", downloaded, test.wantDownload)
			}
			got, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(got) != test.content {
				t.Fatalf("conflicting file overwritten: %q", got)
			}
			if len(*calls) != 0 {
				t.Fatalf("commands executed after conflict: %#v", *calls)
			}
		})
	}
}

func TestInstallerRejectsUnknownOperationBeforeExecution(t *testing.T) {
	installer, calls := testInstaller(t)
	_, err := installer.Execute(context.Background(), "install_anything")
	var installErr *InstallError
	if !errors.As(err, &installErr) || installErr.Code != "unsupported_installation_operation" {
		t.Fatalf("error = %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("commands executed: %#v", *calls)
	}
}

func assertInstallErrorCode(t *testing.T, err error, code string) {
	t.Helper()
	var installErr *InstallError
	if !errors.As(err, &installErr) || installErr.Code != code {
		t.Fatalf("error = %v, want code %q", err, code)
	}
}

func assertStageCode(t *testing.T, report Report, stage, code string) {
	t.Helper()
	for _, result := range report.Stages {
		if result.Stage == stage {
			if result.Status != "failed" || result.Code != code {
				t.Fatalf("stage %q = %#v, want failed code %q", stage, result, code)
			}
			return
		}
	}
	t.Fatalf("stage %q missing from %#v", stage, report.Stages)
}

func TestInstallationCapabilityAdvertisedOnlyForSupportedEnvironment(t *testing.T) {
	root := t.TempDir()
	osReleasePath := filepath.Join(root, "os-release")
	aptGetPath := filepath.Join(root, "apt-get")
	systemctlPath := filepath.Join(root, "systemctl")
	systemdRunPath := filepath.Join(root, "systemd")
	if err := os.WriteFile(osReleasePath, []byte("ID=debian\nVERSION_ID=12\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{aptGetPath, systemctlPath} {
		if err := os.WriteFile(path, []byte("test"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(systemdRunPath, 0o755); err != nil {
		t.Fatal(err)
	}

	if got := SupportedOperationsForEnvironment(osReleasePath, "amd64", "linux", aptGetPath, systemctlPath, systemdRunPath); !reflect.DeepEqual(got, []string{OperationInstall}) {
		t.Fatalf("supported operations = %#v", got)
	}
	if got := SupportedOperationsForEnvironment(osReleasePath, "riscv64", "linux", aptGetPath, systemctlPath, systemdRunPath); len(got) != 0 {
		t.Fatalf("unsupported architecture advertised operations: %#v", got)
	}
	if got := SupportedOperationsForEnvironment(osReleasePath, "amd64", "windows", aptGetPath, systemctlPath, systemdRunPath); len(got) != 0 {
		t.Fatalf("unsupported OS advertised operations: %#v", got)
	}
	if got := SupportedOperationsForEnvironment(osReleasePath, "amd64", "linux", filepath.Join(root, "missing"), systemctlPath, systemdRunPath); len(got) != 0 {
		t.Fatalf("missing APT environment advertised operations: %#v", got)
	}
	if got := SupportedOperationsForEnvironment(osReleasePath, "amd64", "linux", aptGetPath, systemctlPath, filepath.Join(root, "missing")); len(got) != 0 {
		t.Fatalf("missing systemd runtime advertised operations: %#v", got)
	}
}

func testInstaller(t *testing.T) (Installer, *[]string) {
	t.Helper()
	root := t.TempDir()
	osReleasePath := filepath.Join(root, "os-release")
	if err := os.WriteFile(osReleasePath, []byte("ID=debian\nVERSION_ID=12\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"apt-get", "systemctl"} {
		if err := os.WriteFile(filepath.Join(root, path), []byte("test"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	packageBinaryPath := filepath.Join(root, "sing-box")
	systemdRunPath := filepath.Join(root, "systemd")
	if err := os.Mkdir(systemdRunPath, 0o755); err != nil {
		t.Fatal(err)
	}
	calls := []string{}
	installer := Installer{
		osReleasePath:     osReleasePath,
		architecture:      "amd64",
		aptGetPath:        filepath.Join(root, "apt-get"),
		systemctlPath:     filepath.Join(root, "systemctl"),
		systemdRunPath:    systemdRunPath,
		packageBinaryPath: packageBinaryPath,
		keyPath:           filepath.Join(root, "keyrings", "sagernet.asc"),
		sourcePath:        filepath.Join(root, "sources", "sagernet.sources"),
		serviceName:       DefaultServiceName,
		download: func(_ context.Context, url string) ([]byte, error) {
			if url != signingKeyURL {
				t.Fatalf("unexpected URL %q", url)
			}
			return []byte("test-signing-key"), nil
		},
		run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, strings.Join(append([]string{name}, args...), "\x00"))
			switch {
			case strings.HasSuffix(name, "apt-get") && len(args) > 0 && args[0] == "install":
				if err := os.WriteFile(packageBinaryPath, []byte("test"), 0o755); err != nil {
					return nil, err
				}
				return nil, nil
			case strings.HasSuffix(name, "sing-box"):
				return []byte("sing-box version 1.12.0\n"), nil
			case strings.HasSuffix(name, "systemctl"):
				return []byte("loaded\n"), nil
			default:
				return nil, nil
			}
		},
	}
	return installer, &calls
}
