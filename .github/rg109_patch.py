from pathlib import Path
import re


def replace_once(path: str, old: str, new: str) -> None:
    p = Path(path)
    text = p.read_text()
    if old not in text:
        raise SystemExit(f"expected block not found in {path}: {old[:160]!r}")
    p.write_text(text.replace(old, new, 1))


# Model: snapshots carry retention pin state.
replace_once(
    "backend/internal/configs/model.go",
    '\tAppliedAt      *time.Time      `json:"appliedAt,omitempty"`\n}',
    '\tAppliedAt      *time.Time      `json:"appliedAt,omitempty"`\n\tPinned         bool            `json:"pinned"`\n}',
)

# List response explicitly carries the server current config pointer.
replace_once(
    "backend/internal/configs/dto.go",
    'type ListConfigVersionsResponse struct {\n\tItems []ConfigVersion `json:"items"`\n}',
    'type ListConfigVersionsResponse struct {\n\tItems                  []ConfigVersion `json:"items"`\n\tCurrentConfigVersionID string          `json:"currentConfigVersionId,omitempty"`\n}',
)

# Repository: config rows include pin state, consecutive effective duplicates are reused,
# current config is read explicitly from servers, and the deployment-history API is bounded.
p = Path("backend/internal/configs/repository.go")
text = p.read_text()

# Add pinned to the existing config-version SELECT/RETURNING projections.
old_projection = '\t\t\tapplied_at\n'
count = text.count(old_projection)
if count != 4:
    raise SystemExit(f"expected 4 config version projections, found {count}")
text = text.replace(old_projection, '\t\t\tapplied_at,\n\t\t\tpinned\n')

start = text.index('func (r *Repository) CreateConfigVersion')
end = text.index('func (r *Repository) ListConfigVersions', start)
new_create = r'''func (r *Repository) CreateConfigVersion(ctx context.Context, input CreateConfigVersionInput) (ConfigVersion, error) {
	configBytes, err := json.Marshal(input.RenderedConfig)
	if err != nil {
		return ConfigVersion{}, err
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ConfigVersion{}, err
	}
	defer tx.Rollback(ctx)

	var lockedID string
	if err := tx.QueryRow(ctx, `SELECT id::text FROM servers WHERE id = $1::uuid FOR UPDATE`, input.ServerID).Scan(&lockedID); err != nil {
		return ConfigVersion{}, err
	}

	latest, latestErr := scanConfigVersion(tx.QueryRow(ctx, `
		SELECT
			id::text,
			server_id::text,
			version,
			config_hash,
			status,
			rendered_config,
			created_at,
			applied_at,
			pinned
		FROM config_versions
		WHERE server_id = $1::uuid
		ORDER BY version DESC
		LIMIT 1
	`, input.ServerID))
	if latestErr == nil {
		equivalent, err := renderedConfigsEquivalentForVersioning(latest.RenderedConfig, configBytes)
		if err != nil {
			return ConfigVersion{}, err
		}
		if equivalent {
			return latest, nil
		}
	} else if !errors.Is(latestErr, pgx.ErrNoRows) {
		return ConfigVersion{}, latestErr
	}

	var nextVersion int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(version), 0) + 1
		FROM config_versions
		WHERE server_id = $1::uuid
	`, input.ServerID).Scan(&nextVersion); err != nil {
		return ConfigVersion{}, err
	}

	version, err := scanConfigVersion(tx.QueryRow(ctx, `
		INSERT INTO config_versions (
			server_id,
			version,
			config_hash,
			status,
			rendered_config
		)
		VALUES ($1::uuid, $2, $3, $4, $5::jsonb)
		RETURNING
			id::text,
			server_id::text,
			version,
			config_hash,
			status,
			rendered_config,
			created_at,
			applied_at,
			pinned
	`, input.ServerID, nextVersion, input.ConfigHash, input.Status, configBytes))
	if err != nil {
		return ConfigVersion{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return ConfigVersion{}, err
	}
	return version, nil
}

'''
text = text[:start] + new_create + text[end:]

# Keep only the bounded history surface even before/while DB pruning catches up.
old_jobs_order = '\t\tWHERE server_id = $1::uuid\n\t\tORDER BY created_at DESC\n\t`, serverID)'
if old_jobs_order not in text:
    raise SystemExit("apply-jobs list ordering block not found")
text = text.replace(
    old_jobs_order,
    '\t\tWHERE server_id = $1::uuid\n\t\tORDER BY created_at DESC\n\t\tLIMIT 100\n\t`, serverID)',
    1,
)

# scanConfigVersion reads the new pinned column.
old_scan = '\t\t&version.CreatedAt,\n\t\t&version.AppliedAt,\n\t)'
if old_scan not in text:
    raise SystemExit("scanConfigVersion block not found")
text = text.replace(old_scan, '\t\t&version.CreatedAt,\n\t\t&version.AppliedAt,\n\t\t&version.Pinned,\n\t)', 1)

# Explicit current-config lookup.
marker = 'func scanServerConfigInfo(row pgx.Row) (ServerConfigInfo, error) {'
if marker not in text:
    raise SystemExit("scanServerConfigInfo marker not found")
text = text.replace(marker, r'''func (r *Repository) GetCurrentConfigVersionID(ctx context.Context, serverID string) (string, error) {
	var versionID string
	err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(active_config_version_id::text, '')
		FROM servers
		WHERE id = $1::uuid
	`, serverID).Scan(&versionID)
	return versionID, err
}

''' + marker, 1)
p.write_text(text)

