package updates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/audit"
	"github.com/ikaevus/routegate/backend/internal/auth"
	"github.com/ikaevus/routegate/backend/internal/httpx"
)

const (
	trustedUpdateDispatchSocket = "/run/routegate/update-dispatch.sock"
	applyJobLifecycleTimeout     = 30 * time.Minute
	applyFailurePersistTimeout   = 5 * time.Second
	maxApplyRequestBodyBytes     = 1024

	applyInsertAmbiguousCode   = "apply_insert_ambiguous"
	applyStateTransitionCode   = "apply_state_transition_failed"
	applyStagePinFailureCode   = "apply_stage_pin_failed"
	applyDispatchFailureCode   = "privileged_apply_failed"
	applyResultPersistenceCode = "apply_result_persistence_failed"
)

var (
	canonicalUUIDv4Pattern    = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	errDispatchRejected       = errors.New("privileged dispatcher rejected apply")
	errDispatchOutcomeUnknown = errors.New("privileged apply outcome is unknown")
)

type applyJobRepository interface {
	CreateApply(context.Context, string, string) (Job, error)
	MarkRunning(context.Context, string) (Job, error)
	CompleteApply(context.Context, string, ApplyResult) (Job, error)
	Fail(context.Context, string, string) (Job, error)
	Get(context.Context, string) (Job, error)
}

type applyStageLockRepository interface {
	AcquireStageAdmissionLock(context.Context) (func(), error)
}

type privilegedApplyDispatcher interface {
	Apply(context.Context, string) error
}

type unixApplyDispatcher struct {
	socketPath string
}

type ApplyHandler struct {
	logger     *slog.Logger
	repo       applyJobRepository
	audit      auditRecorder
	dispatcher privilegedApplyDispatcher
	pinner     stageApplyPinner
}

func NewApplyHandler(logger *slog.Logger, pool *pgxpool.Pool) *ApplyHandler {
	return &ApplyHandler{
		logger:     logger,
		repo:       NewRepository(pool),
		audit:      audit.NewRecorder(logger, pool),
		dispatcher: unixApplyDispatcher{socketPath: trustedUpdateDispatchSocket},
		pinner:     newStageApplyPinner(managerUpdateStagingRoot),
	}
}

func newApplyHandlerWithDependencies(logger *slog.Logger, repo applyJobRepository, recorder auditRecorder, dispatcher privilegedApplyDispatcher) *ApplyHandler {
	return &ApplyHandler{logger: logger, repo: repo, audit: recorder, dispatcher: dispatcher}
}

