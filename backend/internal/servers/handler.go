package servers

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
		h.logger.Error("list servers failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{
			Status:  "database_error",
			Message: "Failed to list servers.",
		})
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

	server, err := h.service.Create(r.Context(), request)
	if err != nil {
		if errors.Is(err, ErrServerNameRequired) {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{
				Status:  "name_required",
				Message: "Server name is required.",
			})
			return
		}

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