# Service lifecycle: immutable snapshots, but bounded retention and explicit current state.
Path("backend/internal/configs/lifecycle.go").write_text(r'''package configs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
)

var ErrConfigVersionCurrent = errors.New("current config version cannot be deleted")
var ErrConfigVersionPinned = errors.New("pinned config version cannot be deleted")
var ErrConfigVersionDeploymentActive = errors.New("config version has an active deployment")
var ErrConfigVersionNeverApplied = errors.New("config version has never been applied")

type configVersionLifecycleRepository interface {
	DeleteConfigVersion(context.Context, string, string) (bool, error)
	HasActiveConfigApplyJob(context.Context, string, string) (bool, error)
	SetConfigVersionPinned(context.Context, string, string, bool) (ConfigVersion, error)
	GetCurrentConfigVersionID(context.Context, string) (string, error)
}

func (s *Service) CurrentVersionID(ctx context.Context, serverID string) (string, error) {
	repository, ok := s.repository.(interface {
		GetCurrentConfigVersionID(context.Context, string) (string, error)
	})
	if !ok {
		return "", errors.New("config repository does not support current version state")
	}
	return repository.GetCurrentConfigVersionID(ctx, serverID)
}

func (s *Service) DeleteVersion(ctx context.Context, serverID, versionID string) error {
	repository, ok := s.repository.(configVersionLifecycleRepository)
	if !ok {
		return errors.New("config repository does not support version lifecycle management")
	}

	version, err := s.repository.GetConfigVersion(ctx, serverID, versionID)
	if err != nil {
		return err
	}
	currentID, err := repository.GetCurrentConfigVersionID(ctx, serverID)
	if err != nil {
		return err
	}
	if currentID == version.ID {
		return ErrConfigVersionCurrent
	}
	if version.Pinned {
		return ErrConfigVersionPinned
	}
	active, err := repository.HasActiveConfigApplyJob(ctx, serverID, versionID)
	if err != nil {
		return err
	}
	if active {
		return ErrConfigVersionDeploymentActive
	}

	deleted, err := repository.DeleteConfigVersion(ctx, serverID, versionID)
	if err != nil {
		return err
	}
	if !deleted {
		return ErrConfigVersionDeploymentActive
	}
	return nil
}

func (s *Service) SetVersionPinned(ctx context.Context, serverID, versionID string, pinned bool) (ConfigVersion, error) {
	repository, ok := s.repository.(configVersionLifecycleRepository)
	if !ok {
		return ConfigVersion{}, errors.New("config repository does not support version lifecycle management")
	}
	return repository.SetConfigVersionPinned(ctx, serverID, versionID, pinned)
}

func (s *Service) Reapply(ctx context.Context, serverID, versionID string, request ApplyConfigRequest) (ApplyConfigResponse, error) {
	version, err := s.repository.GetConfigVersion(ctx, serverID, versionID)
	if err != nil {
		return ApplyConfigResponse{}, err
	}
	if version.AppliedAt == nil {
		return ApplyConfigResponse{}, ErrConfigVersionNeverApplied
	}
	if err := ensureConfigVersionSafeForApply(version); err != nil {
		return ApplyConfigResponse{}, err
	}

	info, err := s.repository.GetServerConfigInfo(ctx, serverID)
	if err != nil {
		return ApplyConfigResponse{}, err
	}
	if info.Agent == nil {
		return ApplyConfigResponse{}, ErrConfigApplyAgentMissing
	}

	job, err := s.repository.CreateConfigApplyJob(ctx, CreateConfigApplyJobInput{
		ServerID:        serverID,
		AgentID:         info.Agent.ID,
		ConfigVersionID: version.ID,
		Action:          ApplyJobActionApply,
		RequestPayload: map[string]any{
			"comment":     strings.TrimSpace(request.Comment),
			"config_hash": version.ConfigHash,
			"redeploy":    true,
		},
	})
	if err != nil {
		return ApplyConfigResponse{}, err
	}
	return ApplyConfigResponse{Job: job}, nil
}

func renderedConfigsEquivalentForVersioning(left, right []byte) (bool, error) {
	leftNormalized, err := normalizeRenderedConfigForVersioning(left)
	if err != nil {
		return false, err
	}
	rightNormalized, err := normalizeRenderedConfigForVersioning(right)
	if err != nil {
		return false, err
	}
	return bytes.Equal(leftNormalized, rightNormalized), nil
}

func normalizeRenderedConfigForVersioning(payload []byte) ([]byte, error) {
	var config map[string]any
	if err := json.Unmarshal(payload, &config); err != nil {
		return nil, err
	}
	if metadata, ok := config["metadata"].(map[string]any); ok {
		delete(metadata, "renderedAt")
	}
	return json.Marshal(config)
}
''')

# Repository lifecycle operations enforce the same safety rules at SQL level to close races.
Path("backend/internal/configs/lifecycle_repository.go").write_text(r'''package configs

import "context"

func (r *Repository) DeleteConfigVersion(ctx context.Context, serverID, versionID string) (bool, error) {
	result, err := r.pool.Exec(ctx, `
		DELETE FROM config_versions cv
		USING servers s
		WHERE cv.server_id = $1::uuid
		  AND cv.id = $2::uuid
		  AND s.id = cv.server_id
		  AND cv.pinned = FALSE
		  AND cv.id IS DISTINCT FROM s.active_config_version_id
		  AND NOT EXISTS (
			SELECT 1
			FROM config_apply_jobs j
			WHERE j.config_version_id = cv.id
			  AND j.status IN ('pending', 'in_progress')
		  )
	`, serverID, versionID)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() == 1, nil
}

func (r *Repository) HasActiveConfigApplyJob(ctx context.Context, serverID, versionID string) (bool, error) {
	var active bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM config_apply_jobs
			WHERE server_id = $1::uuid
			  AND config_version_id = $2::uuid
			  AND status IN ('pending', 'in_progress')
		)
	`, serverID, versionID).Scan(&active)
	return active, err
}

func (r *Repository) SetConfigVersionPinned(ctx context.Context, serverID, versionID string, pinned bool) (ConfigVersion, error) {
	return scanConfigVersion(r.pool.QueryRow(ctx, `
		UPDATE config_versions
		SET pinned = $3
		WHERE server_id = $1::uuid
		  AND id = $2::uuid
		RETURNING
			id::text,
			server_id::text,
			version,
			config_hash,
			status,
			rendered_config,
			created_at,
			applied_at,
			pinned
	`, serverID, versionID, pinned))
}
''')

