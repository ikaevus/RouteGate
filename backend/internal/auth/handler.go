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

	"github.com/ikaevus/routegate/backend/internal/audit"
	"github.com/ikaevus/routegate/backend/internal/httpx"
)

type Handler struct {
	logger *slog.Logger
	repo   *Repository
	audit  *audit.Recorder
	ttl    time.Duration
}

func NewHandler(logger *slog.Logger, pool *pgxpool.Pool, ttl time.Duration) *Handler {
	return &Handler{logger: logger, repo: NewRepository(pool), audit: audit.NewRecorder(logger, pool), ttl: ttl}
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var request LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		h.recordLoginFailure(r, "", "invalid_request")
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_request", "Request body must be valid JSON."))
		return
	}
	login := strings.TrimSpace(request.Login)
	if login == "" {
		login = strings.TrimSpace(request.Email)
	}
	if login == "" || request.Password == "" {
		h.recordLoginFailure(r, login, "login_and_password_required")
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("login_and_password_required", "Login and password are required."))
		return
	}
	response, err := h.repo.Authenticate(r.Context(), login, request.Password, r.UserAgent(), clientIP(r), h.ttl)
	if err != nil {
		if !errors.Is(err, ErrInvalidCredentials) {
			h.logger.Error("login failed", "error", err)
		}
		h.recordLoginFailure(r, login, "invalid_credentials")
		httpx.WriteJSON(w, http.StatusUnauthorized, httpx.Error("invalid_credentials", "Invalid login or password."))
		return
	}
	h.recordLoginSuccess(r, login, response.User.ID, response.User.Email)
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

func (h *Handler) recordLoginSuccess(r *http.Request, login string, userID string, email string) {
	h.audit.RecordSafe(r.Context(), audit.EventInput{
		ActorUserID:  userID,
		ActorType:    audit.ActorTypeUser,
		Action:       "auth.login.success",
		ResourceType: "auth_session",
		Result:       audit.ResultSuccess,
		Metadata: map[string]any{
			"login":      login,
			"email":      email,
			"ip_address": clientIP(r),
			"user_agent": r.UserAgent(),
		},
	})
}

func (h *Handler) recordLoginFailure(r *http.Request, login string, reason string) {
	h.audit.RecordSafe(r.Context(), audit.EventInput{
		ActorType:    audit.ActorTypeAnonymous,
		Action:       "auth.login.failure",
		ResourceType: "auth_session",
		Result:       audit.ResultFailure,
		Metadata: map[string]any{
			"login":      strings.TrimSpace(login),
			"reason":     reason,
			"ip_address": clientIP(r),
			"user_agent": r.UserAgent(),
		},
	})
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
