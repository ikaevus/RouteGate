package agents

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ikaevus/routegate/backend/internal/audit"
	"github.com/ikaevus/routegate/backend/internal/httpx"
)

type agentOperationCreateRepository interface {
	CreateAgentOperationJob(context.Context, CreateAgentOperationJobInput) (AgentConfigTask, error)
}

type agentOperationQueryRepository interface {
	GetAgentOperationJob(context.Context, string, string) (AgentConfigTask, error)
}

type CreateVPNCoreOperationRequest struct {
	Operation string `json:"operation"`
}

type CreateVPNCoreOperationResponse struct {
	Job AgentConfigTask `json:"job"`
}

func (h *Handler) CreateVPNCoreOperation(w http.ResponseWriter, r *http.Request) {
	var request CreateVPNCoreOperationRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_request", "Request body must be valid JSON."))
		return
	}
	request.Operation = strings.TrimSpace(request.Operation)
	if !ValidVPNCoreOperation(request.Operation) {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_operation", "Operation must be one of: start, stop, restart."))
		return
	}

	repository, ok := h.repository.(agentOperationCreateRepository)
	if !ok {
		httpx.WriteJSON(w, http.StatusNotImplemented, httpx.Error("operation_not_supported", "VPN Core service operations are not supported by this Manager."))
		return
	}

	serverID := strings.TrimSpace(r.PathValue("server_id"))
	job, err := repository.CreateAgentOperationJob(r.Context(), CreateAgentOperationJobInput{ServerID: serverID, Operation: request.Operation})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("server_or_agent_not_found", "A connected compatible Agent was not found."))
		return
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("operation_in_progress", "Another VPN Core operation is already pending or in progress for this server."))
		return
	}
	if err != nil {
		h.logger.Error("create VPN Core operation job failed", "error", err, "server_id", serverID, "operation", request.Operation)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Failed to create VPN Core operation."))
		return
	}

	h.recordAudit(r.Context(), audit.EventInput{
		ActorType: audit.ActorTypeUser, Action: "vpn_core.operation.created", ResourceType: "agent_operation_job", ResourceID: job.ID, Result: audit.ResultSuccess,
		Metadata: map[string]any{"server_id": serverID, "kind": job.Kind, "operation": job.Operation},
	})
	httpx.WriteJSON(w, http.StatusAccepted, CreateVPNCoreOperationResponse{Job: job})
}

func (h *Handler) GetVPNCoreOperation(w http.ResponseWriter, r *http.Request) {
	repository, ok := h.repository.(agentOperationQueryRepository)
	if !ok {
		httpx.WriteJSON(w, http.StatusNotImplemented, httpx.Error("operation_not_supported", "VPN Core service operations are not supported by this Manager."))
		return
	}
	job, err := repository.GetAgentOperationJob(r.Context(), strings.TrimSpace(r.PathValue("server_id")), strings.TrimSpace(r.PathValue("job_id")))
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("operation_not_found", "VPN Core operation was not found."))
		return
	}
	if err != nil {
		h.logger.Error("read VPN Core operation job failed", "error", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Failed to read VPN Core operation."))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, job)
}

func sanitizeVPNCoreInstallationJob(job AgentConfigTask) AgentConfigTask {
	job.ErrorMessage = safeInstallationErrorCode(job.ErrorMessage)
	raw := job.ResultPayload
	safe := map[string]any{
		"kind":      AgentTaskKindVPNCoreInstall,
		"operation": VPNCoreOperationInstallSingBox,
		"status":    boundedString(raw["status"], 32),
	}
	if platform, ok := raw["platform"].(map[string]any); ok {
		safe["platform"] = map[string]any{
			"id":           boundedString(platform["id"], 64),
			"version":      boundedString(platform["version"], 64),
			"architecture": boundedString(platform["architecture"], 32),
		}
	}
	for key, limit := range map[string]int{
		"singBoxVersion": 256,
		"binaryPath":     512,
		"serviceName":    128,
	} {
		if value := boundedString(raw[key], limit); value != "" {
			safe[key] = value
		}
	}
	if stages, ok := raw["stages"].([]any); ok {
		safeStages := make([]map[string]any, 0, min(len(stages), 8))
		for _, rawStage := range stages {
			if len(safeStages) == 8 {
				break
			}
			stage, ok := rawStage.(map[string]any)
			if !ok {
				continue
			}
			safeStages = append(safeStages, map[string]any{
				"stage":  boundedString(stage["stage"], 64),
				"status": boundedString(stage["status"], 32),
				"code":   boundedString(stage["code"], 96),
			})
		}
		safe["stages"] = safeStages
	}
	job.ResultPayload = safe
	return job
}

func boundedString(value any, limit int) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	text = strings.TrimSpace(text)
	if len(text) > limit {
		return text[:limit]
	}
	return text
}