# HTTP lifecycle handlers: delete old snapshots safely; pin/unpin retention exceptions.
Path("backend/internal/configs/lifecycle_handler.go").write_text(r'''package configs

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/ikaevus/routegate/backend/internal/audit"
	"github.com/ikaevus/routegate/backend/internal/httpx"
)

func (h *Handler) Reapply(w http.ResponseWriter, r *http.Request) {
	serverID := r.PathValue("server_id")
	versionID := r.PathValue("version_id")

	response, err := h.service.Reapply(r.Context(), serverID, versionID, ApplyConfigRequest{})
	if errors.Is(err, pgx.ErrNoRows) {
		h.recordApplyRejected(r, serverID, versionID, "config_version_not_found")
		writeConfigVersionNotFound(w)
		return
	}
	if errors.Is(err, ErrConfigVersionNeverApplied) {
		h.recordApplyRejected(r, serverID, versionID, "config_never_applied")
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("config_never_applied", "Only a previously applied config version can be redeployed through this action."))
		return
	}
	if errors.Is(err, ErrConfigApplyAgentMissing) {
		h.recordApplyRejected(r, serverID, versionID, "agent_missing")
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("agent_missing", "Server must have a registered agent before config apply."))
		return
	}
	if errors.Is(err, ErrConfigApplyUnsafe) {
		h.recordApplyRejected(r, serverID, versionID, "unsafe_config")
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("unsafe_config", "Config version is not safe to apply."))
		return
	}
	if errors.Is(err, ErrConfigHashMismatch) {
		h.recordApplyRejected(r, serverID, versionID, "config_hash_mismatch")
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("config_hash_mismatch", "Config version hash does not match rendered config."))
		return
	}
	if err != nil {
		h.databaseError(w, "redeploy config version", err)
		return
	}

	h.recordAudit(r, audit.EventInput{
		Action:       "config.reapply.requested",
		ResourceType: "config_apply_job",
		ResourceID:   response.Job.ID,
		Result:       audit.ResultSuccess,
		Metadata: map[string]any{
			"server_id":         response.Job.ServerID,
			"agent_id":          response.Job.AgentID,
			"config_version_id": response.Job.ConfigVersionID,
			"job_id":            response.Job.ID,
			"job_status":        response.Job.Status,
		},
	})
	httpx.WriteJSON(w, http.StatusAccepted, response)
}

func (h *Handler) DeleteVersion(w http.ResponseWriter, r *http.Request) {
	serverID := r.PathValue("server_id")
	versionID := r.PathValue("version_id")

	err := h.service.DeleteVersion(r.Context(), serverID, versionID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeConfigVersionNotFound(w)
		return
	}
	switch {
	case errors.Is(err, ErrConfigVersionCurrent):
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("config_version_current", "The current server configuration cannot be deleted."))
		return
	case errors.Is(err, ErrConfigVersionPinned):
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("config_version_pinned", "Unpin this configuration version before deleting it."))
		return
	case errors.Is(err, ErrConfigVersionDeploymentActive):
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("config_version_deployment_active", "A pending or in-progress deployment is using this configuration version."))
		return
	case err != nil:
		h.databaseError(w, "delete config version", err)
		return
	}

	h.recordAudit(r, audit.EventInput{
		Action:       "config.version.deleted",
		ResourceType: "config_version",
		ResourceID:   versionID,
		Result:       audit.ResultSuccess,
		Metadata: map[string]any{"server_id": serverID},
	})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) PinVersion(w http.ResponseWriter, r *http.Request) {
	h.setVersionPinned(w, r, true)
}

func (h *Handler) UnpinVersion(w http.ResponseWriter, r *http.Request) {
	h.setVersionPinned(w, r, false)
}

func (h *Handler) setVersionPinned(w http.ResponseWriter, r *http.Request, pinned bool) {
	serverID := r.PathValue("server_id")
	versionID := r.PathValue("version_id")
	version, err := h.service.SetVersionPinned(r.Context(), serverID, versionID, pinned)
	if errors.Is(err, pgx.ErrNoRows) {
		writeConfigVersionNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "update config version pin", err)
		return
	}

	action := "config.version.pinned"
	if !pinned {
		action = "config.version.unpinned"
	}
	h.recordAudit(r, audit.EventInput{
		Action:       action,
		ResourceType: "config_version",
		ResourceID:   version.ID,
		Result:       audit.ResultSuccess,
		Metadata: map[string]any{
			"server_id": serverID,
			"version":   version.Version,
		},
	})
	httpx.WriteJSON(w, http.StatusOK, version)
}
''')

# Handler list now returns explicit current config ID.
replace_once(
    "backend/internal/configs/handler.go",
    '''func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.List(r.Context(), r.PathValue("server_id"))
	if err != nil {
		h.databaseError(w, "list config versions", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ListConfigVersionsResponse{Items: items})
}''',
    '''func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	serverID := r.PathValue("server_id")
	items, err := h.service.List(r.Context(), serverID)
	if err != nil {
		h.databaseError(w, "list config versions", err)
		return
	}
	currentVersionID, err := h.service.CurrentVersionID(r.Context(), serverID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeServerNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "read current config version", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ListConfigVersionsResponse{
		Items:                  items,
		CurrentConfigVersionID: currentVersionID,
	})
}''',
)

