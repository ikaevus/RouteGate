package servers

import (
	"net/url"
	"regexp"
	"strings"
)

const agentInstallerBaseURL = "https://raw.githubusercontent.com/ikaevus/RouteGate"

var (
	fullGitSHAPattern        = regexp.MustCompile(`^[a-f0-9]{40}$`)
	sha256Pattern            = regexp.MustCompile(`^[a-f0-9]{64}$`)
	releaseVersionPattern    = regexp.MustCompile(`^v[A-Za-z0-9][A-Za-z0-9._+-]*$`)
)

func agentBootstrapAvailable(publicURL, version, commit, installerSHA256 string) bool {
	return normalizeBootstrapManagerURL(publicURL) != "" &&
		bootstrapBuildIdentityValid(version, commit, installerSHA256)
}

func buildAgentBootstrapCommand(publicURL, registrationToken, version, commit, installerSHA256 string) (string, string) {
	managerURL := normalizeBootstrapManagerURL(publicURL)
	registrationToken = strings.TrimSpace(registrationToken)
	version = strings.TrimSpace(version)
	commit = strings.TrimSpace(commit)
	installerSHA256 = strings.TrimSpace(installerSHA256)
	if managerURL == "" || registrationToken == "" || !bootstrapBuildIdentityValid(version, commit, installerSHA256) {
		return "", ""
	}

	installerURL := agentInstallerBaseURL + "/" + commit + "/install-agent.sh"
	command := "tmp=$(mktemp) || exit 1; " +
		"trap 'rm -f \"$tmp\"' EXIT; " +
		"curl -fL --proto '=https' --tlsv1.2 --retry 3 --connect-timeout 15 " + shellSingleQuote(installerURL) + " -o \"$tmp\" && " +
		"printf '%s  %s\\n' " + shellSingleQuote(installerSHA256) + " \"$tmp\" | sha256sum -c - && " +
		"sudo env ROUTEGATE_MANAGER_URL=" + shellSingleQuote(managerURL) +
		" ROUTEGATE_REGISTRATION_TOKEN=" + shellSingleQuote(registrationToken) +
		" ROUTEGATE_VERSION=" + shellSingleQuote(version) +
		" bash \"$tmp\""
	return managerURL, command
}

func bootstrapBuildIdentityValid(version, commit, installerSHA256 string) bool {
	return releaseVersionPattern.MatchString(strings.TrimSpace(version)) &&
		fullGitSHAPattern.MatchString(strings.TrimSpace(commit)) &&
		sha256Pattern.MatchString(strings.TrimSpace(installerSHA256))
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
