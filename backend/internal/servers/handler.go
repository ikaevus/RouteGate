package servers

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

type Server struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Hostname  string    `json:"hostname"`
	PublicIP  string    `json:"publicIp"`
	Location  string    `json:"location"`
	Provider  string    `json:"provider"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

type CreateServerRequest struct {
	Name     string `json:"name"`
	Hostname string `json:"hostname"`
	PublicIP string `json:"publicIp"`
	Location string `json:"location"`
	Provider string `json:"provider"`
}

type ListServersResponse struct {
	Items []Server `json:"items"`
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
			name,
			COALESCE(hostname, ''),
			COALESCE(public_ip::text, ''),
			COALESCE(location, ''),
			COALESCE(provider, ''),
			status,
			created_at
		FROM servers
		ORDER BY created_at DESC;
	`)
	if err != nil {
		h.logger.Error("list servers failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{
			Status:  "database_error",
			Message: "Failed to list servers.",
		})
		return
	}
	defer rows.Close()

	items := make([]Server, 0)

	for rows.Next() {
		var item Server

		if err := rows.Scan(
			&item.ID,
			&item.Name,
			&item.Hostname,
			&item.PublicIP,
			&item.Location,
			&item.Provider,
			&item.Status,
			&item.CreatedAt,
		); err != nil {
			h.logger.Error("scan server failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{
				Status:  "database_error",
				Message: "Failed to read server row.",
			})
			return
		}

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		h.logger.Error("iterate servers failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{
			Status:  "database_error",
			Message: "Failed to list servers.",
		})
		return
	}

	if len(items) == 0 {
		if err := h.seedDemoServer(r.Context()); err != nil {
			h.logger.Error("seed demo server failed", "error", err)
		}

		h.List(w, r)
		return
	}

	writeJSON(w, http.StatusOK, ListServersResponse{
		Items: items,
	})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var request CreateServerRequest

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Status:  "invalid_request",
			Message: "Request body must be valid JSON.",
		})
		return
	}

	request.Name = strings.TrimSpace(request.Name)
	request.Hostname = strings.TrimSpace(request.Hostname)
	request.PublicIP = strings.TrimSpace(request.PublicIP)
	request.Location = strings.TrimSpace(request.Location)
	request.Provider = strings.TrimSpace(request.Provider)

	if request.Name == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Status:  "name_required",
			Message: "Server name is required.",
		})
		return
	}

	var server Server

	if err := h.pool.QueryRow(r.Context(), `
		INSERT INTO servers (
			name,
			hostname,
			public_ip,
			location,
			provider,
			status
		)
		VALUES (
			$1,
			NULLIF($2, ''),
			NULLIF($3, '')::inet,
			NULLIF($4, ''),
			NULLIF($5, ''),
			'unknown'
		)
		RETURNING
			id::text,
			name,
			COALESCE(hostname, ''),
			COALESCE(public_ip::text, ''),
			COALESCE(location, ''),
			COALESCE(provider, ''),
			status,
			created_at;
	`,
		request.Name,
		request.Hostname,
		request.PublicIP,
		request.Location,
		request.Provider,
	).Scan(
		&server.ID,
		&server.Name,
		&server.Hostname,
		&server.PublicIP,
		&server.Location,
		&server.Provider,
		&server.Status,
		&server.CreatedAt,
	); err != nil {
		h.logger.Error("create server failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{
			Status:  "database_error",
			Message: "Failed to create server.",
		})
		return
	}

	h.logger.Info("server created", "id", server.ID, "name", server.Name)

	writeJSON(w, http.StatusCreated, server)
}

func (h *Handler) seedDemoServer(ctx context.Context) error {
	_, err := h.pool.Exec(ctx, `
		INSERT INTO servers (
			name,
			hostname,
			public_ip,
			location,
			provider,
			status
		)
		VALUES (
			'Demo Finland VPS',
			'fi-demo.routegate.local',
			'203.0.113.10'::inet,
			'Finland',
			'Demo',
			'online'
		);
	`)

	return err
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}
