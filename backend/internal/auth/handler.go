package auth

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/httpx"
)

type Handler struct {
	logger *slog.Logger
	repo   *Repository
	ttl    time.Duration
}

func NewHandler(logger *slog.Logger, pool *pgxpool.Pool, ttl time.Duration) *Handler {
	return &Handler{logger: logger, repo: NewRepository(pool), ttl: ttl}
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var request LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_request", "Request body must be valid JSON."))
		return
	}
	login := strings.TrimSpace(request.Login)
	if login == "" {
		login = strings.TrimSpace(request.Email)
	}
	if login == "" || request.Password == "" {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("login_and_password_required", "Login and password are required."))
		return
	}
	response, err := h.repo.Authenticate(r.Context(), login, request.Password, r.UserAgent(), clientIP(r), h.ttl)
	if err != nil {
		if !errors.Is(err, ErrInvalidCredentials) {
			h.logger.Error("login failed", "error", err)
		}
		httpx.WriteJSON(w, http.StatusUnauthorized, httpx.Error("invalid_credentials", "Invalid login or password."))
		return
	}
	h.logger.Info("login accepted", "user_id", response.User.ID)
	httpx.WriteJSON(w, http.StatusOK, response)
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		httpx.WriteJSON(w, http.StatusUnauthorized, httpx.Error("unauthorized", "Authentication is required."))
		return
	}
	if err := h.repo.RevokeSession(r.Context(), user.SessionID); err != nil {
		h.logger.Error("logout failed", "error", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Failed to log out."))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, StatusResponse{Status: "ok", Timestamp: time.Now().UTC()})
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		httpx.WriteJSON(w, http.StatusUnauthorized, httpx.Error("unauthorized", "Authentication is required."))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, MeResponse{User: user.UserProfile})
}

func clientIP(r *http.Request) string {
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		return strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
