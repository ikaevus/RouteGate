package updates

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/audit"
	"github.com/ikaevus/routegate/backend/internal/auth"
	"github.com/ikaevus/routegate/backend/internal/buildinfo"
	"github.com/ikaevus/routegate/backend/internal/db"
	"github.com/ikaevus/routegate/backend/internal/httpx"
)

type jobRepository interface {
	CreatePreflight(context.Context, string) (Job, error)
	MarkRunning(context.Context, string) (Job, error)
	CompletePreflight(context.Context, string, PreflightResult) (Job, error)
	Fail(context.Context, string, string) (Job, error)
	Get(context.Context, string) (Job, error)
	List(context.Context, int) ([]Job, error)
}

type schemaVersionReader interface {
	AppliedSchemaVersion(context.Context) (string, error)
}

type auditRecorder interface {
	RecordSafe(context.Context, audit.EventInput)
}

type Handler struct {
	logger *slog.Logger
	repo   jobRepository
	reader schemaVersionReader
	audit  auditRecorder
	info   func() buildinfo.Info
}

func NewHandler(logger *slog.Logger, pool *pgxpool.Pool) *Handler {
	return &Handler{
		logger: logger,
		repo:   NewRepository(pool),
		reader: db.NewSchemaVersionRepository(pool),
		audit:  audit.NewRecorder(logger, pool),
		info:   buildinfo.Current,
	}
}

func NewHandlerWithDependencies(logger *slog.Logger, repo jobRepository, reader schemaVersionReader, recorder auditRecorder, info func() buildinfo.Info) *Handler {
	return &Handler{logger: logger, repo: repo, reader: reader, audit: recorder, info: info}
}

func (h *Handler) CreatePreflight(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		httpx.WriteJSON(w, http.StatusUnauthorized, httpx.Error("unauthorized", "Authentication is required."))
		return
	}

	job, err := h.repo.CreatePreflight(r.Context(), user.ID)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("update_preflight_in_progress", "Another update preflight is already pending or running."))
		return
	}
	if err != nil {
		h.logError("create update preflight job failed", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Failed to create update preflight job."))
		return
	}

	h.record(r.Context(), user.ID, "update.preflight.requested", job, audit.ResultSuccess, nil)

	job, err = h.repo.MarkRunning(r.Context(), job.ID)
	if err != nil {
		h.failJob(r.Context(), user.ID, job, "job_state_transition_failed")
		h.logError("start update preflight job failed", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Failed to start update preflight job."))
		return
	}

	appliedMigration, err := h.reader.AppliedSchemaVersion(r.Context())
	if err != nil {
		h.failJob(r.Context(), user.ID, job, "database_preflight_failed")
		h.logError("read database schema during update preflight failed", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Update preflight could not read the database schema state."))
		return
	}

	result := EvaluatePreflight(h.info(), appliedMigration)
	job, err = h.repo.CompletePreflight(r.Context(), job.ID, result)
	if err != nil {
		h.failJob(r.Context(), user.ID, job, "job_completion_failed")
		h.logError("complete update preflight job failed", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Failed to complete update preflight job."))
		return
	}

	h.record(r.Context(), user.ID, "update.preflight.completed", job, audit.ResultSuccess, map[string]any{
		"decision": result.Decision,
		"blockers": result.Blockers,
	})
	httpx.WriteJSON(w, http.StatusAccepted, CreateResponse{Job: job})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	job, err := h.repo.Get(r.Context(), strings.TrimSpace(r.PathValue("job_id")))
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("update_job_not_found", "Update job was not found."))
		return
	}
	if err != nil {
		h.logError("read update job failed", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Failed to read update job."))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, job)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_limit", "limit must be between 1 and 100."))
			return
		}
		limit = parsed
	}

	items, err := h.repo.List(r.Context(), limit)
	if err != nil {
		h.logError("list update jobs failed", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Failed to list update jobs."))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ListResponse{Items: items})
}

func (h *Handler) failJob(ctx context.Context, userID string, job Job, errorCode string) {
	failed, err := h.repo.Fail(ctx, job.ID, errorCode)
	if err != nil {
		h.logError("mark update preflight failed", err)
		return
	}
	h.record(ctx, userID, "update.preflight.failed", failed, audit.ResultFailure, map[string]any{"error_code": errorCode})
}

func (h *Handler) record(ctx context.Context, userID, action string, job Job, result string, metadata map[string]any) {
	if h.audit == nil {
		return
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["operation"] = job.Operation
	metadata["stage"] = job.Stage
	metadata["status"] = job.Status
	h.audit.RecordSafe(ctx, audit.EventInput{
		ActorUserID:  userID,
		ActorType:    audit.ActorTypeUser,
		Action:       action,
		ResourceType: "update_job",
		ResourceID:   job.ID,
		Result:       result,
		Metadata:     metadata,
	})
}

func (h *Handler) logError(message string, err error) {
	if h.logger != nil {
		h.logger.Error(message, "error", err)
	}
}