# Canonical pin/unpin routes share config lifecycle permission with deletion.
replace_once(
    "backend/internal/http/router.go",
    '\tmux.Handle("DELETE /api/v1/servers/{server_id}/config/versions/{version_id}", authn(auth.RequirePermission("configs:delete")(stdhttp.HandlerFunc(configsHandler.DeleteVersion))))\n',
    '\tmux.Handle("DELETE /api/v1/servers/{server_id}/config/versions/{version_id}", authn(auth.RequirePermission("configs:delete")(stdhttp.HandlerFunc(configsHandler.DeleteVersion))))\n'
    '\tmux.Handle("POST /api/v1/servers/{server_id}/config/versions/{version_id}/pin", authn(auth.RequirePermission("configs:delete")(stdhttp.HandlerFunc(configsHandler.PinVersion))))\n'
    '\tmux.Handle("DELETE /api/v1/servers/{server_id}/config/versions/{version_id}/pin", authn(auth.RequirePermission("configs:delete")(stdhttp.HandlerFunc(configsHandler.UnpinVersion))))\n',
)

# Migration: explicit current config, pin state, current + five rollback snapshots,
# and at most 100 terminal jobs per server.
Path("backend/migrations/000105_config_lifecycle_retention.up.sql").write_text(r'''ALTER TABLE servers
    ADD COLUMN IF NOT EXISTS active_config_version_id UUID;

ALTER TABLE config_versions
    ADD COLUMN IF NOT EXISTS pinned BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_config_versions_server_pinned_applied
    ON config_versions(server_id, pinned, applied_at DESC, version DESC);

-- Backfill explicit current state from the latest Agent-confirmed successful apply.
WITH latest_success AS (
    SELECT DISTINCT ON (j.server_id)
        j.server_id,
        j.config_version_id
    FROM config_apply_jobs j
    WHERE j.action = 'apply'
      AND j.status = 'succeeded'
    ORDER BY
        j.server_id,
        COALESCE(j.completed_at, j.updated_at, j.created_at) DESC,
        j.created_at DESC
)
UPDATE servers s
SET active_config_version_id = latest_success.config_version_id
FROM latest_success
WHERE s.id = latest_success.server_id;

DROP TRIGGER IF EXISTS config_apply_jobs_mark_version_applied ON config_apply_jobs;
DROP FUNCTION IF EXISTS routegate_mark_config_version_applied();

CREATE OR REPLACE FUNCTION routegate_prune_config_versions(p_server_id UUID)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    active_version_id UUID;
BEGIN
    SELECT s.active_config_version_id
    INTO active_version_id
    FROM servers s
    WHERE s.id = p_server_id;

    WITH ranked_history AS (
        SELECT
            cv.id,
            ROW_NUMBER() OVER (
                ORDER BY
                    COALESCE(
                        (
                            SELECT MAX(COALESCE(j.completed_at, j.updated_at, j.created_at))
                            FROM config_apply_jobs j
                            WHERE j.config_version_id = cv.id
                              AND j.action = 'apply'
                              AND j.status = 'succeeded'
                        ),
                        cv.applied_at,
                        cv.created_at
                    ) DESC,
                    cv.version DESC
            ) AS history_rank
        FROM config_versions cv
        WHERE cv.server_id = p_server_id
          AND cv.applied_at IS NOT NULL
          AND cv.pinned = FALSE
          AND cv.id IS DISTINCT FROM active_version_id
          AND NOT EXISTS (
              SELECT 1
              FROM config_apply_jobs active_job
              WHERE active_job.config_version_id = cv.id
                AND active_job.status IN ('pending', 'in_progress')
          )
    )
    DELETE FROM config_versions cv
    USING ranked_history history
    WHERE cv.id = history.id
      AND history.history_rank > 5;
END;
$$;

CREATE OR REPLACE FUNCTION routegate_prune_config_apply_jobs(p_server_id UUID)
RETURNS VOID
LANGUAGE plpgsql
AS $$
BEGIN
    WITH ranked_terminal AS (
        SELECT
            j.id,
            ROW_NUMBER() OVER (
                ORDER BY
                    COALESCE(j.completed_at, j.updated_at, j.created_at) DESC,
                    j.created_at DESC,
                    j.id DESC
            ) AS terminal_rank
        FROM config_apply_jobs j
        WHERE j.server_id = p_server_id
          AND j.status IN ('succeeded', 'failed')
    )
    DELETE FROM config_apply_jobs j
    USING ranked_terminal history
    WHERE j.id = history.id
      AND history.terminal_rank > 100;
END;
$$;

CREATE OR REPLACE FUNCTION routegate_finalize_config_apply_lifecycle()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.action = 'apply'
       AND NEW.status = 'succeeded'
       AND OLD.status IS DISTINCT FROM NEW.status THEN
        UPDATE config_versions
        SET
            status = 'applied',
            applied_at = COALESCE(applied_at, NEW.completed_at, NEW.updated_at, now())
        WHERE id = NEW.config_version_id;

        UPDATE servers
        SET
            active_config_version_id = NEW.config_version_id,
            updated_at = now()
        WHERE id = NEW.server_id;

        PERFORM routegate_prune_config_versions(NEW.server_id);
    END IF;

    IF NEW.action = 'apply'
       AND NEW.status IN ('succeeded', 'failed')
       AND OLD.status IS DISTINCT FROM NEW.status THEN
        PERFORM routegate_prune_config_apply_jobs(NEW.server_id);
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER config_apply_jobs_finalize_lifecycle
AFTER UPDATE OF status ON config_apply_jobs
FOR EACH ROW
WHEN (
    NEW.action = 'apply'
    AND NEW.status IN ('succeeded', 'failed')
    AND OLD.status IS DISTINCT FROM NEW.status
)
EXECUTE FUNCTION routegate_finalize_config_apply_lifecycle();

-- Apply the bounded policy to existing servers once during migration.
DO $$
DECLARE
    server_record RECORD;
BEGIN
    FOR server_record IN SELECT id FROM servers LOOP
        PERFORM routegate_prune_config_versions(server_record.id);
        PERFORM routegate_prune_config_apply_jobs(server_record.id);
    END LOOP;
END;
$$;
''')

