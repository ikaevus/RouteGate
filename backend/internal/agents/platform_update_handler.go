package agents

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ikaevus/routegate/backend/internal/audit"
	"github.com/ikaevus/routegate/backend/internal/httpx"
)

const maxPlatformUpdateCreateRequestBytes = 1024

type platformUpdateCreateRepository interface {
	CreatePlatformUpdateJob(context.Context, CreatePlatformUpdateJobInput) (PlatformUpdateJob, error)
}

type platformUpdateQueryRepository interface {
	GetPlatformUpdateJob(context.Context, string, string) (PlatformUpdateJob, error)
}

type CreatePlatformUpdateRequest struct {
	TargetVersion string `json:"targetVersion"`
}

type CreatePlatformUpdateResponse struct {
	Job PlatformUpdateJob `json:"job"`
}

func (h *Handler) CreatePlatformUpdate(w http.ResponseWriter, r *http.Request) {
	serverID := strings.TrimSpace(r.PathValue("server_id"))
	var request CreatePlatformUpdateRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPlatformUpdateCreateRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		h.recordPlatformUpdateCreateAudit(r, serverID, "", "", audit.ResultFailure, "invalid_request")
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_request", "Request body must contain exactly one valid platform update request."))
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		h.recordPlatformUpdateCreateAudit(r, serverID, "", "", audit.ResultFailure, "invalid_request")
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_request", "Request body must contain exactly one valid platform update request."))
		return
	}
	if !validPlatformUpdateTargetVersion(request.TargetVersion) {
		h.recordPlatformUpdateCreateAudit(r, serverID, "", "", audit.ResultFailure, "invalid_target_version")
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_target_version", "targetVersion must be a canonical RouteGate release version that fits the Agent update-task contract."))
		return
	}

	repository, ok := h.repository.(platformUpdateCreateRepository)
	if !ok {
		h.recordPlatformUpdateCreateAudit(r, serverID, request.TargetVersion, "", audit.ResultFailure, "update_not_supported")
		httpx.WriteJSON(w, http.StatusNotImplemented, httpx.Error("update_not_supported", "Remote platform updates are not supported by this Manager."))
		return
	}

	job, err := repository.CreatePlatformUpdateJob(r.Context(), CreatePlatformUpdateJobInput{
		ServerID:      serverID,
		TargetVersion: request.TargetVersion,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		h.recordPlatformUpdateCreateAudit(r, serverID, request.TargetVersion, "", audit.ResultFailure, "update_not_ready")
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("update_not_ready", "The target VPN node does not advertise the ready RouteGate software-update capability."))
		return
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		h.recordPlatformUpdateCreateAudit(r, serverID, request.TargetVersion, "", audit.ResultFailure, "update_in_progress")
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("update_in_progress", "Another RouteGate software update is already active for this server."))
		return
	}
	if err != nil {
		h.logger.Error("create remote platform update job failed", "error", err, "server_id", serverID)
		h.recordPlatformUpdateCreateAudit(r, serverID, request.TargetVersion, "", audit.ResultFailure, "database_error")
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Failed to create RouteGate software update."))
		return
	}

	h.recordPlatformUpdateCreateAudit(r, serverID, job.TargetVersion, job.ID, audit.ResultSuccess, "")
	httpx.WriteJSON(w, http.StatusAccepted, CreatePlatformUpdateResponse{Job: job})
}

func (h *Handler) GetPlatformUpdate(w http.ResponseWriter, r *http.Request) {
	repository, ok := h.repository.(platformUpdateQueryRepository)
	if !ok {
		httpx.WriteJSON(w, http.StatusNotImplemented, httpx.Error("update_not_supported", "Remote platform updates are not supported by this Manager."))
		return
	}
	serverID := strings.TrimSpace(r.PathValue("server_id"))
	jobID := strings.TrimSpace(r.PathValue("job_id"))
	job, err := repository.GetPlatformUpdateJob(r.Context(), serverID, jobID)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("update_not_found", "RouteGate software update job was not found."))
		return
	}
	if err != nil {
		h.logger.Error("read remote platform update job failed", "error", err, "server_id", serverID, "job_id", jobID)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Failed to read RouteGate software update."))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, job)
}

func (h *Handler) recordPlatformUpdateCreateAudit(r *http.Request, serverID, targetVersion, jobID, result, reason string) {
	metadata := map[string]any{"server_id": serverID}
	if targetVersion != "" {
		metadata["target_version"] = targetVersion
	}
	if reason != "" {
		metadata["reason"] = reason
	}
	h.recordAudit(r.Context(), audit.EventInput{
		ActorType:    audit.ActorTypeUser,
		Action:       "server.software_update.created",
		ResourceType: "platform_update_job",
		ResourceID:   jobID,
		Result:       result,
		Metadata:     metadata,
	})
}
