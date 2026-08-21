package auth

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ikaevus/routegate/backend/internal/audit"
	"github.com/ikaevus/routegate/backend/internal/httpx"
)

type SecuritySession struct {
	ID         string     `json:"id"`
	Current    bool       `json:"current"`
	IPAddress  string     `json:"ip_address,omitempty"`
	UserAgent  string     `json:"user_agent,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt  time.Time  `json:"expires_at"`
}

type SecuritySessionsResponse struct {
	Sessions []SecuritySession `json:"sessions"`
}

type SecurityEvent struct {
	ID        string         `json:"id"`
	Action    string         `json:"action"`
	Result    string         `json:"result"`
	Metadata  map[string]any `json:"metadata"`
	CreatedAt time.Time      `json:"created_at"`
}

type SecurityEventsResponse struct {
	Events []SecurityEvent `json:"events"`
}

func (r *Repository) SecuritySessions(ctx context.Context, userID, currentSessionID string) ([]SecuritySession, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text,
		       COALESCE(ip_address::text, ''),
		       COALESCE(user_agent, ''),
		       created_at,
		       last_used_at,
		       expires_at
		FROM auth_sessions
		WHERE user_id=$1
		  AND revoked_at IS NULL
		  AND expires_at > now()
		ORDER BY COALESCE(last_used_at, created_at) DESC, created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sessions := []SecuritySession{}
	for rows.Next() {
		var session SecuritySession
		if err := rows.Scan(
			&session.ID,
			&session.IPAddress,
			&session.UserAgent,
			&session.CreatedAt,
			&session.LastUsedAt,
			&session.ExpiresAt,
		); err != nil {
			return nil, err
		}
		session.Current = session.ID == currentSessionID
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

func (r *Repository) RevokeOtherSecuritySession(ctx context.Context, userID, currentSessionID, targetSessionID string) (bool, error) {
	result, err := r.pool.Exec(ctx, `
		UPDATE auth_sessions
		SET revoked_at=now()
		WHERE id=$1
		  AND user_id=$2
		  AND id<>$3
		  AND revoked_at IS NULL
		  AND expires_at > now()
	`, targetSessionID, userID, currentSessionID)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() == 1, nil
}

func (r *Repository) RevokeOtherSecuritySessions(ctx context.Context, userID, currentSessionID string) (int64, error) {
	result, err := r.pool.Exec(ctx, `
		UPDATE auth_sessions
		SET revoked_at=now()
		WHERE user_id=$1
		  AND id<>$2
		  AND revoked_at IS NULL
		  AND expires_at > now()
	`, userID, currentSessionID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

func (r *Repository) SecurityEvents(ctx context.Context, userID string, limit int) ([]SecurityEvent, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows, err := r.pool.Query(ctx, `
		SELECT e.id::text, e.action, e.result, e.metadata, e.created_at
		FROM audit_events e
		WHERE e.actor_user_id=$1
		  AND e.action LIKE 'auth.%'
		  AND e.created_at > COALESCE(
		      (SELECT visibility.cleared_before
		       FROM auth_security_event_visibility visibility
		       WHERE visibility.user_id=$1),
		      '-infinity'::timestamptz
		  )
		ORDER BY e.created_at DESC
		LIMIT $2
	`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := []SecurityEvent{}
	for rows.Next() {
		var event SecurityEvent
		if err := rows.Scan(&event.ID, &event.Action, &event.Result, &event.Metadata, &event.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (r *Repository) ClearSecurityEventHistory(ctx context.Context, userID string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO auth_security_event_visibility (user_id, cleared_before, updated_at)
		VALUES ($1, now(), now())
		ON CONFLICT (user_id) DO UPDATE
		SET cleared_before=EXCLUDED.cleared_before,
		    updated_at=EXCLUDED.updated_at
	`, userID)
	return err
}

func (h *Handler) ListSecuritySessions(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		httpx.WriteJSON(w, http.StatusUnauthorized, httpx.Error("unauthorized", "Authentication is required."))
		return
	}
	sessions, err := h.repo.SecuritySessions(r.Context(), user.ID, user.SessionID)
	if err != nil {
		h.logger.Error("list security sessions failed", "error", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Failed to load active sessions."))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, SecuritySessionsResponse{Sessions: sessions})
}

func (h *Handler) RevokeSecuritySession(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		httpx.WriteJSON(w, http.StatusUnauthorized, httpx.Error("unauthorized", "Authentication is required."))
		return
	}
	targetSessionID := r.PathValue("session_id")
	if targetSessionID == "" || targetSessionID == user.SessionID {
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("current_session_protected", "Use logout to end the current session."))
		return
	}
	revoked, err := h.repo.RevokeOtherSecuritySession(r.Context(), user.ID, user.SessionID, targetSessionID)
	if err != nil {
		h.logger.Error("revoke security session failed", "error", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Failed to revoke session."))
		return
	}
	if !revoked {
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
	httpx.WriteJSON(w, http.StatusOK, StatusResponse{Status: "ok", Timestamp: time.Now().UTC()})
}

func (h *Handler) RevokeOtherSecuritySessions(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		httpx.WriteJSON(w, http.StatusUnauthorized, httpx.Error("unauthorized", "Authentication is required."))
		return
	}
	count, err := h.repo.RevokeOtherSecuritySessions(r.Context(), user.ID, user.SessionID)
	if err != nil {
		h.logger.Error("revoke other security sessions failed", "error", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Failed to revoke other sessions."))
		return
	}
	h.recordAudit(r, audit.EventInput{
		ActorUserID:  user.ID,
		ActorType:    audit.ActorTypeUser,
		Action:       "auth.sessions.revoked_others",
		ResourceType: "auth_session",
		Result:       audit.ResultSuccess,
		Metadata: map[string]any{
			"revoked_count": count,
			"ip_address":    clientIP(r),
			"user_agent":    r.UserAgent(),
		},
	})
	httpx.WriteJSON(w, http.StatusOK, StatusResponse{Status: "ok", Timestamp: time.Now().UTC()})
}

func (h *Handler) ListSecurityEvents(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		httpx.WriteJSON(w, http.StatusUnauthorized, httpx.Error("unauthorized", "Authentication is required."))
		return
	}
	events, err := h.repo.SecurityEvents(r.Context(), user.ID, 20)
	if errors.Is(err, pgx.ErrNoRows) {
		events = []SecurityEvent{}
		err = nil
	}
	if err != nil {
		h.logger.Error("list security events failed", "error", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Failed to load security events."))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, SecurityEventsResponse{Events: events})
}

func (h *Handler) ClearSecurityEvents(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		httpx.WriteJSON(w, http.StatusUnauthorized, httpx.Error("unauthorized", "Authentication is required."))
		return
	}
	if err := h.repo.ClearSecurityEventHistory(r.Context(), user.ID); err != nil {
		h.logger.Error("clear security event history failed", "error", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Failed to clear security event history."))
		return
	}
	h.recordAudit(r, audit.EventInput{
		ActorUserID:  user.ID,
		ActorType:    audit.ActorTypeUser,
		Action:       "security.events.cleared",
		ResourceType: "user",
		ResourceID:   user.ID,
		Result:       audit.ResultSuccess,
		Metadata: map[string]any{
			"ip_address": clientIP(r),
			"user_agent": r.UserAgent(),
		},
	})
	httpx.WriteJSON(w, http.StatusOK, StatusResponse{Status: "ok", Timestamp: time.Now().UTC()})
}
