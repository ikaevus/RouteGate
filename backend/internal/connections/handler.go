package connections

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ikaevus/routegate/backend/internal/agents"
	"github.com/ikaevus/routegate/backend/internal/httpx"
)

type Handler struct {
	logger *slog.Logger
	repo   *Repository
	now    func() time.Time
}

func NewHandler(logger *slog.Logger, pool *pgxpool.Pool) *Handler {
	return &Handler{logger: logger, repo: NewRepository(pool), now: time.Now}
}

func (h *Handler) ReportSnapshot(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if token == "" || token == r.Header.Get("Authorization") { h.unauthorized(w); return }
	var request SnapshotRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil { h.invalid(w, "Request body must be valid JSON."); return }
	if len(request.Items) > MaxSnapshotItems { h.invalid(w, "too many presence items in one snapshot"); return }
	now := h.now().UTC()
	observedAt := request.ObservedAt.UTC()
	if request.ObservedAt.IsZero() { observedAt = now }
	if observedAt.Before(now.Add(-5*time.Minute)) || observedAt.After(now.Add(time.Minute)) {
		h.invalid(w, "observedAt is outside the accepted time window"); return
	}
	seen := make(map[string]struct{}, len(request.Items))
	for index := range request.Items {
		item := &request.Items[index]
		item.VPNAccountID = strings.TrimSpace(item.VPNAccountID)
		item.Protocol = strings.TrimSpace(item.Protocol)
		item.Source = strings.TrimSpace(item.Source)
		item.Confidence = strings.TrimSpace(item.Confidence)
		if item.VPNAccountID == "" || item.Protocol == "" || item.Source == "" { h.invalid(w, "presence item fields are required"); return }
		if item.ConnectionCount <= 0 { h.invalid(w, "connectionCount must be greater than zero"); return }
		if item.Confidence != "exact" && item.Confidence != "heuristic" { h.invalid(w, "confidence must be exact or heuristic"); return }
		key := item.VPNAccountID + "\x00" + strings.ToLower(item.Protocol)
		if _, exists := seen[key]; exists { h.invalid(w, "snapshot contains a duplicate account and protocol"); return }
		seen[key] = struct{}{}
	}
	result, err := h.repo.ReplaceSnapshot(r.Context(), agents.HashToken(token), SnapshotInput{ObservedAt: observedAt, Items: request.Items})
	if errors.Is(err, ErrUnauthorizedAgent) { h.unauthorized(w); return }
	if errors.Is(err, ErrVPNAccountNotFound) { h.invalid(w, "snapshot references an unknown, inactive, or foreign VPN account"); return }
	if err != nil { h.logger.Error("client presence snapshot failed", "error", err); httpx.WriteJSON(w, 500, httpx.Error("database_error", "Failed to store client presence.")); return }
	httpx.WriteJSON(w, http.StatusAccepted, result)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if value := r.URL.Query().Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 500 { h.invalid(w, "limit must be between 1 and 500"); return }
		limit = parsed
	}
	result, err := h.repo.List(r.Context(), h.now().UTC(), limit)
	if err != nil { h.logger.Error("list client connections failed", "error", err); httpx.WriteJSON(w, 500, httpx.Error("database_error", "Failed to load client connections.")); return }
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) invalid(w http.ResponseWriter, message string) { httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_request", message)) }
func (h *Handler) unauthorized(w http.ResponseWriter) { httpx.WriteJSON(w, http.StatusUnauthorized, httpx.Error("unauthorized", "Valid Agent bearer token is required.")) }