func safeInstallationErrorCode(value string) string {
	switch strings.TrimSpace(value) {
	case "":
		return ""
	case "unsupported_platform",
		"unsupported_distribution",
		"unsupported_architecture",
		"platform_detection_failed",
		"repository_configuration_failed",
		"signing_key_download_failed",
		"signing_key_download_timeout",
		"signing_key_conflict",
		"repository_source_conflict",
		"package_index_refresh_failed",
		"package_installation_failed",
		"service_start_guard_failed",
		"service_start_guard_cleanup_failed",
		"installed_binary_not_found",
		"binary_verification_failed",
		"binary_version_unavailable",
		"service_verification_failed",
		"unsupported_installation_task",
		"unsupported_installation_operation":
		return strings.TrimSpace(value)
	default:
		return "installation_failed"
	}
}

func (h *Handler) CreateVPNCoreInstallation(w http.ResponseWriter, r *http.Request) {
	repository, ok := h.repository.(agentOperationCreateRepository)
	if !ok {
		httpx.WriteJSON(w, http.StatusNotImplemented, httpx.Error("installation_not_supported", "VPN Core installation is not supported by this Manager."))
		return
	}

	serverID := strings.TrimSpace(r.PathValue("server_id"))
	job, err := repository.CreateAgentOperationJob(r.Context(), CreateAgentOperationJobInput{
		ServerID:  serverID,
		Kind:      AgentTaskKindVPNCoreInstall,
		Operation: VPNCoreOperationInstallSingBox,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("agent_installation_unsupported", "The connected Agent does not support sing-box installation on this server."))
		return
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("operation_in_progress", "Another VPN Core operation is already pending or in progress for this server."))
		return
	}
	if err != nil {
		h.logger.Error("create VPN Core installation job failed", "error", err, "server_id", serverID)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Failed to create VPN Core installation."))
		return
	}

	h.recordAudit(r.Context(), audit.EventInput{
		ActorType: audit.ActorTypeUser, Action: "vpn_core.installation.created", ResourceType: "agent_operation_job", ResourceID: job.ID, Result: audit.ResultSuccess,
		Metadata: map[string]any{"server_id": serverID, "kind": job.Kind, "operation": job.Operation},
	})
	httpx.WriteJSON(w, http.StatusAccepted, CreateVPNCoreOperationResponse{Job: job})
}

func (h *Handler) GetVPNCoreInstallation(w http.ResponseWriter, r *http.Request) {
	repository, ok := h.repository.(agentOperationQueryRepository)
	if !ok {
		httpx.WriteJSON(w, http.StatusNotImplemented, httpx.Error("installation_not_supported", "VPN Core installation is not supported by this Manager."))
		return
	}
	job, err := repository.GetAgentOperationJob(r.Context(), strings.TrimSpace(r.PathValue("server_id")), strings.TrimSpace(r.PathValue("job_id")))
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && job.Kind != AgentTaskKindVPNCoreInstall) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("installation_not_found", "VPN Core installation was not found."))
		return
	}
	if err != nil {
		h.logger.Error("read VPN Core installation job failed", "error", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Failed to read VPN Core installation."))
		return
	}
	job = sanitizeVPNCoreInstallationJob(job)
	httpx.WriteJSON(w, http.StatusOK, job)
}
