package updates

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/audit"
	"github.com/ikaevus/routegate/backend/internal/auth"
	"github.com/ikaevus/routegate/backend/internal/buildinfo"
	"github.com/ikaevus/routegate/backend/internal/db"
	"github.com/ikaevus/routegate/backend/internal/httpx"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

const (
	updateJobLifecycleTimeout      = 10 * time.Second
	failurePersistTimeout          = 5 * time.Second
	preflightInsertAmbiguousCode   = "preflight_insert_ambiguous"
	discoveryInsertAmbiguousCode   = "discovery_insert_ambiguous"
	discoveryExternalFailureCode   = "release_discovery_failed"
	discoveryStateTransitionCode   = "discovery_state_transition_failed"
	discoveryCompletionFailureCode = "discovery_completion_failed"
	maxDiscoveryRequestBodyBytes   = 1
)

type jobRepository interface {
	CreatePreflight(context.Context, string) (Job, error)
	MarkRunning(context.Context, string) (Job, error)
	CompletePreflight(context.Context, string, PreflightResult) (Job, error)
	Fail(context.Context, string, string) (Job, error)
	Get(context.Context, string) (Job, error)
	List(context.Context, int) ([]Job, error)
}

type discoveryJobRepository interface {
	CreateDiscovery(context.Context, string) (Job, error)
	MarkRunning(context.Context, string) (Job, error)
	CompleteDiscovery(context.Context, string, DiscoveryResult) (Job, error)
	Fail(context.Context, string, string) (Job, error)
}

type jobFailureRepository interface {
	Fail(context.Context, string, string) (Job, error)
}

type schemaVersionReader interface {
	AppliedSchemaVersion(context.Context) (string, error)
}

type auditRecorder interface {
	RecordSafe(context.Context, audit.EventInput)
}

type Handler struct {
	logger        *slog.Logger
	repo          jobRepository
	discoveryRepo discoveryJobRepository
	reader        schemaVersionReader
	audit         auditRecorder
	info          func() buildinfo.Info
	discoverer    releaseDiscoverer
	runtimeOS     string
	runtimeArch   string
}

func NewHandler(logger *slog.Logger, pool *pgxpool.Pool) *Handler {
	repo := NewRepository(pool)
	return &Handler{
		logger:        logger,
		repo:          repo,
		discoveryRepo: repo,
		reader:        db.NewSchemaVersionRepository(pool),
		audit:         audit.NewRecorder(logger, pool),
		info:          buildinfo.Current,
		discoverer:    NewOfficialReleaseDiscoverer(nil),
		runtimeOS:     runtime.GOOS,
		runtimeArch:   runtime.GOARCH,
	}
}

func NewHandlerWithDependencies(logger *slog.Logger, repo jobRepository, reader schemaVersionReader, recorder auditRecorder, info func() buildinfo.Info) *Handler {
	handler := &Handler{
		logger:      logger,
		repo:        repo,
		reader:      reader,
		audit:       recorder,
		info:        info,
		discoverer:  NewOfficialReleaseDiscoverer(nil),
		runtimeOS:   runtime.GOOS,
		runtimeArch: runtime.GOARCH,
	}
	if discoveryRepo, ok := repo.(discoveryJobRepository); ok {
		handler.discoveryRepo = discoveryRepo
	}
	return handler
}

