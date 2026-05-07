package agents

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/artuazh/routegate/backend/internal/httpx"
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
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error(
			"database_error",
			"Failed to list agents.",
		))
		return
	}

	httpx.WriteJSON(w, http.StatusOK, ListAgentsResponse{
		Items: items,
	})
}

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var request RegisterAgentRequest

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error(
			"invalid_request",
			"Request body must be valid JSON.",
		))
		return
	}

	agent, err := h.service.Register(r.Context(), request)
	if err != nil {
		if errors.Is(err, ErrAgentNameRequired) {
			httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error(
				"name_required",
				"Agent name is required.",
			))
			return
		}

		h.logger.Error("register agent failed", "error", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error(
			"database_error",
			"Failed to register agent.",
		))
		return
	}

	h.logger.Info("agent registered", "id", agent.ID, "name", agent.Name)

	httpx.WriteJSON(w, http.StatusCreated, RegisterAgentResponse{
		Agent: agent,
		Token: "routegate-agent-dev-token",
	})
}

func (h *Handler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	var request HeartbeatRequest

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error(
			"invalid_request",
			"Request body must be valid JSON.",
		))
		return
	}

	timestamp, err := h.service.Heartbeat(r.Context(), request)
	if err != nil {
		if errors.Is(err, ErrAgentIDRequired) {
			httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error(
				"agent_id_required",
				"Agent ID is required.",
			))
			return
		}

		if errors.Is(err, ErrAgentNotFound) {
			httpx.WriteJSON(w, http.StatusNotFound, httpx.Error(
				"agent_not_found",
				"Agent was not found.",
			))
			return
		}

		h.logger.Error("agent heartbeat failed", "error", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error(
			"database_error",
			"Failed to update agent heartbeat.",
		))
		return
	}

	h.logger.Debug("agent heartbeat accepted", "id", request.AgentID, "status", request.Status)

	httpx.WriteJSON(w, http.StatusOK, HeartbeatResponse{
		Status:    "ok",
		Timestamp: timestamp,
	})
}
