package vpncoreinstall

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
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
