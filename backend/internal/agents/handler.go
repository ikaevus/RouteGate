package agents

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	logger *slog.Logger
	pool   *pgxpool.Pool
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

func NewHandler(logger *slog.Logger, pool *pgxpool.Pool) *Handler {
	return &Handler{
		logger: logger,
		pool:   pool,
	}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT
			id::text,
			COALESCE(server_id::text, ''),
			name,
			COALESCE(version, ''),
			COALESCE(hostname, ''),
			status,
			COALESCE(last_seen_at, created_at),
			created_at
		FROM agents
		ORDER BY created_at DESC;
	`)
	if err != nil {
		h.logger.Error("list agents failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{
			Status:  "database_error",
			Message: "Failed to list agents.",
		})
		return
	}
	defer rows.Close()

	items := make([]Agent, 0)

	for rows.Next() {
		var item Agent

		if err := rows.Scan(
			&item.ID,
			&item.ServerID,
			&item.Name,
			&item.Version,
			&item.Hostname,
			&item.Status,
			&item.LastSeen,
			&item.CreatedAt,
		); err != nil {
			h.logger.Error("scan agent failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{
				Status:  "database_error",
				Message: "Failed to read agent row.",
			})
			return
		}

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		h.logger.Error("iterate agents failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{
			Status:  "database_error",
			Message: "Failed to list agents.",
		})
		return
	}

	if len(items) == 0 {
		if err := h.seedDemoAgent(r.Context()); err != nil {
			h.logger.Error("seed demo agent failed", "error", err)
		}

		h.List(w, r)
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

	serverID := normalizeServerID(request.ServerID)
	now := time.Now().UTC()

	var agent Agent

	if err := h.pool.QueryRow(r.Context(), `
		INSERT INTO agents (
			server_id,
			name,
			version,
			hostname,
			status,
			last_seen_at
		)
		VALUES (
			$1,
			$2,
			NULLIF($3, ''),
			NULLIF($4, ''),
			'online',
			$5
		)
		RETURNING
			id::text,
			COALESCE(server_id::text, ''),
			name,
			COALESCE(version, ''),
			COALESCE(hostname, ''),
			status,
			COALESCE(last_seen_at, created_at),
			created_at;
	`,
		serverID,
		request.Name,
		fallback(request.Version, "0.1.0"),
		request.Hostname,
		now,
	).Scan(
		&agent.ID,
		&agent.ServerID,
		&agent.Name,
		&agent.Version,
		&agent.Hostname,
		&agent.Status,
		&agent.LastSeen,
		&agent.CreatedAt,
	); err != nil {
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

	now := time.Now().UTC()
	status := fallback(request.Status, "online")

	commandTag, err := h.pool.Exec(r.Context(), `
		UPDATE agents
		SET
			status = $1,
			last_seen_at = $2,
			version = COALESCE(NULLIF($3, ''), version),
			hostname = COALESCE(NULLIF($4, ''), hostname),
			updated_at = now()
		WHERE id = $5::uuid;
	`,
		status,
		now,
		request.Version,
		request.Hostname,
		request.AgentID,
	)
	if err != nil {
		h.logger.Error("agent heartbeat failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{
			Status:  "database_error",
			Message: "Failed to update agent heartbeat.",
		})
		return
	}

	if commandTag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, ErrorResponse{
			Status:  "agent_not_found",
			Message: "Agent was not found.",
		})
		return
	}

	_, _ = h.pool.Exec(r.Context(), `
		INSERT INTO agent_heartbeats (
			agent_id,
			payload
		)
		VALUES (
			$1::uuid,
			jsonb_build_object(
				'status', $2::text,
				'version', $3::text,
				'hostname', $4::text
			)
		);
	`,
		request.AgentID,
		status,
		request.Version,
		request.Hostname,
	)

	h.logger.Debug("agent heartbeat accepted", "id", request.AgentID, "status", status)

	writeJSON(w, http.StatusOK, HeartbeatResponse{
		Status:    "ok",
		Timestamp: now,
	})
}

func (h *Handler) seedDemoAgent(ctx context.Context) error {
	var serverID string

	if err := h.pool.QueryRow(ctx, `
		SELECT id::text
		FROM servers
		ORDER BY created_at ASC
		LIMIT 1;
	`).Scan(&serverID); err != nil {
		return err
	}

	_, err := h.pool.Exec(ctx, `
		INSERT INTO agents (
			server_id,
			name,
			version,
			hostname,
			status,
			last_seen_at
		)
		VALUES (
			$1::uuid,
			'Demo Agent',
			'0.1.0',
			'fi-demo-routegate',
			'online',
			now()
		);
	`, serverID)

	return err
}

func normalizeServerID(value string) any {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "srv-dev-") {
		return nil
	}

	return value
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