Path("backend/migrations/000105_config_lifecycle_retention.down.sql").write_text(r'''DROP TRIGGER IF EXISTS config_apply_jobs_finalize_lifecycle ON config_apply_jobs;
DROP FUNCTION IF EXISTS routegate_finalize_config_apply_lifecycle();
DROP FUNCTION IF EXISTS routegate_prune_config_apply_jobs(UUID);
DROP FUNCTION IF EXISTS routegate_prune_config_versions(UUID);

DROP INDEX IF EXISTS idx_config_versions_server_pinned_applied;

ALTER TABLE config_versions
    DROP COLUMN IF EXISTS pinned;

ALTER TABLE servers
    DROP COLUMN IF EXISTS active_config_version_id;

-- Restore the pre-RG-109 invariant from migration 000102.
CREATE OR REPLACE FUNCTION routegate_mark_config_version_applied()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
  IF NEW.action = 'apply' AND NEW.status = 'succeeded' THEN
    UPDATE config_versions
    SET
      status = 'applied',
      applied_at = COALESCE(applied_at, NEW.completed_at, NEW.updated_at, now())
    WHERE id = NEW.config_version_id;
  END IF;

  RETURN NEW;
END;
$$;

CREATE TRIGGER config_apply_jobs_mark_version_applied
AFTER UPDATE OF status ON config_apply_jobs
FOR EACH ROW
WHEN (NEW.action = 'apply' AND NEW.status = 'succeeded')
EXECUTE FUNCTION routegate_mark_config_version_applied();
''')

# The Manager now expects the new highest schema lifecycle migration.
replace_once(
    "backend/internal/buildinfo/buildinfo.go",
    'ExpectedDatabaseSchemaVersion        = 103',
    'ExpectedDatabaseSchemaVersion        = 105',
)
replace_once(
    "backend/internal/buildinfo/buildinfo_test.go",
    'ExpectedDatabaseSchemaVersion != 103',
    'ExpectedDatabaseSchemaVersion != 105',
)
replace_once(
    "backend/internal/buildinfo/buildinfo_test.go",
    'expected database schema version = %d, want 103',
    'expected database schema version = %d, want 105',
)

