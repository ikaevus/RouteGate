package setup

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/audit"
	"github.com/ikaevus/routegate/backend/internal/auth"
	"github.com/ikaevus/routegate/backend/internal/httpx"
)

const (
	initialSetupTTL  = 30 * time.Minute
	minPasswordBytes = 12
	maxPasswordBytes = 128
	maxRequestBytes  = 16 * 1024
)

type Handler struct {
	logger     *slog.Logger
	pool       *pgxpool.Pool
	authRepo   *auth.Repository
	audit      *audit.Recorder
	sessionTTL time.Duration
}

type createTokenResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

type setupTokenRequest struct {
	Token string `json:"token"`
}

type setupInspectResponse struct {
	Email     string    `json:"email"`
	ExpiresAt time.Time `json:"expires_at"`
}

type completeSetupRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func NewHandler(logger *slog.Logger, pool *pgxpool.Pool, sessionTTL time.Duration) *Handler {
	return &Handler{
		logger:     logger,
		pool:       pool,
		authRepo:   auth.NewRepository(pool),
		audit:      audit.NewRecorder(logger, pool),
		sessionTTL: sessionTTL,
	}
}

func (h *Handler) CreateToken(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		httpx.WriteJSON(w, http.StatusUnauthorized, httpx.Error("unauthorized", "Authentication is required."))
		return
	}

	token, err := randomToken()
	if err != nil {
		h.internalError(w, "generate initial setup token", err)
		return
	}
	expiresAt := time.Now().UTC().Add(initialSetupTTL)

	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		h.internalError(w, "begin initial setup token transaction", err)
		return
	}
	defer tx.Rollback(r.Context())

	var completed bool
	err = tx.QueryRow(
		r.Context(),
		`SELECT initial_setup_completed_at IS NOT NULL FROM users WHERE id=$1 FOR UPDATE`,
		user.ID,
	).Scan(&completed)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteJSON(w, http.StatusUnauthorized, httpx.Error("unauthorized", "Authentication is required."))
		return
	}
	if err != nil {
		h.internalError(w, "load initial setup state", err)
		return
	}
	if completed {
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("initial_setup_completed", "Initial setup has already been completed."))
		return
	}

	if _, err := tx.Exec(
		r.Context(),
		`UPDATE initial_setup_tokens SET used_at=now() WHERE user_id=$1 AND used_at IS NULL`,
		user.ID,
	); err != nil {
		h.internalError(w, "invalidate previous initial setup tokens", err)
		return
	}

	if _, err := tx.Exec(
		r.Context(),
		`INSERT INTO initial_setup_tokens (user_id, token_hash, expires_at) VALUES ($1,$2,$3)`,
		user.ID,
		auth.TokenHash(token),
		expiresAt,
	); err != nil {
		h.internalError(w, "store initial setup token", err)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		h.internalError(w, "commit initial setup token", err)
		return
	}

	h.audit.RecordSafe(r.Context(), audit.EventInput{
		ActorUserID:  user.ID,
		ActorType:    audit.ActorTypeUser,
		Action:       "auth.initial_setup.token_created",
		ResourceType: "user",
		ResourceID:   user.ID,
		Result:       audit.ResultSuccess,
		Metadata: map[string]any{
			"expires_at": expiresAt,
			"ip_address": clientIP(r),
		},
	})

	httpx.WriteJSON(w, http.StatusCreated, createTokenResponse{Token: token, ExpiresAt: expiresAt})
}

