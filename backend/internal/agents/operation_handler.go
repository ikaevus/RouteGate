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
	job, err := repository.CreateAgentOperationJob(r.Context(), CreateAgentOperationJobInput{
		ServerID:  serverID,
		Operation: request.Operation,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("server_or_agent_not_found", "A connected server was not found."))
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
		ActorType:    audit.ActorTypeUser,
		Action:       "vpn_core.operation.created",
		ResourceType: "agent_operation_job",
		ResourceID:   job.ID,
		Result:       audit.ResultSuccess,
		Metadata: map[string]any{
			"server_id": serverID,
			"kind":      job.Kind,
			"operation": job.Operation,
		},
	})
	httpx.WriteJSON(w, http.StatusAccepted, CreateVPNCoreOperationResponse{Job: job})
}
