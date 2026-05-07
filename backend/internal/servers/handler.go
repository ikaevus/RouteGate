package servers

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
		h.logger.Error("list servers failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{
			Status:  "database_error",
			Message: "Failed to list servers.",
		})
		return
	}

	if len(items) == 0 {
		if err := h.repository.SeedDemo(r.Context()); err != nil {
			h.logger.Error("seed demo server failed", "error", err)
		}

		items, err = h.repository.List(r.Context())
		if err != nil {
			h.logger.Error("list servers after seed failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{
				Status:  "database_error",
				Message: "Failed to list servers.",
			})
			return
		}
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

	server, err := h.repository.Create(r.Context(), request)
	if err != nil {
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

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}