func (h *ApplyHandler) Create(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		httpx.WriteJSON(w, http.StatusUnauthorized, httpx.Error("unauthorized", "Authentication is required."))
		return
	}

	request, err := decodeApplyRequest(r)
	if err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_request", "A valid stageJobId is required."))
		return
	}

	executionCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), applyJobLifecycleTimeout)
	defer cancel()

	var releaseStageLock func()
	if locker, ok := h.repo.(applyStageLockRepository); ok {
		releaseStageLock, err = locker.AcquireStageAdmissionLock(executionCtx)
		if err != nil {
			h.logError("acquire update stage retention lock for apply", err)
			httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("apply_admission_failed", "Unable to reserve the staged candidate for apply."))
			return
		}
		defer releaseStageLock()
	}

	stageJob, err := h.repo.Get(executionCtx, request.StageJobID)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("stage_job_not_found", "The stage job was not found."))
		return
	}
	if err != nil {
		h.logError("load stage job for apply", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("update_job_lookup_failed", "Unable to load the stage job."))
		return
	}

	stageResult, err := applicableStageResult(stageJob)
	if err != nil {
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("stage_job_not_applicable", "The stage job does not describe an applicable verified update."))
		return
	}

	job, err := h.repo.CreateApply(executionCtx, user.ID, request.StageJobID)
	if errors.Is(err, ErrApplyInProgress) {
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("apply_in_progress", "Another update apply job is already pending or running."))
		return
	}
	if err != nil {
		if job.ID != "" {
			h.reconcileAmbiguousCreate(job, user.ID)
		}
		h.logError("create update apply job", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("update_job_create_failed", "Unable to create the apply job."))
		return
	}
	h.record(executionCtx, user.ID, "update.apply.requested", job, audit.ResultSuccess)

	job, err = h.repo.MarkRunning(executionCtx, job.ID)
	if err != nil {
		h.failJob(job.ID, applyStateTransitionCode, user.ID)
		h.logError("mark update apply job running", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error(applyStateTransitionCode, "Unable to start the update apply job."))
		return
	}

	var releasePin func() error
	keepPin := false
	if h.pinner != nil {
		releasePin, err = h.pinner.Pin(request.StageJobID)
		if err != nil {
			h.failJob(job.ID, applyStagePinFailureCode, user.ID)
			h.logError("pin staged candidate for privileged apply", err)
			httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error(applyStagePinFailureCode, "Unable to pin the staged candidate for privileged apply."))
			return
		}
		defer func() {
			if releasePin != nil && !keepPin {
				if err := releasePin(); err != nil {
					h.logError("release staged candidate apply pin", err)
				}
			}
		}()
	}

	if h.dispatcher == nil {
		h.failJob(job.ID, applyDispatchFailureCode, user.ID)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("update_apply_unavailable", "Privileged update apply is unavailable."))
		return
	}
	if err := h.dispatcher.Apply(executionCtx, request.StageJobID); err != nil {
		if errors.Is(err, errDispatchOutcomeUnknown) {
			keepPin = true
			h.failJob(job.ID, ErrorCodeApplyOutcomeUnknown, user.ID)
			h.logError("privileged update apply outcome became unknown", err)
			httpx.WriteJSON(w, http.StatusBadGateway, httpx.Error(ErrorCodeApplyOutcomeUnknown, "The privileged transaction may have continued after the Manager lost its bounded result. It will not be retried automatically."))
			return
		}
		h.failJob(job.ID, applyDispatchFailureCode, user.ID)
		h.logError("dispatch privileged update apply", err)
		httpx.WriteJSON(w, http.StatusBadGateway, httpx.Error(applyDispatchFailureCode, "The privileged update transaction failed before a successful result was returned."))
		return
	}

	result := ApplyResult{
		StageJobID:        request.StageJobID,
		CandidateVersion:  stageResult.CandidateVersion,
		VerifiedVersion:   stageResult.VerifiedVersion,
		VerifiedCommit:    stageResult.VerifiedCommit,
		ExpectedMigration: stageResult.ExpectedMigration,
		RuntimeOS:         stageResult.RuntimeOS,
		RuntimeArch:       stageResult.RuntimeArch,
		Artifact:          stageResult.Artifact,
		ProvenanceStatus:  stageResult.ProvenanceStatus,
		Verification:      stageResult.Verification,
	}
	job, err = h.repo.CompleteApply(executionCtx, job.ID, result)
	if err != nil {
		if reconciled, committed := h.reconcileApplyCompletion(job.ID, result); committed {
			job = reconciled
			h.record(context.Background(), user.ID, "update.apply.completed", job, audit.ResultSuccess)
			httpx.WriteJSON(w, http.StatusCreated, CreateResponse{Job: job})
			return
		}
		h.failJob(job.ID, applyResultPersistenceCode, user.ID)
		h.logError("complete update apply job", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error(applyResultPersistenceCode, "The host update returned success, but its durable Manager result could not be confirmed."))
		return
	}
	h.record(executionCtx, user.ID, "update.apply.completed", job, audit.ResultSuccess)
	httpx.WriteJSON(w, http.StatusCreated, CreateResponse{Job: job})
}

func decodeApplyRequest(r *http.Request) (ApplyRequest, error) {
	limited := io.LimitReader(r.Body, maxApplyRequestBodyBytes+1)
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()
	var request ApplyRequest
	if err := decoder.Decode(&request); err != nil {
		return ApplyRequest{}, err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return ApplyRequest{}, err
	}
	if !canonicalUUIDv4Pattern.MatchString(request.StageJobID) {
		return ApplyRequest{}, errors.New("stageJobId is not a canonical UUIDv4")
	}
	return request, nil
}

