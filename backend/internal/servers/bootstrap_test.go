package servers

import (
	"os/exec"
	"strings"
	"testing"
)

func TestBuildAgentBootstrapCommand(t *testing.T) {
	const (
		version = "v1.2.3"
		commit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		installerSHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	)
	managerURL, command := buildAgentBootstrapCommand(
		"https://manager.routegate.example/",
		"rg_reg_secret",
		version,
		commit,
		installerSHA,
		"",
	)
	if managerURL != "https://manager.routegate.example" {
		t.Fatalf("manager URL = %q", managerURL)
	}
	for _, fragment := range []string{
		"( tmp=$(mktemp)",
		"trap 'rm -f \"$tmp\"' EXIT",
		"https://raw.githubusercontent.com/ikaevus/RouteGate/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/install-agent.sh",
		"'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb' \"$tmp\" | sha256sum -c -",
		"ROUTEGATE_MANAGER_URL='https://manager.routegate.example'",
		"ROUTEGATE_REGISTRATION_TOKEN='rg_reg_secret'",
		"ROUTEGATE_VERSION='v1.2.3'",
		"Bootstrap failed at stage temporary file",
		"Bootstrap failed at stage installer download",
		"Bootstrap failed at stage installer checksum",
		"Bootstrap failed at stage Agent installation",
		"Next action: resolve the error, open Connect server, and generate a fresh command.",
	} {
		if !strings.Contains(command, fragment) {
			t.Fatalf("bootstrap command does not contain %q: %q", fragment, command)
		}
	}
	if err := exec.Command("bash", "-n", "-c", command).Run(); err != nil {
		t.Fatalf("generated bootstrap command has invalid shell syntax: %v", err)
	}
}

func TestBuildAgentBootstrapCommandRejectsUnsafePublicURL(t *testing.T) {
	const (
		version = "v1.2.3"
		commit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		installerSHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	)
	for _, value := range []string{
		"",
		"http://manager.routegate.example",
		"https://user:pass@manager.routegate.example",
		"https://manager.routegate.example/path",
		"https://manager.routegate.example/?token=secret",
	} {
		managerURL, command := buildAgentBootstrapCommand(value, "rg_reg_secret", version, commit, installerSHA, "")
		if managerURL != "" || command != "" {
			t.Fatalf("unsafe public URL %q produced bootstrap material", value)
		}
	}
}

func TestBuildAgentBootstrapCommandRejectsUntrustedBuildIdentity(t *testing.T) {
	tests := []struct {
		name, version, commit, sha string
	}{
		{name: "development version", version: "dev", commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", sha: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
		{name: "non-release production-like version", version: "production-like", commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", sha: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
		{name: "short commit", version: "v1.2.3", commit: "deadbeef", sha: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
		{name: "unknown checksum", version: "v1.2.3", commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", sha: "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			managerURL, command := buildAgentBootstrapCommand(
				"https://manager.routegate.example",
				"rg_reg_secret",
				tt.version,
				tt.commit,
				tt.sha,
				"",
			)
			if managerURL != "" || command != "" {
				t.Fatalf("untrusted build identity produced bootstrap material: manager=%q command=%q", managerURL, command)
			}
		})
	}
}


func TestBuildAgentBootstrapCommandSupportsCommitAddressedBundleSource(t *testing.T) {
	const (
		version      = "production-like"
		commit       = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		installerSHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		bundleBase   = "https://manager.routegate.example/bootstrap/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/"
	)
	managerURL, command := buildAgentBootstrapCommand(
		"https://manager.routegate.example/",
		"rg_reg_secret",
		version,
		commit,
		installerSHA,
		bundleBase,
	)
	if managerURL != "https://manager.routegate.example" {
		t.Fatalf("manager URL = %q", managerURL)
	}
	for _, fragment := range []string{
		"ROUTEGATE_VERSION='production-like'",
		"ROUTEGATE_BUNDLE_BASE_URL='https://manager.routegate.example/bootstrap/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'",
		"https://raw.githubusercontent.com/ikaevus/RouteGate/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/install-agent.sh",
	} {
		if !strings.Contains(command, fragment) {
			t.Fatalf("bootstrap command %q does not contain %q", command, fragment)
		}
	}
}

func TestBuildAgentBootstrapCommandRejectsUnsafeBundleBaseURL(t *testing.T) {
	const (
		version      = "production-like"
		commit       = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		installerSHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	)
	for _, value := range []string{
		"http://manager.routegate.example/bootstrap/commit",
		"https://user:pass@manager.routegate.example/bootstrap/commit",
		"https://manager.routegate.example/bootstrap/commit?token=secret",
		"https://manager.routegate.example/bootstrap/commit#fragment",
	} {
		managerURL, command := buildAgentBootstrapCommand(
			"https://manager.routegate.example",
			"rg_reg_secret",
			version,
			commit,
			installerSHA,
			value,
		)
		if managerURL != "" || command != "" {
			t.Fatalf("unsafe bundle base URL %q produced bootstrap material", value)
		}
	}
}

func TestShellSingleQuoteEscapesEmbeddedQuote(t *testing.T) {
	if got, want := shellSingleQuote("one'two"), `'one'"'"'two'`; got != want {
		t.Fatalf("quoted value = %q, want %q", got, want)
	}
}
