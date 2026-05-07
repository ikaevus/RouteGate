package health

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/artuazh/routegate/backend/internal/httpx"
)

type Handler struct {
	logger *slog.Logger
}

type Response struct {
	Status    string    `json:"status"`
	Service   string    `json:"service"`
	Timestamp time.Time `json:"timestamp"`
}

func NewHandler(logger *slog.Logger) *Handler {
	return &Handler{logger: logger}
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, Response{
		Status:    "ok",
		Service:   "routegate-manager",
		Timestamp: time.Now().UTC(),
	})
}
