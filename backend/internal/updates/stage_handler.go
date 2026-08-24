package updates

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/audit"
	"github.com/ikaevus/routegate/backend/internal/auth"
	"github.com/ikaevus/routegate/backend/internal/httpx"
)

const (
	stageJobLifecycleTimeout      = 10 * time.Minute
	stageFailurePersistTimeout    = 5 * time.Second
	stageInsertAmbiguousCode      = "stage_insert_ambiguous"
	stageExecutionFailureCode     = "artifact_staging_failed"
	stageStateTransitionCode      = "stage_state_transition_failed"
	stageCompletionFailureCode    = "stage_completion_failed"
	maxStageRequestBodyBytes      = 1024
)

type stageJobRepository interface {
	CreateStage(context.Context, string, string) (Job, error)
	MarkRunning(context.Context, string) (Job, error)
	CompleteStage(context.Context, string, StageResult) (Job, error)
	Fail(context.Context, string, string) (Job, error)
	Get(context.Context, string) (Job, error)
}

type StageHandler struct {
	logger *slog.Logger
	repo   stageJobRepository
	audit  auditRecorder
	stager artifactStager
}

func NewStageHandler(logger *slog.Logger, pool *pgxpool.Pool) *StageHandler {
	return &StageHandler{
		logger: logger,
		repo:   NewRepository(pool),
		audit:  audit.NewRecorder(logger, pool),
		stager: newReleaseArtifactStager(),
	}
}

func newStageHandlerWithDependencies(logger *slog.Logger, repo stageJobRepository, recorder auditRecorder, stager artifactStager) *StageHandler {
	return &StageHandler{logger: logger, repo: repo, audit: recorder, stager: stager}
}

func (h *StageHandler) Create(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		httpx.WriteJSON(w, http.StatusUnauthorized, httpx.Error("unauthorized", "Authentication is required."))
		return
	}

	request, err := decodeStageRequest(r)
	if err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_request", "A valid discoveryJobId is required."))
		return
	}

	lifecycleCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), stageJobLifecycleTimeout)
	defer cancel()

	discoveryJob, err := h.repo.Get(lifecycleCtx, request.DiscoveryJobID)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("discovery_job_not_found", "The discovery job was not found."))
		return
	}
	if err != nil {
		h.logError("load discovery job for staging", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("update_job_lookup_failed", "Unable to load the discovery job."))
		return
	}

	discovery, err := stageableDiscoveryResult(discoveryJob)
	if err != nil {
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("discovery_job_not_stageable", "The discovery job does not describe a stageable update."))
		return
	}

	job, err := h.repo.CreateStage(lifecycleCtx, user.ID, request.DiscoveryJobID)
	if err != nil {
		if job.ID != "" {
			h.reconcileAmbiguousCreate(job, user.ID)
		}
		h.logError("create update stage job", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("update_job_create_failed", "Unable to create the stage job."))
		return
	}
	h.record(lifecycleCtx, user.ID, "update.stage.requested", job, audit.ResultSuccess)

	job, err = h.repo.MarkRunning(lifecycleCtx, job.ID)
	if err != nil {
		h.failAndCleanup(job.ID, stageStateTransitionCode, user.ID)
		h.logError("mark update stage job running", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error(stageStateTransitionCode, "Unable to start artifact staging."))
		return
	}

	result, err := h.stager.StageAndVerify(lifecycleCtx, job.ID, discovery)
	if err != nil {
		h.failAndCleanup(job.ID, stageExecutionFailureCode, user.ID)
		h.logError("stage and verify update artifacts", err)
		httpx.WriteJSON(w, http.StatusBadGateway, httpx.Error(stageExecutionFailureCode, "Unable to stage and verify the release artifacts."))
		return
	}
	result.DiscoveryJobID = request.DiscoveryJobID

	job, err = h.repo.CompleteStage(lifecycleCtx, job.ID, result)
	if err != nil {
		h.failAndCleanup(job.ID, stageCompletionFailureCode, user.ID)
		h.logError("complete update stage job", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error(stageCompletionFailureCode, "Unable to persist the verified staged candidate."))
		return
	}
	h.record(lifecycleCtx, user.ID, "update.stage.completed", job, audit.ResultSuccess)
	httpx.WriteJSON(w, http.StatusCreated, CreateResponse{Job: job})
}

func decodeStageRequest(r *http.Request) (StageRequest, error) {
	limited := io.LimitReader(r.Body, maxStageRequestBodyBytes+1)
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()
	var request StageRequest
	if err := decoder.Decode(&request); err != nil {
		return StageRequest{}, err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return StageRequest{}, err
	}
	if !uuidPattern.MatchString(request.DiscoveryJobID) {
		return StageRequest{}, errors.New("discoveryJobId is not a UUID")
	}
	return request, nil
}

func stageableDiscoveryResult(job Job) (DiscoveryResult, error) {
	if job.Operation != OperationDiscovery || job.Stage != StageDiscovery || job.Status != StatusSucceeded {
		return DiscoveryResult{}, errors.New("discovery job is not successful")
	}
	var result DiscoveryResult
	if err := json.Unmarshal(job.ResultPayload, &result); err != nil {
		return DiscoveryResult{}, err
	}
	if _, _, err := validateDiscoveryForStage(result); err != nil {
		return DiscoveryResult{}, err
	}
	return result, nil
}

func (h *StageHandler) reconcileAmbiguousCreate(job Job, userID string) {
	ctx, cancel := context.WithTimeout(context.Background(), stageFailurePersistTimeout)
	defer cancel()
	failed, err := h.repo.Fail(ctx, job.ID, stageInsertAmbiguousCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return
	}
	if err != nil {
		h.logError("reconcile ambiguous update stage insert", err)
		return
	}
	h.record(ctx, userID, "update.stage.failed", failed, audit.ResultFailure)
}

func (h *StageHandler) failAndCleanup(jobID, errorCode, userID string) {
	if h.stager != nil {
		if err := h.stager.Cleanup(jobID); err != nil {
			h.logError("clean failed update staging directory", err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), stageFailurePersistTimeout)
	defer cancel()
	failed, err := h.repo.Fail(ctx, jobID, errorCode)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			h.logError("persist failed update stage job", err)
		}
		return
	}
	h.record(ctx, userID, "update.stage.failed", failed, audit.ResultFailure)
}

func (h *StageHandler) record(ctx context.Context, userID, action string, job Job, result string) {
	if h.audit == nil {
		return
	}
	h.audit.RecordSafe(ctx, audit.EventInput{
		ActorType:    audit.ActorTypeUser,
		ActorUserID:  userID,
		Action:       action,
		ResourceType: "update_job",
		ResourceID:   job.ID,
		Result:       result,
		Metadata: map[string]any{
			"operation": job.Operation,
			"stage":     job.Stage,
			"status":    job.Status,
			"error_code": job.ErrorCode,
		},
	})
}

func (h *StageHandler) logError(message string, err error) {
	if h.logger != nil {
		h.logger.Error(message, "error", err)
	}
}
