package health

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
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
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	_ = json.NewEncoder(w).Encode(Response{
		Status:    "ok",
		Service:   "routegate-manager",
		Timestamp: time.Now().UTC(),
	})
}