func applicableStageResult(job Job) (StageResult, error) {
	if job.Operation != OperationStage || job.Stage != StageStage || job.Status != StatusSucceeded {
		return StageResult{}, errors.New("stage job is not successful")
	}
	var retention struct {
		Retention string `json:"retention"`
	}
	if err := json.Unmarshal(job.ResultPayload, &retention); err != nil {
		return StageResult{}, err
	}
	if retention.Retention == stageRetentionEvicting || retention.Retention == stageRetentionEvicted {
		return StageResult{}, errors.New("stage candidate is not retained")
	}
	var result StageResult
	if err := json.Unmarshal(job.ResultPayload, &result); err != nil {
		return StageResult{}, err
	}
	if !canonicalUUIDv4Pattern.MatchString(job.ID) || !canonicalUUIDv4Pattern.MatchString(result.DiscoveryJobID) {
		return StageResult{}, errors.New("stage result contains a non-canonical UUID")
	}
	if result.ProvenanceStatus != ProvenanceVerified || result.Verification != VerificationRG96C3A {
		return StageResult{}, errors.New("stage verification contract is not canonical")
	}
	if !releaseTagPattern.MatchString(result.CandidateVersion) || result.VerifiedVersion != result.CandidateVersion {
		return StageResult{}, errors.New("stage version contract is invalid")
	}
	if !verifiedCommitPattern.MatchString(result.VerifiedCommit) || !verifiedMigrationPattern.MatchString(result.ExpectedMigration) {
		return StageResult{}, errors.New("stage verified descriptor is invalid")
	}
	if result.RuntimeOS != "linux" || (result.RuntimeArch != "amd64" && result.RuntimeArch != "arm64") {
		return StageResult{}, errors.New("stage target platform is unsupported")
	}
	if result.Artifact.OS != result.RuntimeOS || result.Artifact.Arch != result.RuntimeArch || !verifiedSHA256Pattern.MatchString(result.Artifact.SHA256) || result.Artifact.Size <= 0 {
		return StageResult{}, errors.New("stage artifact descriptor is invalid")
	}
	return result, nil
}

func (d unixApplyDispatcher) Apply(ctx context.Context, stageJobID string) error {
	if d.socketPath != trustedUpdateDispatchSocket {
		return errors.New("dispatch socket path is not the fixed RouteGate socket")
	}
	if !canonicalUUIDv4Pattern.MatchString(stageJobID) {
		return errors.New("stage job ID is not a canonical UUIDv4")
	}

	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", d.socketPath)
	if err != nil {
		return err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return err
		}
	}

	request := stageJobID + "\n"
	n, writeErr := io.WriteString(conn, request)
	if n != len(request) {
		if writeErr != nil {
			return fmt.Errorf("write dispatch request: %w", writeErr)
		}
		return io.ErrShortWrite
	}
	if writeErr != nil {
		return fmt.Errorf("%w: request was fully sent before write error: %v", errDispatchOutcomeUnknown, writeErr)
	}
	if unixConn, ok := conn.(*net.UnixConn); ok {
		if err := unixConn.CloseWrite(); err != nil {
			return fmt.Errorf("%w: close dispatch request stream: %v", errDispatchOutcomeUnknown, err)
		}
	}

	response, err := io.ReadAll(io.LimitReader(conn, 5))
	if err != nil {
		return fmt.Errorf("%w: read dispatch result: %v", errDispatchOutcomeUnknown, err)
	}
	switch string(response) {
	case "OK\n":
		return nil
	case "ERR\n":
		return errDispatchRejected
	default:
		return fmt.Errorf("%w: invalid bounded dispatch response", errDispatchOutcomeUnknown)
	}
}

func (h *ApplyHandler) reconcileAmbiguousCreate(job Job, userID string) {
	ctx, cancel := context.WithTimeout(context.Background(), applyFailurePersistTimeout)
	defer cancel()
	failed, err := h.repo.Fail(ctx, job.ID, applyInsertAmbiguousCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return
	}
	if err != nil {
		h.logError("reconcile ambiguous update apply insert", err)
		return
	}
	h.record(ctx, userID, "update.apply.failed", failed, audit.ResultFailure)
}

func (h *ApplyHandler) reconcileApplyCompletion(jobID string, expected ApplyResult) (Job, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), applyFailurePersistTimeout)
	defer cancel()
	persisted, err := h.repo.Get(ctx, jobID)
	if err != nil || persisted.Operation != OperationApply || persisted.Stage != StageApply || persisted.Status != StatusSucceeded {
		return Job{}, false
	}
	var actual ApplyResult
	if err := json.Unmarshal(persisted.ResultPayload, &actual); err != nil || actual != expected {
		return Job{}, false
	}
	return persisted, true
}

func (h *ApplyHandler) failJob(jobID, errorCode, userID string) {
	ctx, cancel := context.WithTimeout(context.Background(), applyFailurePersistTimeout)
	defer cancel()
	failed, err := h.repo.Fail(ctx, jobID, errorCode)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			h.logError("persist failed update apply job", err)
		}
		return
	}
	h.record(ctx, userID, "update.apply.failed", failed, audit.ResultFailure)
}

func (h *ApplyHandler) record(ctx context.Context, userID, action string, job Job, result string) {
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
			"operation":  job.Operation,
			"stage":      job.Stage,
			"status":     job.Status,
			"error_code": job.ErrorCode,
		},
	})
}

func (h *ApplyHandler) logError(message string, err error) {
	if h.logger != nil {
		h.logger.Error(message, "error", err)
	}
}
