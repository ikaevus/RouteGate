package servers

import (
	"net/url"
	"strings"
)

const agentInstallerURL = "https://raw.githubusercontent.com/ikaevus/RouteGate/main/install-agent.sh"

func agentBootstrapAvailable(publicURL string) bool {
	return normalizeBootstrapManagerURL(publicURL) != ""
}

func buildAgentBootstrapCommand(publicURL, registrationToken string) (string, string) {
	managerURL := normalizeBootstrapManagerURL(publicURL)
	registrationToken = strings.TrimSpace(registrationToken)
	if managerURL == "" || registrationToken == "" {
		return "", ""
	}

	command := "curl -fsSL " + agentInstallerURL +
		" | sudo env ROUTEGATE_MANAGER_URL=" + shellSingleQuote(managerURL) +
		" ROUTEGATE_REGISTRATION_TOKEN=" + shellSingleQuote(registrationToken) + " bash"
	return managerURL, command
}

func normalizeBootstrapManagerURL(value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return ""
	}
	parsed.Path = ""
	parsed.RawPath = ""
	return strings.TrimRight(parsed.String(), "/")
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
