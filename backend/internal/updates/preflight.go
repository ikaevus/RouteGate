package updates

import (
	"strconv"
	"strings"

	"github.com/ikaevus/routegate/backend/internal/buildinfo"
)

const (
	BlockerManagerBuildMetadataUnavailable = "manager_build_metadata_unavailable"
	BlockerDatabaseSchemaMismatch          = "database_schema_mismatch"
)

func EvaluatePreflight(info buildinfo.Info, appliedMigration string) PreflightResult {
	blockers := make([]string, 0, 2)
	if managerBuildMetadataUnavailable(info) {
		blockers = append(blockers, BlockerManagerBuildMetadataUnavailable)
	}

	generation, ok := migrationGeneration(appliedMigration)
	if !ok || generation != info.ExpectedDatabaseSchemaVersion {
		blockers = append(blockers, BlockerDatabaseSchemaMismatch)
	}

	decision := DecisionProceed
	if len(blockers) > 0 {
		decision = DecisionBlocked
	}

	return PreflightResult{
		Decision:                  decision,
		Blockers:                  blockers,
		ManagerVersion:            info.Version,
		ManagerGitCommit:          info.GitCommit,
		ManagerBuildDate:          info.BuildDate,
		DatabaseAppliedMigration:  strings.TrimSpace(appliedMigration),
		ExpectedSchemaVersion:     info.ExpectedDatabaseSchemaVersion,
		UpdateStatus:              info.UpdateStatus,
		UpdateChannel:             info.UpdateChannel,
		AutomaticUpdatesSupported: info.AutomaticUpdatesSupported,
		HostTrustPreflight:        HostTrustPreflightDeferred,
	}
}

func managerBuildMetadataUnavailable(info buildinfo.Info) bool {
	version := strings.TrimSpace(info.Version)
	commit := strings.TrimSpace(info.GitCommit)
	buildDate := strings.TrimSpace(info.BuildDate)
	return version == "" || version == "dev" ||
		commit == "" || commit == "unknown" || len(commit) != 40 ||
		buildDate == "" || buildDate == "unknown"
}

func migrationGeneration(identifier string) (int, bool) {
	identifier = strings.TrimSpace(identifier)
	if len(identifier) < 6 {
		return 0, false
	}
	generation, err := strconv.Atoi(identifier[:6])
	if err != nil {
		return 0, false
	}
	return generation, true
}
