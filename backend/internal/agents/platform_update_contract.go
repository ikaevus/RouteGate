package agents

import "strings"

const maxPlatformUpdateAgentTaskPayloadBytes = 256

func validPlatformUpdateTargetVersion(targetVersion string) bool {
	if targetVersion != strings.TrimSpace(targetVersion) || targetVersion == "" {
		return false
	}
	if !canonicalRouteGateVersionPattern.MatchString(targetVersion) {
		return false
	}
	// Validate against the exact Manager serialization used by task claim rather
	// than a duplicated character-count formula. This keeps the public create
	// contract fail-closed if the schema-v1 payload representation ever changes
	// while the Agent's 256-byte decoder bound remains in force.
	payload, err := platformUpdateTaskPayload(targetVersion)
	return err == nil && len(payload) <= maxPlatformUpdateAgentTaskPayloadBytes
}