func (h *Handler) CreatePreflight(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		httpx.WriteJSON(w, http.StatusUnauthorized, httpx.Error("unauthorized", "Authentication is required."))
		return
	}

	lifecycleCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), updateJobLifecycleTimeout)
	defer cancel()

	job, err := h.repo.CreatePreflight(lifecycleCtx, user.ID)
	if err != nil {
		h.reconcileAmbiguousCreate(lifecycleCtx, h.repo, user.ID, job, preflightInsertAmbiguousCode, "update.preflight.failed")
		h.logError("create update preflight job failed", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Failed to create update preflight job."))
		return
	}

	h.record(lifecycleCtx, user.ID, "update.preflight.requested", job, audit.ResultSuccess, nil)

	job, err = h.repo.MarkRunning(lifecycleCtx, job.ID)
	if err != nil {
		h.failJob(lifecycleCtx, h.repo, user.ID, job, "job_state_transition_failed", "update.preflight.failed")
		h.logError("start update preflight job failed", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Failed to start update preflight job."))
		return
	}

	appliedMigration, err := h.reader.AppliedSchemaVersion(lifecycleCtx)
	if err != nil {
		h.failJob(lifecycleCtx, h.repo, user.ID, job, "database_preflight_failed", "update.preflight.failed")
		h.logError("read database schema during update preflight failed", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Update preflight could not read the database schema state."))
		return
	}

	result := EvaluatePreflight(h.info(), appliedMigration)
	job, err = h.repo.CompletePreflight(lifecycleCtx, job.ID, result)
	if err != nil {
		h.failJob(lifecycleCtx, h.repo, user.ID, job, "job_completion_failed", "update.preflight.failed")
		h.logError("complete update preflight job failed", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Failed to complete update preflight job."))
		return
	}

	h.record(lifecycleCtx, user.ID, "update.preflight.completed", job, audit.ResultSuccess, map[string]any{
		"decision": result.Decision,
		"blockers": result.Blockers,
	})
	httpx.WriteJSON(w, http.StatusAccepted, CreateResponse{Job: job})
}

func (h *Handler) CreateDiscovery(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		httpx.WriteJSON(w, http.StatusUnauthorized, httpx.Error("unauthorized", "Authentication is required."))
		return
	}
	if hasDiscoveryRequestBody(r) {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_update_discovery_request", "Update discovery does not accept request parameters."))
		return
	}

	if h.discoveryRepo == nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("update_discovery_unavailable", "Update discovery is unavailable."))
		return
	}

	lifecycleCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), updateJobLifecycleTimeout)
	defer cancel()

	job, err := h.discoveryRepo.CreateDiscovery(lifecycleCtx, user.ID)
	if err != nil {
		h.reconcileAmbiguousCreate(lifecycleCtx, h.discoveryRepo, user.ID, job, discoveryInsertAmbiguousCode, "update.discovery.failed")
		h.logError("create update discovery job failed", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Failed to create update discovery job."))
		return
	}
	h.record(lifecycleCtx, user.ID, "update.discovery.requested", job, audit.ResultSuccess, nil)

	job, err = h.discoveryRepo.MarkRunning(lifecycleCtx, job.ID)
	if err != nil {
		h.failJob(lifecycleCtx, h.discoveryRepo, user.ID, job, discoveryStateTransitionCode, "update.discovery.failed")
		h.logError("start update discovery job failed", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Failed to start update discovery job."))
		return
	}

	result, err := h.discoverer.Discover(lifecycleCtx, h.info().Version, h.runtimeOS, h.runtimeArch)
	if err != nil {
		h.failJob(lifecycleCtx, h.discoveryRepo, user.ID, job, discoveryExternalFailureCode, "update.discovery.failed")
		h.logError("official release discovery failed", err)
		httpx.WriteJSON(w, http.StatusBadGateway, httpx.Error(discoveryExternalFailureCode, "Official RouteGate release discovery failed."))
		return
	}

	job, err = h.discoveryRepo.CompleteDiscovery(lifecycleCtx, job.ID, result)
	if err != nil {
		h.failJob(lifecycleCtx, h.discoveryRepo, user.ID, job, discoveryCompletionFailureCode, "update.discovery.failed")
		h.logError("complete update discovery job failed", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Failed to complete update discovery job."))
		return
	}

	h.record(lifecycleCtx, user.ID, "update.discovery.completed", job, audit.ResultSuccess, map[string]any{
		"availability":        result.Availability,
		"candidate_version":   result.CandidateVersion,
		"runtime_os":          result.RuntimeOS,
		"runtime_arch":        result.RuntimeArch,
		"missing_asset_count": len(result.MissingAssets),
	})
	httpx.WriteJSON(w, http.StatusAccepted, CreateResponse{Job: job})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	jobID := strings.TrimSpace(r.PathValue("job_id"))
	if !uuidPattern.MatchString(jobID) {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_update_job_id", "Update job ID must be a UUID."))
		return
	}

	job, err := h.repo.Get(r.Context(), jobID)
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

func hasDiscoveryRequestBody(r *http.Request) bool {
	if r.Body == nil {
		return false
	}
	payload, err := io.ReadAll(io.LimitReader(r.Body, maxDiscoveryRequestBodyBytes))
	return err != nil || len(payload) != 0
}

func (h *Handler) reconcileAmbiguousCreate(ctx context.Context, repo jobFailureRepository, userID string, job Job, errorCode, auditAction string) {
	if !uuidPattern.MatchString(job.ID) {
		return
	}

	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), failurePersistTimeout)
	defer cancel()

	failed, err := repo.Fail(persistCtx, job.ID, errorCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return
	}
	if err != nil {
		h.logError("reconcile ambiguous update job insert failed", err)
		return
	}
	h.record(persistCtx, userID, auditAction, failed, audit.ResultFailure, map[string]any{"error_code": errorCode})
}

func (h *Handler) failJob(ctx context.Context, repo jobFailureRepository, userID string, job Job, errorCode, auditAction string) {
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), failurePersistTimeout)
	defer cancel()

	failed, err := repo.Fail(persistCtx, job.ID, errorCode)
	if err != nil {
		h.logError("mark update job failed", err)
		return
	}
	h.record(persistCtx, userID, auditAction, failed, audit.ResultFailure, map[string]any{"error_code": errorCode})
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
