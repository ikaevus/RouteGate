package servers

import "testing"

func TestBuildAgentBootstrapCommand(t *testing.T) {
	managerURL, command := buildAgentBootstrapCommand("https://manager.routegate.example/", "rg_reg_secret")
	if managerURL != "https://manager.routegate.example" {
		t.Fatalf("manager URL = %q", managerURL)
	}
	want := "curl -fsSL https://raw.githubusercontent.com/ikaevus/RouteGate/main/install-agent.sh | sudo env ROUTEGATE_MANAGER_URL='https://manager.routegate.example' ROUTEGATE_REGISTRATION_TOKEN='rg_reg_secret' bash"
	if command != want {
		t.Fatalf("bootstrap command = %q, want %q", command, want)
	}
}

func TestBuildAgentBootstrapCommandRejectsUnsafePublicURL(t *testing.T) {
	for _, value := range []string{
		"",
		"http://manager.routegate.example",
		"https://user:pass@manager.routegate.example",
		"https://manager.routegate.example/path",
		"https://manager.routegate.example/?token=secret",
	} {
		managerURL, command := buildAgentBootstrapCommand(value, "rg_reg_secret")
		if managerURL != "" || command != "" {
			t.Fatalf("unsafe public URL %q produced bootstrap material", value)
		}
	}
}

func TestShellSingleQuoteEscapesEmbeddedQuote(t *testing.T) {
	if got, want := shellSingleQuote("one'two"), `'one'"'"'two'`; got != want {
		t.Fatalf("quoted value = %q, want %q", got, want)
	}
}
