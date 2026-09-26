package servers

import "testing"

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
	)
	if managerURL != "https://manager.routegate.example" {
		t.Fatalf("manager URL = %q", managerURL)
	}
	want := "tmp=$(mktemp) || exit 1; trap 'rm -f \"$tmp\"' EXIT; curl -fL --proto '=https' --tlsv1.2 --retry 3 --connect-timeout 15 'https://raw.githubusercontent.com/ikaevus/RouteGate/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/install-agent.sh' -o \"$tmp\" && printf '%s  %s\\n' 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb' \"$tmp\" | sha256sum -c - && sudo env ROUTEGATE_MANAGER_URL='https://manager.routegate.example' ROUTEGATE_REGISTRATION_TOKEN='rg_reg_secret' ROUTEGATE_VERSION='v1.2.3' bash \"$tmp\""
	if command != want {
		t.Fatalf("bootstrap command = %q, want %q", command, want)
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
		managerURL, command := buildAgentBootstrapCommand(value, "rg_reg_secret", version, commit, installerSHA)
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
			)
			if managerURL != "" || command != "" {
				t.Fatalf("untrusted build identity produced bootstrap material: manager=%q command=%q", managerURL, command)
			}
		})
	}
}

func TestShellSingleQuoteEscapesEmbeddedQuote(t *testing.T) {
	if got, want := shellSingleQuote("one'two"), `'one'"'"'two'`; got != want {
		t.Fatalf("quoted value = %q, want %q", got, want)
	}
}
