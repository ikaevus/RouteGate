package tasks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
)

const (
	TaskKindPlatformUpdate       = "platform_update"
	PlatformUpdateSchemaVersion = 1
)

var routeGateReleaseVersionPattern = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z][0-9A-Za-z.-]*)?(?:\+[0-9A-Za-z][0-9A-Za-z.-]*)?$`)

// PlatformUpdateRequest is intentionally tiny. Privileged selectors such as
// URLs, paths, artifact names, checksums, repositories, signers, roles and
// commands are reconstructed from fixed RouteGate policy on the node and are
// never accepted from the Manager task payload.
type PlatformUpdateRequest struct {
	SchemaVersion int    `json:"schemaVersion"`
	TargetVersion string `json:"targetVersion"`
}

func DecodePlatformUpdateRequest(payload json.RawMessage) (PlatformUpdateRequest, error) {
	if len(payload) == 0 {
		return PlatformUpdateRequest{}, fmt.Errorf("platform update payload is required")
	}
	if len(payload) > 256 {
		return PlatformUpdateRequest{}, fmt.Errorf("platform update payload is too large")
	}

	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var request PlatformUpdateRequest
	if err := decoder.Decode(&request); err != nil {
		return PlatformUpdateRequest{}, fmt.Errorf("decode platform update payload: %w", err)
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return PlatformUpdateRequest{}, err
	}
	if request.SchemaVersion != PlatformUpdateSchemaVersion {
		return PlatformUpdateRequest{}, fmt.Errorf("unsupported platform update schema version %d", request.SchemaVersion)
	}
	request.TargetVersion = strings.TrimSpace(request.TargetVersion)
	if !routeGateReleaseVersionPattern.MatchString(request.TargetVersion) {
		return PlatformUpdateRequest{}, fmt.Errorf("invalid RouteGate target release version")
	}
	return request, nil
}

func rejectTrailingJSON(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode trailing platform update payload: %w", err)
	}
	return fmt.Errorf("platform update payload must contain exactly one JSON object")
}
