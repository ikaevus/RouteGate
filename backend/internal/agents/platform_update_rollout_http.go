package agents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/ikaevus/routegate/backend/internal/audit"
	"github.com/ikaevus/routegate/backend/internal/buildinfo"
	"github.com/ikaevus/routegate/backend/internal/httpx"
)

const maxPlatformUpdateRolloutCreateRequestBytes = 64 * 1024

var canonicalUUIDv4Pattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type platformUpdateRolloutRepository interface {
	PersistPlatformUpdateRolloutPlanIdempotent(context.Context, PlatformUpdateRolloutPlan, string, string) (string, bool, error)
	GetPlatformUpdateRollout(context.Context, string) (PlatformUpdateRolloutView, error)
	AdvancePlatformUpdateRollout(context.Context, string) (PlatformUpdateRolloutStepResult, error)
}

type CreatePlatformUpdateRolloutRequest struct {
	TargetVersion string   `json:"targetVersion"`
	ServerIDs     []string `json:"serverIds"`
}

type PlatformUpdateRolloutResponse struct {
	Rollout PlatformUpdateRolloutView `json:"rollout"`
}

type AdvancePlatformUpdateRolloutResponse struct {
	RolloutID     string                          `json:"rolloutId"`
	RolloutStatus PlatformUpdateRolloutStatus     `json:"rolloutStatus"`
	ServerID      string                          `json:"serverId,omitempty"`
	JobID         string                          `json:"jobId,omitempty"`
	Action        PlatformUpdateRolloutStepAction `json:"action"`
	WaitingReason string                          `json:"waitingReason,omitempty"`
	ErrorCode     string                          `json:"errorCode,omitempty"`
	BlockerCode   string                          `json:"blockerCode,omitempty"`
}

func (h *Handler) CreatePlatformUpdateRollout(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !canonicalUUIDv4Pattern.MatchString(key) {
		h.recordPlatformUpdateRolloutAudit(r, "platform_update_rollout.created", "", "", 0, audit.ResultFailure, "invalid_idempotency_key")
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_idempotency_key", "Idempotency-Key must be a canonical UUIDv4."))
		return
	}
	var request CreatePlatformUpdateRolloutRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPlatformUpdateRolloutCreateRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		h.recordPlatformUpdateRolloutAudit(r, "platform_update_rollout.created", "", "", 0, audit.ResultFailure, "invalid_request")
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_request", "Request body must contain exactly one bounded rollout request."))
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		h.recordPlatformUpdateRolloutAudit(r, "platform_update_rollout.created", "", "", 0, audit.ResultFailure, "invalid_request")
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_request", "Request body must contain exactly one bounded rollout request."))
		return
	}
	if !validPlatformUpdateTargetVersion(request.TargetVersion) || len(request.ServerIDs) == 0 || len(request.ServerIDs) > maxPlatformUpdateRolloutMembers {
		h.recordPlatformUpdateRolloutAudit(r, "platform_update_rollout.created", "", "", 0, audit.ResultFailure, "invalid_request")
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_request", "targetVersion and between 1 and 1024 canonical serverIds are required."))
		return
	}
	candidates := make([]PlatformUpdateRolloutCandidate, len(request.ServerIDs))
	for i, id := range request.ServerIDs {
		if canonical, err := canonicalPlatformUpdateServerID(id); err != nil || canonical != id {
			h.recordPlatformUpdateRolloutAudit(r, "platform_update_rollout.created", "", "", 0, audit.ResultFailure, "invalid_server_id")
			httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_server_id", "Every serverId must be a canonical RouteGate UUID."))
			return
		}
		candidates[i] = PlatformUpdateRolloutCandidate{ServerID: id}
	}
	plan, err := PlanPlatformUpdateRollout(buildinfo.Current().Version, request.TargetVersion, candidates)
	if err != nil {
		h.recordPlatformUpdateRolloutAudit(r, "platform_update_rollout.created", "", "", 0, audit.ResultFailure, "invalid_request")
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_request", "The rollout request contains duplicate or invalid serverIds."))
		return
	}
	hash := rolloutCreationRequestHash(request.TargetVersion, request.ServerIDs)
	repository, ok := h.repository.(platformUpdateRolloutRepository)
	if !ok {
		h.recordPlatformUpdateRolloutAudit(r, "platform_update_rollout.created", "", "", 0, audit.ResultFailure, "update_not_supported")
		httpx.WriteJSON(w, http.StatusNotImplemented, httpx.Error("update_not_supported", "Platform update rollouts are not supported by this Manager."))
		return
	}
	rolloutID, replayed, err := repository.PersistPlatformUpdateRolloutPlanIdempotent(r.Context(), plan, key, hash)
	if errors.Is(err, ErrPlatformUpdateRolloutIdempotencyConflict) {
		h.recordPlatformUpdateRolloutAudit(r, "platform_update_rollout.created", "", request.TargetVersion, len(request.ServerIDs), audit.ResultFailure, "idempotency_conflict")
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("idempotency_conflict", "Idempotency-Key was already used for a different rollout request."))
		return
	}
	if err != nil {
		h.logger.Error("create platform update rollout failed", "error", err)
		h.recordPlatformUpdateRolloutAudit(r, "platform_update_rollout.created", "", request.TargetVersion, len(request.ServerIDs), audit.ResultFailure, "database_error")
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Failed to create platform update rollout."))
		return
	}
	view, err := repository.GetPlatformUpdateRollout(r.Context(), rolloutID)
	if err != nil {
		h.logger.Error("read created platform update rollout failed", "error", err, "rollout_id", rolloutID)
		h.recordPlatformUpdateRolloutAudit(r, "platform_update_rollout.created", rolloutID, request.TargetVersion, len(request.ServerIDs), audit.ResultFailure, "database_error")
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Failed to read platform update rollout."))
		return
	}
	h.recordPlatformUpdateRolloutAudit(r, "platform_update_rollout.created", rolloutID, request.TargetVersion, len(request.ServerIDs), audit.ResultSuccess, "")
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	httpx.WriteJSON(w, status, PlatformUpdateRolloutResponse{Rollout: view})
}

