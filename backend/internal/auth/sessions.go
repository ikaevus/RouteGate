package auth

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/ikaevus/routegate/backend/internal/audit"
	"github.com/ikaevus/routegate/backend/internal/httpx"
)

type SessionInfo struct {
	ID         string    `json:"id"`
	Current    bool      `json:"current"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt time.Time `json:"last_used_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	UserAgent  string    `json:"user_agent,omitempty"`
	IPAddress  string    `json:"ip_address,omitempty"`
}

type SessionsResponse struct {
	Items []SessionInfo `json:"items"`
}

type RevokeOtherSessionsResponse struct {
	Revoked int64 `json:"revoked"`
}

func (r *Repository) ActiveSessions(ctx context.Context, userID, currentSessionID string) ([]SessionInfo, error) {
	rows, err := r.pool.Query(
		ctx,
		`
		SELECT
			id::text,
			id::text = $2,
			created_at,
			COALESCE(last_used_at, created_at),
			expires_at,
			COALESCE(user_agent, ''),
			COALESCE(ip_address, '')
		FROM auth_sessions
		WHERE user_id = $1
		  AND revoked_at IS NULL
		  AND expires_at > now()
		ORDER BY
			(id::text = $2) DESC,
			COALESCE(last_used_at, created_at) DESC,
			created_at DESC
		`,
		userID,
		currentSessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sessions := make([]SessionInfo, 0)
	for rows.Next() {
		var session SessionInfo
		if err := rows.Scan(
			&session.ID,
			&session.Current,
			&session.CreatedAt,
			&session.LastUsedAt,
			&session.ExpiresAt,
			&session.UserAgent,
			&session.IPAddress,
		); err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return sessions, nil
}

func (r *Repository) RevokeOwnedOtherSession(ctx context.Context, userID, currentSessionID, targetSessionID string) (bool, error) {
	if isCurrentSession(currentSessionID, targetSessionID) {
		return false, nil
	}

	commandTag, err := r.pool.Exec(
		ctx,
		`
		UPDATE auth_sessions
		SET revoked_at = now()
		WHERE user_id = $1
		  AND id::text = $2
		  AND id::text <> $3
		  AND revoked_at IS NULL
		  AND expires_at > now()
		`,
		userID,
		targetSessionID,
		currentSessionID,
	)
	if err != nil {
		return false, err
	}

	return commandTag.RowsAffected() == 1, nil
}

func (r *Repository) RevokeOtherSessions(ctx context.Context, userID, currentSessionID string) (int64, error) {
	commandTag, err := r.pool.Exec(
		ctx,
		`
		UPDATE auth_sessions
		SET revoked_at = now()
		WHERE user_id = $1
		  AND id::text <> $2
		  AND revoked_at IS NULL
		  AND expires_at > now()
		`,
		userID,
		currentSessionID,
	)
	if err != nil {
		return 0, err
	}

	return commandTag.RowsAffected(), nil
}

func (h *Handler) ListSessions(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		httpx.WriteJSON(w, http.StatusUnauthorized, httpx.Error("unauthorized", "Authentication is required."))
		return
	}

	sessions, err := h.repo.ActiveSessions(r.Context(), user.ID, user.SessionID)
	if err != nil {
		h.logger.Error("list active sessions failed", "user_id", user.ID, "error", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Failed to load active sessions."))
		return
	}

	httpx.WriteJSON(w, http.StatusOK, SessionsResponse{Items: sessions})
}

func (h *Handler) RevokeOtherSession(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		httpx.WriteJSON(w, http.StatusUnauthorized, httpx.Error("unauthorized", "Authentication is required."))
		return
	}

	targetSessionID := strings.TrimSpace(r.PathValue("session_id"))
	if targetSessionID == "" {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("session_id_required", "Session ID is required."))
		return
	}
	if isCurrentSession(user.SessionID, targetSessionID) {
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("current_session", "The current session cannot be revoked from this action."))
		return
	}

	revoked, err := h.repo.RevokeOwnedOtherSession(r.Context(), user.ID, user.SessionID, targetSessionID)
	if err != nil {
		h.logger.Error("revoke active session failed", "user_id", user.ID, "session_id", targetSessionID, "error", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Failed to revoke the session."))
		return
	}
	if !revoked {
		// Deliberately do not distinguish an unknown session from a session owned by
		// another user. This avoids leaking session existence across accounts.
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("session_not_found", "Active session was not found."))
		return
	}

	h.recordAudit(r, audit.EventInput{
		ActorUserID:  user.ID,
		ActorType:    audit.ActorTypeUser,
		Action:       "auth.session.revoked",
		ResourceType: "auth_session",
		ResourceID:   targetSessionID,
		Result:       audit.ResultSuccess,
		Metadata: map[string]any{
			"ip_address": clientIP(r),
			"user_agent": r.UserAgent(),
		},
	})

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) RevokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		httpx.WriteJSON(w, http.StatusUnauthorized, httpx.Error("unauthorized", "Authentication is required."))
		return
	}

	revoked, err := h.repo.RevokeOtherSessions(r.Context(), user.ID, user.SessionID)
	if err != nil {
		h.logger.Error("revoke other sessions failed", "user_id", user.ID, "error", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Failed to revoke other sessions."))
		return
	}

	h.recordAudit(r, audit.EventInput{
		ActorUserID:  user.ID,
		ActorType:    audit.ActorTypeUser,
		Action:       "auth.sessions.others_revoked",
		ResourceType: "auth_session",
		ResourceID:   user.SessionID,
		Result:       audit.ResultSuccess,
		Metadata: map[string]any{
			"revoked_count": revoked,
			"ip_address":    clientIP(r),
			"user_agent":    r.UserAgent(),
		},
	})

	httpx.WriteJSON(w, http.StatusOK, RevokeOtherSessionsResponse{Revoked: revoked})
}

func isCurrentSession(currentSessionID, targetSessionID string) bool {
	return strings.TrimSpace(currentSessionID) != "" && strings.TrimSpace(currentSessionID) == strings.TrimSpace(targetSessionID)
}
