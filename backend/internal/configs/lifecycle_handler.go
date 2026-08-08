package configs

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
		Metadata:     map[string]any{"server_id": serverID},
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