func (h *Handler) Inspect(w http.ResponseWriter, r *http.Request) {
	var request setupTokenRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	token := strings.TrimSpace(request.Token)
	if !validTokenShape(token) {
		h.invalidToken(w)
		return
	}

	var response setupInspectResponse
	err := h.pool.QueryRow(
		r.Context(),
		`SELECT u.email, t.expires_at
		 FROM initial_setup_tokens t
		 JOIN users u ON u.id=t.user_id
		 WHERE t.token_hash=$1
		   AND t.used_at IS NULL
		   AND t.expires_at>now()
		   AND u.status='active'
		   AND u.initial_setup_completed_at IS NULL`,
		auth.TokenHash(token),
	).Scan(&response.Email, &response.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		h.invalidToken(w)
		return
	}
	if err != nil {
		h.internalError(w, "inspect initial setup token", err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, response)
}

func (h *Handler) Complete(w http.ResponseWriter, r *http.Request) {
	var request completeSetupRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	token := strings.TrimSpace(request.Token)
	if !validTokenShape(token) {
		h.invalidToken(w)
		return
	}
	if message := validatePassword(request.NewPassword); message != "" {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_password", message))
		return
	}

	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		h.internalError(w, "begin initial setup transaction", err)
		return
	}
	defer tx.Rollback(r.Context())

	var userID, email, status string
	var expiresAt time.Time
	var used, completed bool
	err = tx.QueryRow(
		r.Context(),
		`SELECT t.user_id::text, u.email, u.status, t.expires_at,
		        t.used_at IS NOT NULL, u.initial_setup_completed_at IS NOT NULL
		 FROM initial_setup_tokens t
		 JOIN users u ON u.id=t.user_id
		 WHERE t.token_hash=$1
		 FOR UPDATE OF t, u`,
		auth.TokenHash(token),
	).Scan(&userID, &email, &status, &expiresAt, &used, &completed)
	if errors.Is(err, pgx.ErrNoRows) {
		h.invalidToken(w)
		return
	}
	if err != nil {
		h.internalError(w, "lock initial setup token", err)
		return
	}
	if used || completed || status != "active" || !expiresAt.After(time.Now().UTC()) {
		h.invalidToken(w)
		return
	}

	// Perform the expensive password derivation only after the high-entropy token
	// has been validated. This keeps random unauthenticated requests inexpensive.
	passwordHash, err := auth.HashPassword(request.NewPassword)
	if err != nil {
		h.internalError(w, "hash initial password", err)
		return
	}
	sessionToken, err := randomToken()
	if err != nil {
		h.internalError(w, "generate initial session token", err)
		return
	}
	sessionExpiresAt := time.Now().UTC().Add(h.sessionTTL)

	if _, err := tx.Exec(
		r.Context(),
		`UPDATE users
		 SET password_hash=$2, initial_setup_completed_at=now(), last_login_at=now(), updated_at=now()
		 WHERE id=$1`,
		userID,
		passwordHash,
	); err != nil {
		h.internalError(w, "complete initial password setup", err)
		return
	}
	if _, err := tx.Exec(
		r.Context(),
		`UPDATE initial_setup_tokens SET used_at=now() WHERE token_hash=$1`,
		auth.TokenHash(token),
	); err != nil {
		h.internalError(w, "consume initial setup token", err)
		return
	}
	if _, err := tx.Exec(
		r.Context(),
		`UPDATE auth_sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`,
		userID,
	); err != nil {
		h.internalError(w, "revoke bootstrap sessions", err)
		return
	}
	if _, err := tx.Exec(
		r.Context(),
		`INSERT INTO auth_sessions (user_id,token_hash,expires_at,user_agent,ip_address)
		 VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''))`,
		userID,
		auth.TokenHash(sessionToken),
		sessionExpiresAt,
		r.UserAgent(),
		clientIP(r),
	); err != nil {
		h.internalError(w, "create post-setup session", err)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		h.internalError(w, "commit initial setup", err)
		return
	}

	profile, err := h.authRepo.ProfileByID(r.Context(), userID)
	if err != nil {
		h.internalError(w, "load activated user profile", err)
		return
	}

	h.audit.RecordSafe(r.Context(), audit.EventInput{
		ActorUserID:  userID,
		ActorType:    audit.ActorTypeUser,
		Action:       "auth.initial_setup.completed",
		ResourceType: "user",
		ResourceID:   userID,
		Result:       audit.ResultSuccess,
		Metadata: map[string]any{
			"email":      email,
			"ip_address": clientIP(r),
			"user_agent": r.UserAgent(),
		},
	})

	httpx.WriteJSON(w, http.StatusOK, auth.LoginResponse{
		Token:     sessionToken,
		ExpiresAt: sessionExpiresAt,
		User:      profile,
	})
}