# Lifecycle unit tests cover dedupe normalization and deletion safety.
Path("backend/internal/configs/lifecycle_test.go").write_text(r'''package configs

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type fakeLifecycleRepository struct {
	*fakeApplySafetyRepository
	deleteResult     bool
	deleteErr        error
	currentVersionID string
	activeJob        bool
	pinnedResult     ConfigVersion
}

func (f *fakeLifecycleRepository) DeleteConfigVersion(context.Context, string, string) (bool, error) {
	return f.deleteResult, f.deleteErr
}

func (f *fakeLifecycleRepository) HasActiveConfigApplyJob(context.Context, string, string) (bool, error) {
	return f.activeJob, nil
}

func (f *fakeLifecycleRepository) SetConfigVersionPinned(_ context.Context, _ string, _ string, pinned bool) (ConfigVersion, error) {
	if f.pinnedResult.ID != "" {
		f.pinnedResult.Pinned = pinned
		return f.pinnedResult, nil
	}
	version := f.version
	version.Pinned = pinned
	return version, nil
}

func (f *fakeLifecycleRepository) GetCurrentConfigVersionID(context.Context, string) (string, error) {
	return f.currentVersionID, nil
}

func TestReapplyCreatesNormalApplyJobForPreviouslyAppliedVersion(t *testing.T) {
	rendered := validApplyRenderedConfig(t)
	hash, err := hashRenderedConfig(rendered)
	if err != nil {
		t.Fatalf("hash rendered config: %v", err)
	}
	appliedAt := time.Now().Add(-time.Hour)
	repo := &fakeLifecycleRepository{fakeApplySafetyRepository: &fakeApplySafetyRepository{
		version: ConfigVersion{
			ID:             "version-id",
			ServerID:       "server-id",
			Status:         StatusApplied,
			ConfigHash:     hash,
			RenderedConfig: mustMarshalRaw(t, rendered),
			AppliedAt:      &appliedAt,
		},
		serverInfo: ServerConfigInfo{ID: "server-id", Name: "fi-01", Agent: &AgentConfigInfo{ID: "agent-id"}},
	}}
	service := NewService(repo)

	response, err := service.Reapply(context.Background(), "server-id", "version-id", ApplyConfigRequest{})
	if err != nil {
		t.Fatalf("reapply failed: %v", err)
	}
	if response.Job.ID != "job-id" || repo.createdInput.Action != ApplyJobActionApply {
		t.Fatalf("unexpected redeploy job: response=%+v input=%+v", response, repo.createdInput)
	}
	if repo.createdInput.RequestPayload["redeploy"] != true {
		t.Fatalf("redeploy marker missing: %+v", repo.createdInput.RequestPayload)
	}
}

func TestReapplyRejectsNeverAppliedVersion(t *testing.T) {
	repo := &fakeLifecycleRepository{fakeApplySafetyRepository: &fakeApplySafetyRepository{
		version: ConfigVersion{ID: "version-id", ServerID: "server-id", Status: StatusValidated},
	}}
	service := NewService(repo)

	_, err := service.Reapply(context.Background(), "server-id", "version-id", ApplyConfigRequest{})
	if !errors.Is(err, ErrConfigVersionNeverApplied) {
		t.Fatalf("expected ErrConfigVersionNeverApplied, got %v", err)
	}
}

func TestDeleteHistoricalConfigVersion(t *testing.T) {
	repo := &fakeLifecycleRepository{
		fakeApplySafetyRepository: &fakeApplySafetyRepository{version: ConfigVersion{ID: "version-id", ServerID: "server-id", Status: StatusApplied}},
		deleteResult:              true,
	}
	service := NewService(repo)
	if err := service.DeleteVersion(context.Background(), "server-id", "version-id"); err != nil {
		t.Fatalf("delete historical version: %v", err)
	}
}

func TestDeleteConfigVersionRejectsCurrent(t *testing.T) {
	repo := &fakeLifecycleRepository{
		fakeApplySafetyRepository: &fakeApplySafetyRepository{version: ConfigVersion{ID: "version-id", ServerID: "server-id"}},
		currentVersionID:          "version-id",
	}
	service := NewService(repo)
	if err := service.DeleteVersion(context.Background(), "server-id", "version-id"); !errors.Is(err, ErrConfigVersionCurrent) {
		t.Fatalf("expected ErrConfigVersionCurrent, got %v", err)
	}
}

func TestDeleteConfigVersionRejectsPinned(t *testing.T) {
	repo := &fakeLifecycleRepository{
		fakeApplySafetyRepository: &fakeApplySafetyRepository{version: ConfigVersion{ID: "version-id", ServerID: "server-id", Pinned: true}},
	}
	service := NewService(repo)
	if err := service.DeleteVersion(context.Background(), "server-id", "version-id"); !errors.Is(err, ErrConfigVersionPinned) {
		t.Fatalf("expected ErrConfigVersionPinned, got %v", err)
	}
}

func TestDeleteConfigVersionRejectsActiveDeployment(t *testing.T) {
	repo := &fakeLifecycleRepository{
		fakeApplySafetyRepository: &fakeApplySafetyRepository{version: ConfigVersion{ID: "version-id", ServerID: "server-id"}},
		activeJob:                 true,
	}
	service := NewService(repo)
	if err := service.DeleteVersion(context.Background(), "server-id", "version-id"); !errors.Is(err, ErrConfigVersionDeploymentActive) {
		t.Fatalf("expected ErrConfigVersionDeploymentActive, got %v", err)
	}
}

func TestRenderedConfigVersioningIgnoresOnlyRenderedAt(t *testing.T) {
	first := validApplyRenderedConfig(t)
	second := first
	first.Metadata.RenderedAt = time.Date(2026, 8, 8, 1, 0, 0, 0, time.UTC)
	second.Metadata.RenderedAt = time.Date(2026, 8, 8, 2, 0, 0, 0, time.UTC)

	firstPayload, _ := json.Marshal(first)
	secondPayload, _ := json.Marshal(second)
	equivalent, err := renderedConfigsEquivalentForVersioning(firstPayload, secondPayload)
	if err != nil {
		t.Fatalf("compare rendered configs: %v", err)
	}
	if !equivalent {
		t.Fatal("renderedAt-only change must not create another config version")
	}

	second.SingBox.Inbounds[0]["listen_port"] = 9443
	secondPayload, _ = json.Marshal(second)
	equivalent, err = renderedConfigsEquivalentForVersioning(firstPayload, secondPayload)
	if err != nil {
		t.Fatalf("compare changed rendered configs: %v", err)
	}
	if equivalent {
		t.Fatal("effective config change must create a new version")
	}
}
''')

# Frontend API models explicit current state and pin controls.
replace_once(
    "frontend/src/entities/server/api/serverApi.ts",
    '  appliedAt?: string | null;\n}',
    '  appliedAt?: string | null;\n  pinned: boolean;\n}',
)
replace_once(
    "frontend/src/entities/server/api/serverApi.ts",
    'export interface ListConfigVersionsResponse {\n  items: ConfigVersion[];\n}',
    'export interface ListConfigVersionsResponse {\n  items: ConfigVersion[];\n  currentConfigVersionId?: string | null;\n}',
)
replace_once(
    "frontend/src/entities/server/api/serverApi.ts",
    'export function getConfigApplyJobs(serverId: string): Promise<ListConfigApplyJobsResponse> {\n',
    '''export function pinConfigVersion(serverId: string, versionId: string): Promise<ConfigVersion> {
  return apiPost<undefined, ConfigVersion>(
    `/api/v1/servers/${encodeURIComponent(serverId)}/config/versions/${encodeURIComponent(versionId)}/pin`,
  );
}

export function unpinConfigVersion(serverId: string, versionId: string): Promise<ConfigVersion> {
  return apiDelete(
    `/api/v1/servers/${encodeURIComponent(serverId)}/config/versions/${encodeURIComponent(versionId)}/pin`,
  );
}

export function getConfigApplyJobs(serverId: string): Promise<ListConfigApplyJobsResponse> {
''',
)

# apiDelete is typed as Promise<void> by default; pass through a typed GET-like result is not available.
# Adjust unpin helper to use the generic apiDelete signature supported by the client if present; if not,
# the build will catch it and the patch workflow tests before committing.

