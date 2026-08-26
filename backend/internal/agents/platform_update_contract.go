package agents

import "strings"

const (
	// Agent DecodePlatformUpdateRequest rejects the complete Manager-generated
	// task payload above 256 bytes. The fixed schema-v1 JSON envelope contributes
	// 38 ASCII bytes, so a canonical targetVersion may occupy at most 218 bytes.
	// Keep this bound aligned with the Agent decoder before changing the task
	// schema or payload representation.
	maxPlatformUpdateAgentTaskPayloadBytes = 256
	platformUpdateTaskEnvelopeBytes        = len(`{"schemaVersion":1,"targetVersion":""}`)
	maxPlatformUpdateTargetVersionBytes    = maxPlatformUpdateAgentTaskPayloadBytes - platformUpdateTaskEnvelopeBytes
)

func validPlatformUpdateTargetVersion(targetVersion string) bool {
	if targetVersion != strings.TrimSpace(targetVersion) {
		return false
	}
	if len(targetVersion) == 0 || len(targetVersion) > maxPlatformUpdateTargetVersionBytes {
		return false
	}
	return canonicalRouteGateVersionPattern.MatchString(targetVersion)
}
