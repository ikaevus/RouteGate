package configs

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/audit"
	"github.com/ikaevus/routegate/backend/internal/auth"
	"github.com/ikaevus/routegate/backend/internal/httpx"
)

const (
	defaultApplyHistoryLimit = 20
	maxApplyHistoryLimit     = 100
)

type Handler struct {
	logger  *slog.Logger
	service *Service
	audit   *audit.Recorder
}

func NewHandler(logger *slog.Logger, pool *pgxpool.Pool) *Handler {
	repository := NewRepository(pool)
	return &Handler{
		logger:  logger,
		service: NewService(repository),
		audit:   audit.NewRecorder(logger, pool),
	}
}

func (h *Handler) Render(w http.ResponseWriter, r *http.Request) {
	var request RenderConfigRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil && !errors.Is(err, io.EOF) {
			writeInvalidRequest(w, "Request body must be valid JSON.")
			return
		}
	}

	response, err := h.service.Render(r.Context(), r.PathValue("server_id"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeServerNotFound(w)
		return
	}
	if errors.Is(err, ErrNodeRoleNoVPN) {
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("node_role_incompatible", "This node role does not host the VPN plane."))
		return
	}
	if err != nil {
		h.databaseError(w, "render config", err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, response)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
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
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	version, err := h.service.Get(r.Context(), r.PathValue("server_id"), r.PathValue("version_id"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeConfigVersionNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "get config version", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, version)
}

func (h *Handler) Validate(w http.ResponseWriter, r *http.Request) {
	response, err := h.service.Validate(r.Context(), r.PathValue("server_id"), r.PathValue("version_id"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeConfigVersionNotFound(w)
		return
	}
	if errors.Is(err, ErrInvalidRenderedConfig) {
		httpx.WriteJSON(w, http.StatusBadRequest, response)
		return
	}
	if err != nil {
		h.databaseError(w, "validate config version", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, response)
}

func (h *Handler) Apply(w http.ResponseWriter, r *http.Request) {
	serverID := r.PathValue("server_id")
	versionID := r.PathValue("version_id")
	var request ApplyConfigRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil && !errors.Is(err, io.EOF) {
			h.recordApplyRejected(r, serverID, versionID, "invalid_request")
			writeInvalidRequest(w, "Request body must be valid JSON.")
			return
		}
	}

	response, err := h.service.Apply(r.Context(), serverID, versionID, request)
	if errors.Is(err, pgx.ErrNoRows) {
		h.recordApplyRejected(r, serverID, versionID, "config_version_not_found")
		writeConfigVersionNotFound(w)
		return
	}
	if errors.Is(err, ErrConfigVersionNotValidated) {
		h.recordApplyRejected(r, serverID, versionID, "config_not_validated")
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("config_not_validated", "Config version must be validated before apply."))
		return
	}
	if errors.Is(err, ErrConfigApplyAgentMissing) {
		h.recordApplyRejected(r, serverID, versionID, "agent_missing")
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("agent_missing", "Server must have a registered agent before config apply."))
		return
	}
	if errors.Is(err, ErrNodeRoleNoVPN) {
		h.recordApplyRejected(r, serverID, versionID, "node_role_incompatible")
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("node_role_incompatible", "This node role does not host the VPN plane."))
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
		h.databaseError(w, "create config apply job", err)
		return
	}

	h.recordApplyRequested(r, response.Job)
	httpx.WriteJSON(w, http.StatusAccepted, response)
}

func (h *Handler) ListApplyJobs(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := parseApplyHistoryPagination(r)
	if err != nil {
		writeInvalidRequest(w, err.Error())
		return
	}

	items, total, err := h.service.ListApplyJobsPage(r.Context(), r.PathValue("server_id"), limit, offset)
	if err != nil {
		h.databaseError(w, "list config apply jobs", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ListConfigApplyJobsResponse{
		Items:  items,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	})
}

func (h *Handler) ClearCompletedApplyHistory(w http.ResponseWriter, r *http.Request) {
	serverID := r.PathValue("server_id")
	deleted, err := h.service.ClearCompletedApplyHistory(r.Context(), serverID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeServerNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "clear completed config apply jobs", err)
		return
	}

	h.recordAudit(r, audit.EventInput{
		Action:       "config.apply_history.cleared",
		ResourceType: "server",
		ResourceID:   serverID,
		Result:       audit.ResultSuccess,
		Metadata: map[string]any{
			"deleted_count": deleted,
			"scope":         "terminal_jobs_only",
		},
	})
	httpx.WriteJSON(w, http.StatusOK, ClearConfigApplyHistoryResponse{Deleted: deleted})
}

func (h *Handler) GetApplyJob(w http.ResponseWriter, r *http.Request) {
	job, err := h.service.GetApplyJob(r.Context(), r.PathValue("server_id"), r.PathValue("job_id"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeConfigApplyJobNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "get config apply job", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, job)
}

func parseApplyHistoryPagination(r *http.Request) (int, int, error) {
	limit := defaultApplyHistoryLimit
	offset := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > maxApplyHistoryLimit {
			return 0, 0, errors.New("limit must be an integer between 1 and 100")
		}
		limit = parsed
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			return 0, 0, errors.New("offset must be a non-negative integer")
		}
		offset = parsed
	}
	return limit, offset, nil
}

func (h *Handler) recordApplyRequested(r *http.Request, job ConfigApplyJob) {
	h.recordAudit(r, audit.EventInput{
		Action:       "config.apply.requested",
		ResourceType: "config_apply_job",
		ResourceID:   job.ID,
		Result:       audit.ResultSuccess,
		Metadata: map[string]any{
			"server_id":         job.ServerID,
			"agent_id":          job.AgentID,
			"config_version_id": job.ConfigVersionID,
			"job_id":            job.ID,
			"job_status":        job.Status,
		},
	})
}

func (h *Handler) recordApplyRejected(r *http.Request, serverID string, versionID string, reason string) {
	h.recordAudit(r, audit.EventInput{
		Action:       "config.apply.rejected",
		ResourceType: "config_version",
		ResourceID:   versionID,
		Result:       audit.ResultFailure,
		Metadata: map[string]any{
			"server_id":         serverID,
			"config_version_id": versionID,
			"reason":            reason,
		},
	})
}

func (h *Handler) recordAudit(r *http.Request, input audit.EventInput) {
	if user, ok := auth.UserFromContext(r.Context()); ok {
		input.ActorUserID = user.ID
		input.ActorType = audit.ActorTypeUser
	} else if input.ActorType == "" {
		input.ActorType = audit.ActorTypeSystem
	}
	h.audit.RecordSafe(r.Context(), input)
}

func (h *Handler) databaseError(w http.ResponseWriter, operation string, err error) {
	h.logger.Error(operation+" failed", "error", err)
	httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Database operation failed."))
}

func writeInvalidRequest(w http.ResponseWriter, message string) {
	httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_request", message))
}

func writeServerNotFound(w http.ResponseWriter) {
	httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("server_not_found", "Server not found."))
}

func writeConfigVersionNotFound(w http.ResponseWriter) {
	httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("config_version_not_found", "Config version not found."))
}

func writeConfigApplyJobNotFound(w http.ResponseWriter) {
	httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("config_apply_job_not_found", "Config apply job not found."))
}