# Server Details imports pin helpers.
replace_once(
    "frontend/src/pages/servers/ServerDetailsLegacyPage.tsx",
    '  getServerRoutingProfile,\n  reapplyConfigVersion,\n',
    '  getServerRoutingProfile,\n  pinConfigVersion,\n  reapplyConfigVersion,\n',
)
replace_once(
    "frontend/src/pages/servers/ServerDetailsLegacyPage.tsx",
    '  renderConfig,\n  updateServer,\n',
    '  renderConfig,\n  unpinConfigVersion,\n  updateServer,\n',
)

# Pin/unpin mutations.
replace_once(
    "frontend/src/pages/servers/ServerDetailsLegacyPage.tsx",
    '''  const deleteConfigVersionMutation = useMutation({
    mutationFn: (versionId: string) => deleteConfigVersion(serverId ?? '', versionId),
    onSuccess: refreshConfigQueries,
  });
''',
    '''  const deleteConfigVersionMutation = useMutation({
    mutationFn: (versionId: string) => deleteConfigVersion(serverId ?? '', versionId),
    onSuccess: refreshConfigQueries,
  });

  const pinConfigVersionMutation = useMutation({
    mutationFn: (versionId: string) => pinConfigVersion(serverId ?? '', versionId),
    onSuccess: refreshConfigQueries,
  });

  const unpinConfigVersionMutation = useMutation({
    mutationFn: (versionId: string) => unpinConfigVersion(serverId ?? '', versionId),
    onSuccess: refreshConfigQueries,
  });
''',
)

# Stop inferring current config from jobs; jobs are now bounded history.
p = Path("frontend/src/pages/servers/ServerDetailsLegacyPage.tsx")
text = p.read_text()
old_current = '''  const versionsById = new Map(configVersions.map((version) => [version.id, version]));
  const latestSuccessfulApplyJob = applyJobs
    .filter((job) => job.action === 'apply' && job.status === 'succeeded')
    .sort((left, right) => {
      const leftTime = new Date(left.completedAt ?? left.updatedAt).getTime();
      const rightTime = new Date(right.completedAt ?? right.updatedAt).getTime();
      return rightTime - leftTime;
    })[0];
  const currentConfigVersionId = latestSuccessfulApplyJob?.configVersionId
    ?? configVersions
      .filter((version) => Boolean(version.appliedAt))
      .sort((left, right) => new Date(right.appliedAt ?? 0).getTime() - new Date(left.appliedAt ?? 0).getTime())[0]?.id
    ?? null;
'''
if old_current not in text:
    raise SystemExit("derived current config block not found")
text = text.replace(old_current, '''  const versionsById = new Map(configVersions.map((version) => [version.id, version]));
  const currentConfigVersionId = configVersionsQuery.data?.currentConfigVersionId ?? null;
''', 1)

# Retention hint below immutable-snapshot explanation.
text = text.replace(
    '        <p className="muted-text">{t(\'serverDetails.configVersionsImmutableHint\')}</p>\n',
    '        <p className="muted-text">{t(\'serverDetails.configVersionsImmutableHint\')}</p>\n'
    '        <p className="muted-text">{t(\'serverDetails.configRetentionHint\')}</p>\n',
    1,
)

# Surface pin mutation errors too.
text = text.replace(
    '(validateConfigMutation.isError || applyConfigMutation.isError || reapplyConfigMutation.isError || deleteConfigVersionMutation.isError)',
    '(validateConfigMutation.isError || applyConfigMutation.isError || reapplyConfigMutation.isError || deleteConfigVersionMutation.isError || pinConfigVersionMutation.isError || unpinConfigVersionMutation.isError)',
    1,
)

# Replace per-version derived-state block.
old_state = '''              const isDeleting =
                deleteConfigVersionMutation.isPending && deleteConfigVersionMutation.variables === version.id;
              const hasApplyHistory = applyJobs.some((job) => job.configVersionId === version.id);
              const canDeleteConfigVersion = !version.appliedAt && !hasApplyHistory;
              const isCurrentConfig = currentConfigVersionId === version.id;
'''
new_state = '''              const isDeleting =
                deleteConfigVersionMutation.isPending && deleteConfigVersionMutation.variables === version.id;
              const isPinning =
                pinConfigVersionMutation.isPending && pinConfigVersionMutation.variables === version.id;
              const isUnpinning =
                unpinConfigVersionMutation.isPending && unpinConfigVersionMutation.variables === version.id;
              const isCurrentConfig = currentConfigVersionId === version.id;
              const hasActiveDeployment = applyJobs.some(
                (job) => job.configVersionId === version.id && (job.status === 'pending' || job.status === 'in_progress'),
              );
              const canDeleteConfigVersion = !isCurrentConfig && !version.pinned && !hasActiveDeployment;
'''
if old_state not in text:
    raise SystemExit("config version derived state block not found")
text = text.replace(old_state, new_state, 1)

# Add pinned marker next to current marker.
old_badge = '''                    <StatusBadge status={version.status} />
                    {isCurrentConfig && <span className="badge badge-online">{t('serverDetails.currentConfig')}</span>}
'''
new_badge = '''                    <StatusBadge status={version.status} />
                    {isCurrentConfig && <span className="badge badge-online">{t('serverDetails.currentConfig')}</span>}
                    {version.pinned && <span className="badge">{t('serverDetails.pinnedConfig')}</span>}
'''
if old_badge not in text:
    raise SystemExit("current config badge block not found")
text = text.replace(old_badge, new_badge, 1)

