package configs

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/httpx"
)

type Handler struct {
	logger  *slog.Logger
	service *Service
}

func NewHandler(logger *slog.Logger, pool *pgxpool.Pool) *Handler {
	repository := NewRepository(pool)
	return &Handler{
		logger:  logger,
		service: NewService(repository),
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
	if err != nil {
		h.databaseError(w, "render config", err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, response)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.List(r.Context(), r.PathValue("server_id"))
	if err != nil {
		h.databaseError(w, "list config versions", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ListConfigVersionsResponse{Items: items})
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
	var request ApplyConfigRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil && !errors.Is(err, io.EOF) {
			writeInvalidRequest(w, "Request body must be valid JSON.")
			return
		}
	}

	response, err := h.service.Apply(r.Context(), r.PathValue("server_id"), r.PathValue("version_id"), request)
	if errors.Is(err, pgx.ErrNoRows) {
		writeConfigVersionNotFound(w)
		return
	}
	if errors.Is(err, ErrConfigVersionNotValidated) {
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("config_not_validated", "Config version must be validated before apply."))
		return
	}
	if errors.Is(err, ErrConfigApplyAgentMissing) {
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("agent_missing", "Server must have a registered agent before config apply."))
		return
	}
	if err != nil {
		h.databaseError(w, "create config apply job", err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, response)
}

func (h *Handler) ListApplyJobs(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.ListApplyJobs(r.Context(), r.PathValue("server_id"))
	if err != nil {
		h.databaseError(w, "list config apply jobs", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ListConfigApplyJobsResponse{Items: items})
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
