package agents

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
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

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.List(r.Context())
	if err != nil {
		h.logger.Error("list agents failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{
			Status:  "database_error",
			Message: "Failed to list agents.",
		})
		return
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

	agent, err := h.service.Register(r.Context(), request)
	if err != nil {
		if errors.Is(err, ErrAgentNameRequired) {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{
				Status:  "name_required",
				Message: "Agent name is required.",
			})
			return
		}

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

	timestamp, err := h.service.Heartbeat(r.Context(), request)
	if err != nil {
		if errors.Is(err, ErrAgentIDRequired) {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{
				Status:  "agent_id_required",
				Message: "Agent ID is required.",
			})
			return
		}

		if errors.Is(err, ErrAgentNotFound) {
			writeJSON(w, http.StatusNotFound, ErrorResponse{
				Status:  "agent_not_found",
				Message: "Agent was not found.",
			})
			return
		}

		h.logger.Error("agent heartbeat failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{
			Status:  "database_error",
			Message: "Failed to update agent heartbeat.",
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