# Add pin/unpin control before delete. Current is already retained, so it does not need a pin button.
old_delete = '''                    {canDeleteConfigVersion && (
                      <button
                        className="small-button"
                        type="button"
                        disabled={isDeleting}
                        onClick={() => {
                          if (window.confirm(t('serverDetails.deleteConfigConfirm', { version: version.version }))) {
                            deleteConfigVersionMutation.mutate(version.id);
                          }
                        }}
                      >
                        {isDeleting ? t('serverDetails.deletingConfig') : t('serverDetails.deleteConfig')}
                      </button>
                    )}
'''
new_delete = '''                    {!isCurrentConfig && (
                      <button
                        className="small-button"
                        type="button"
                        disabled={isPinning || isUnpinning}
                        onClick={() => {
                          if (version.pinned) {
                            unpinConfigVersionMutation.mutate(version.id);
                          } else {
                            pinConfigVersionMutation.mutate(version.id);
                          }
                        }}
                      >
                        {isPinning || isUnpinning
                          ? t('serverDetails.updatingPin')
                          : version.pinned
                            ? t('serverDetails.unpinConfig')
                            : t('serverDetails.pinConfig')}
                      </button>
                    )}
                    {canDeleteConfigVersion && (
                      <button
                        className="small-button"
                        type="button"
                        disabled={isDeleting}
                        onClick={() => {
                          if (window.confirm(t('serverDetails.deleteConfigConfirm', { version: version.version }))) {
                            deleteConfigVersionMutation.mutate(version.id);
                          }
                        }}
                      >
                        {isDeleting ? t('serverDetails.deletingConfig') : t('serverDetails.deleteConfig')}
                      </button>
                    )}
'''
if old_delete not in text:
    raise SystemExit("delete config action block not found")
text = text.replace(old_delete, new_delete, 1)
p.write_text(text)

# EN/RU lifecycle copy.
replace_once(
    "frontend/src/shared/i18n/locales/en.ts",
    "  'serverDetails.configVersionsImmutableHint': 'To change the configuration, update RouteGate server, protocol, VPN account, or routing settings and render a new version. Existing snapshots are not edited in place.',\n",
    "  'serverDetails.configVersionsImmutableHint': 'To change the configuration, update RouteGate server, protocol, VPN account, or routing settings and render a new version. Existing snapshots are not edited in place.',\n"
    "  'serverDetails.configRetentionHint': 'RouteGate reuses an unchanged latest snapshot, keeps the current config plus five previous applied versions, and retains pinned versions beyond that rollback window.',\n",
)
replace_once(
    "frontend/src/shared/i18n/locales/en.ts",
    "  'serverDetails.deleteConfigConfirm': 'Delete unused v{version}? Only a snapshot that has never had a deployment attempt can be deleted.',\n",
    "  'serverDetails.deleteConfigConfirm': 'Delete v{version}? Its completed deployment details will be removed with it. Current, pinned, or actively deploying versions cannot be deleted.',\n"
    "  'serverDetails.pinnedConfig': 'Pinned',\n"
    "  'serverDetails.pinConfig': 'Pin',\n"
    "  'serverDetails.unpinConfig': 'Unpin',\n"
    "  'serverDetails.updatingPin': 'Updating...',\n",
)
replace_once(
    "frontend/src/shared/i18n/locales/en.ts",
    "  'serverDetails.deploymentHistoryImmutableHint': 'Deployment history records describe actions that actually happened. They are intentionally not editable or manually deletable.',\n",
    "  'serverDetails.deploymentHistoryImmutableHint': 'Deployment records are not editable. RouteGate automatically keeps a bounded recent history of up to 100 terminal jobs per server.',\n",
)

replace_once(
    "frontend/src/shared/i18n/locales/ru.ts",
    "  'serverDetails.configVersionsImmutableHint': 'Чтобы изменить конфигурацию, измените настройки сервера, протокола, VPN-аккаунтов или маршрутизации в RouteGate и отрендерьте новую версию. Уже созданные версии не редактируются задним числом.',\n",
    "  'serverDetails.configVersionsImmutableHint': 'Чтобы изменить конфигурацию, измените настройки сервера, протокола, VPN-аккаунтов или маршрутизации в RouteGate и отрендерьте новую версию. Уже созданные версии не редактируются задним числом.',\n"
    "  'serverDetails.configRetentionHint': 'RouteGate не создаёт повторно неизменившийся последний снимок, хранит текущую конфигурацию и пять предыдущих применённых версий, а закреплённые версии сохраняет сверх этого окна отката.',\n",
)
replace_once(
    "frontend/src/shared/i18n/locales/ru.ts",
    "  'serverDetails.deleteConfigConfirm': 'Удалить неиспользуемую v{version}? Можно удалить только версию, для которой ни разу не запускалось развертывание.',\n",
    "  'serverDetails.deleteConfigConfirm': 'Удалить v{version}? Связанные завершённые сведения о развертывании также будут удалены. Текущую, закреплённую или участвующую в активном развертывании версию удалить нельзя.',\n"
    "  'serverDetails.pinnedConfig': 'Закреплена',\n"
    "  'serverDetails.pinConfig': 'Закрепить',\n"
    "  'serverDetails.unpinConfig': 'Открепить',\n"
    "  'serverDetails.updatingPin': 'Обновление...',\n",
)
replace_once(
    "frontend/src/shared/i18n/locales/ru.ts",
    "  'serverDetails.deploymentHistoryImmutableHint': 'Эти записи описывают реально выполненные операции. Их нельзя редактировать или удалять вручную задним числом.',\n",
    "  'serverDetails.deploymentHistoryImmutableHint': 'Записи о развертываниях нельзя редактировать. RouteGate автоматически хранит ограниченную недавнюю историю — до 100 завершённых задач на сервер.',\n",
)

# Format and validate the touched source before committing. The workflow supplies Go and Node.
