package agents

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Handler struct {
	logger *slog.Logger
	mu     sync.RWMutex
	items  []Agent
}

type Agent struct {
	ID        string    `json:"id"`
	ServerID  string    `json:"serverId"`
	Name      string    `json:"name"`
	Version   string    `json:"version"`
	Hostname  string    `json:"hostname"`
	Status    string    `json:"status"`
	LastSeen  time.Time `json:"lastSeen"`
	CreatedAt time.Time `json:"createdAt"`
}

type ListAgentsResponse struct {
	Items []Agent `json:"items"`
}

type RegisterAgentRequest struct {
	ServerID string `json:"serverId"`
	Name     string `json:"name"`
	Version  string `json:"version"`
	Hostname string `json:"hostname"`
}

type RegisterAgentResponse struct {
	Agent Agent  `json:"agent"`
	Token string `json:"token"`
}

type HeartbeatRequest struct {
	AgentID  string `json:"agentId"`
	Version  string `json:"version"`
	Hostname string `json:"hostname"`
	Status   string `json:"status"`
}

type HeartbeatResponse struct {
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
}

type ErrorResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

func NewHandler(logger *slog.Logger) *Handler {
	now := time.Now().UTC()

	return &Handler{
		logger: logger,
		items: []Agent{
			{
				ID:        "agt-dev-001",
				ServerID:  "srv-dev-001",
				Name:      "Demo Agent",
				Version:   "0.1.0",
				Hostname:  "fi-demo-routegate",
				Status:    "online",
				LastSeen:  now,
				CreatedAt: now,
			},
		},
	}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	writeJSON(w, http.StatusOK, ListAgentsResponse{
		Items: h.items,
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

	now := time.Now().UTC()

	agent := Agent{
		ID:        "agt-dev-" + now.Format("20060102150405"),
		ServerID:  request.ServerID,
		Name:      request.Name,
		Version:   fallback(request.Version, "0.1.0"),
		Hostname:  request.Hostname,
		Status:    "online",
		LastSeen:  now,
		CreatedAt: now,
	}

	h.mu.Lock()
	h.items = append([]Agent{agent}, h.items...)
	h.mu.Unlock()

	h.logger.Info("dev agent registered", "id", agent.ID, "name", agent.Name)

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

	now := time.Now().UTC()
	status := fallback(request.Status, "online")

	h.mu.Lock()
	defer h.mu.Unlock()

	for index := range h.items {
		if h.items[index].ID == request.AgentID {
			h.items[index].Status = status
			h.items[index].LastSeen = now

			if request.Version != "" {
				h.items[index].Version = request.Version
			}

			if request.Hostname != "" {
				h.items[index].Hostname = request.Hostname
			}

			h.logger.Debug("dev agent heartbeat accepted", "id", request.AgentID, "status", status)

			writeJSON(w, http.StatusOK, HeartbeatResponse{
				Status:    "ok",
				Timestamp: now,
			})
			return
		}
	}

	writeJSON(w, http.StatusNotFound, ErrorResponse{
		Status:  "agent_not_found",
		Message: "Agent was not found.",
	})
}

func fallback(value string, defaultValue string) string {
	if value == "" {
		return defaultValue
	}

	return value
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}
