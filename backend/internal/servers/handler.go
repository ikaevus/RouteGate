package servers

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
		h.logger.Error("list servers failed", "error", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error(
			"database_error",
			"Failed to list servers.",
		))
		return
	}

	httpx.WriteJSON(w, http.StatusOK, ListServersResponse{
		Items: items,
	})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var request CreateServerRequest

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error(
			"invalid_request",
			"Request body must be valid JSON.",
		))
		return
	}

	server, err := h.service.Create(r.Context(), request)
	if err != nil {
		if errors.Is(err, ErrServerNameRequired) {
			httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error(
				"name_required",
				"Server name is required.",
			))
			return
		}

		h.logger.Error("create server failed", "error", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error(
			"database_error",
			"Failed to create server.",
		))
		return
	}

	h.logger.Info("server created", "id", server.ID, "name", server.Name)

	httpx.WriteJSON(w, http.StatusCreated, server)
}