func (h *Handler) GetPlatformUpdateRollout(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("rollout_id"))
	if canonical, err := canonicalPlatformUpdateServerID(id); err != nil || canonical != id {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("rollout_not_found", "Platform update rollout was not found."))
		return
	}
	repository, ok := h.repository.(platformUpdateRolloutRepository)
	if !ok {
		httpx.WriteJSON(w, http.StatusNotImplemented, httpx.Error("update_not_supported", "Platform update rollouts are not supported by this Manager."))
		return
	}
	view, err := repository.GetPlatformUpdateRollout(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("rollout_not_found", "Platform update rollout was not found."))
		return
	}
	if err != nil {
		h.logger.Error("read platform update rollout failed", "error", err, "rollout_id", id)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Failed to read platform update rollout."))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, PlatformUpdateRolloutResponse{Rollout: view})
}

func (h *Handler) AdvancePlatformUpdateRollout(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("rollout_id"))
	if canonical, err := canonicalPlatformUpdateServerID(id); err != nil || canonical != id {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("rollout_not_found", "Platform update rollout was not found."))
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1))
	if err != nil || len(body) != 0 {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_request", "Advance request body must be empty."))
		return
	}
	repository, ok := h.repository.(platformUpdateRolloutRepository)
	if !ok {
		httpx.WriteJSON(w, http.StatusNotImplemented, httpx.Error("update_not_supported", "Platform update rollouts are not supported by this Manager."))
		return
	}
	result, err := repository.AdvancePlatformUpdateRollout(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("rollout_not_found", "Platform update rollout was not found."))
		return
	}
	if err != nil {
		h.logger.Error("advance platform update rollout failed", "error", err, "rollout_id", id)
		h.recordPlatformUpdateRolloutAudit(r, "platform_update_rollout.advanced", id, "", 0, audit.ResultFailure, "database_error")
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Failed to advance platform update rollout."))
		return
	}
	h.recordPlatformUpdateRolloutAudit(r, "platform_update_rollout.advanced", id, "", 0, audit.ResultSuccess, string(result.Action))
	httpx.WriteJSON(w, http.StatusOK, AdvancePlatformUpdateRolloutResponse{
		RolloutID: result.RolloutID, RolloutStatus: result.RolloutStatus,
		ServerID: result.ServerID, JobID: result.JobID, Action: result.Action,
		WaitingReason: result.WaitingReason, ErrorCode: result.ErrorCode, BlockerCode: result.BlockerCode,
	})
}

func rolloutCreationRequestHash(version string, ids []string) string {
	h := sha256.New()
	_, _ = io.WriteString(h, version)
	h.Write([]byte{0})
	for _, id := range ids {
		_, _ = io.WriteString(h, id)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (h *Handler) recordPlatformUpdateRolloutAudit(r *http.Request, action, id, version string, count int, result, reason string) {
	metadata := map[string]any{"server_count": count}
	if version != "" {
		metadata["target_version"] = version
	}
	if reason != "" {
		metadata["reason"] = reason
	}
	h.recordAudit(r.Context(), audit.EventInput{ActorType: audit.ActorTypeUser, Action: action, ResourceType: "platform_update_rollout", ResourceID: id, Result: result, Metadata: metadata})
}