func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		httpx.WriteJSON(w, http.StatusUnauthorized, httpx.Error("unauthorized", "Authentication is required."))
		return
	}

	var request changePasswordRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	if message := validatePassword(request.NewPassword); message != "" {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_password", message))
		return
	}

	var currentHash string
	err := h.pool.QueryRow(
		r.Context(),
		`SELECT COALESCE(password_hash,'') FROM users WHERE id=$1 AND status='active'`,
		user.ID,
	).Scan(&currentHash)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteJSON(w, http.StatusUnauthorized, httpx.Error("invalid_current_password", "The current password is incorrect."))
		return
	}
	if err != nil {
		h.internalError(w, "load current password hash", err)
		return
	}
	if !auth.VerifyPassword(currentHash, request.CurrentPassword) {
		httpx.WriteJSON(w, http.StatusUnauthorized, httpx.Error("invalid_current_password", "The current password is incorrect."))
		return
	}
	if auth.VerifyPassword(currentHash, request.NewPassword) {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("password_unchanged", "The new password must be different from the current password."))
		return
	}

	newHash, err := auth.HashPassword(request.NewPassword)
	if err != nil {
		h.internalError(w, "hash replacement password", err)
		return
	}

	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		h.internalError(w, "begin password change", err)
		return
	}
	defer tx.Rollback(r.Context())

	if _, err := tx.Exec(
		r.Context(),
		`UPDATE users SET password_hash=$2, updated_at=now() WHERE id=$1`,
		user.ID,
		newHash,
	); err != nil {
		h.internalError(w, "update password", err)
		return
	}
	if _, err := tx.Exec(
		r.Context(),
		`UPDATE auth_sessions
		 SET revoked_at=now()
		 WHERE user_id=$1 AND id<>$2 AND revoked_at IS NULL`,
		user.ID,
		user.SessionID,
	); err != nil {
		h.internalError(w, "revoke other sessions", err)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		h.internalError(w, "commit password change", err)
		return
	}

	h.audit.RecordSafe(r.Context(), audit.EventInput{
		ActorUserID:  user.ID,
		ActorType:    audit.ActorTypeUser,
		Action:       "auth.password.changed",
		ResourceType: "user",
		ResourceID:   user.ID,
		Result:       audit.ResultSuccess,
		Metadata: map[string]any{
			"ip_address": clientIP(r),
			"user_agent": r.UserAgent(),
		},
	})

	httpx.WriteJSON(w, http.StatusOK, auth.StatusResponse{Status: "ok", Timestamp: time.Now().UTC()})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_request", "Request body must be valid JSON."))
		return err
	}
	return nil
}

func validatePassword(password string) string {
	length := len([]byte(password))
	if length < minPasswordBytes {
		return "Password must be at least 12 characters long."
	}
	if length > maxPasswordBytes {
		return "Password must not exceed 128 characters."
	}
	if strings.TrimSpace(password) == "" {
		return "Password cannot contain only whitespace."
	}
	return ""
}

func validTokenShape(token string) bool {
	return len(token) >= 32 && len(token) <= 256 && !strings.ContainsAny(token, " \t\r\n")
}

func randomToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func (h *Handler) invalidToken(w http.ResponseWriter) {
	httpx.WriteJSON(w, http.StatusGone, httpx.Error("initial_setup_token_invalid", "This setup link is invalid, expired, or has already been used."))
}

func (h *Handler) internalError(w http.ResponseWriter, action string, err error) {
	h.logger.Error(action+" failed", "error", err)
	httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("internal_error", "The operation could not be completed."))
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
