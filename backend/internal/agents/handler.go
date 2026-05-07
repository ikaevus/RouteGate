package agents

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	logger     *slog.Logger
	repository *Repository
}

func NewHandler(logger *slog.Logger, pool *pgxpool.Pool) *Handler {
	return &Handler{
		logger:     logger,
		repository: NewRepository(pool),
	}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	items, err := h.repository.List(r.Context())
	if err != nil {
		h.logger.Error("list agents failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{
			Status:  "database_error",
			Message: "Failed to list agents.",
		})
		return
	}

	if len(items) == 0 {
		if err := h.repository.SeedDemo(r.Context()); err != nil {
			h.logger.Error("seed demo agent failed", "error", err)
		}

		items, err = h.repository.List(r.Context())
		if err != nil {
			h.logger.Error("list agents after seed failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{
				Status:  "database_error",
				Message: "Failed to list agents.",
			})
			return
		}
	}

	writeJSON(w, http.StatusOK, ListAgentsResponse{
		Items: items,
	})
}

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var request RegisterAgentRequest

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Status:  "invalid_request",
			Message: "Request body must be valid JSON.",
		})
		return
	}

	request.ServerID = strings.TrimSpace(request.ServerID)
	request.Name = strings.TrimSpace(request.Name)
	request.Version = strings.TrimSpace(request.Version)
	request.Hostname = strings.TrimSpace(request.Hostname)

	if request.Name == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Status:  "name_required",
			Message: "Agent name is required.",
		})
		return
	}

	agent, err := h.repository.Register(r.Context(), request)
	if err != nil {
		h.logger.Error("register agent failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{
			Status:  "database_error",
			Message: "Failed to register agent.",
		})
		return
	}

	h.logger.Info("agent registered", "id", agent.ID, "name", agent.Name)

	writeJSON(w, http.StatusCreated, RegisterAgentResponse{
		Agent: agent,
		Token: "routegate-agent-dev-token",
	})
}

func (h *Handler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	var request HeartbeatRequest

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Status:  "invalid_request",
			Message: "Request body must be valid JSON.",
		})
		return
	}

	request.AgentID = strings.TrimSpace(request.AgentID)
	request.Version = strings.TrimSpace(request.Version)
	request.Hostname = strings.TrimSpace(request.Hostname)
	request.Status = strings.TrimSpace(request.Status)

	if request.AgentID == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Status:  "agent_id_required",
			Message: "Agent ID is required.",
		})
		return
	}

	timestamp, found, err := h.repository.Heartbeat(r.Context(), request)
	if err != nil {
		h.logger.Error("agent heartbeat failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{
			Status:  "database_error",
			Message: "Failed to update agent heartbeat.",
		})
		return
	}

	if !found {
		writeJSON(w, http.StatusNotFound, ErrorResponse{
			Status:  "agent_not_found",
			Message: "Agent was not found.",
		})
		return
	}

	h.logger.Debug("agent heartbeat accepted", "id", request.AgentID, "status", request.Status)

	writeJSON(w, http.StatusOK, HeartbeatResponse{
		Status:    "ok",
		Timestamp: timestamp,
	})
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}
