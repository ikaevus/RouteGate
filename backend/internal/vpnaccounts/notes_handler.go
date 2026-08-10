package vpnaccounts

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/audit"
	"github.com/ikaevus/routegate/backend/internal/auth"
	"github.com/ikaevus/routegate/backend/internal/httpx"
)

const maxVPNAccountNotesRunes = 4000

type NotesHandler struct {
	logger *slog.Logger
	pool   *pgxpool.Pool
	audit  *audit.Recorder
}

type VPNAccountNotesResponse struct {
	VPNAccountID string    `json:"vpnAccountId"`
	Notes        string    `json:"notes"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type UpdateVPNAccountNotesRequest struct {
	Notes string `json:"notes"`
}

func NewNotesHandler(logger *slog.Logger, pool *pgxpool.Pool) *NotesHandler {
	return &NotesHandler{
		logger: logger,
		pool:   pool,
		audit:  audit.NewRecorder(logger, pool),
	}
}

func (h *NotesHandler) Get(w http.ResponseWriter, r *http.Request) {
	accountID := r.PathValue("id")
	var response VPNAccountNotesResponse

	err := h.pool.QueryRow(r.Context(), `
		SELECT
			a.id::text,
			COALESCE(n.notes, ''),
			COALESCE(n.updated_at, a.created_at)
		FROM vpn_accounts a
		LEFT JOIN vpn_account_notes n ON n.vpn_account_id = a.id
		WHERE a.id = $1::uuid
	`, accountID).Scan(&response.VPNAccountID, &response.Notes, &response.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		writeAccountNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "get vpn account notes", err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, response)
}

func (h *NotesHandler) Update(w http.ResponseWriter, r *http.Request) {
	accountID := r.PathValue("id")
	var request UpdateVPNAccountNotesRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeInvalidRequest(w, "Request body must be valid JSON.")
		return
	}

	notes := strings.TrimSpace(request.Notes)
	if utf8.RuneCountInString(notes) > maxVPNAccountNotesRunes {
		writeInvalidRequest(w, "notes must not exceed 4000 characters")
		return
	}

	var response VPNAccountNotesResponse
	err := h.pool.QueryRow(r.Context(), `
		INSERT INTO vpn_account_notes (vpn_account_id, notes, updated_at)
		SELECT id, $2, now()
		FROM vpn_accounts
		WHERE id = $1::uuid
		ON CONFLICT (vpn_account_id) DO UPDATE
		SET notes = EXCLUDED.notes, updated_at = now()
		RETURNING vpn_account_id::text, notes, updated_at
	`, accountID, notes).Scan(&response.VPNAccountID, &response.Notes, &response.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		writeAccountNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "update vpn account notes", err)
		return
	}

	input := audit.EventInput{
		Action:       "vpn_account.notes_updated",
		ResourceType: "vpn_account",
		ResourceID:   accountID,
		Result:       audit.ResultSuccess,
		Metadata: map[string]any{
			"notes_length": utf8.RuneCountInString(notes),
		},
	}
	if user, ok := auth.UserFromContext(r.Context()); ok {
		input.ActorUserID = user.ID
		input.ActorType = audit.ActorTypeUser
	} else {
		input.ActorType = audit.ActorTypeSystem
	}
	h.audit.RecordSafe(r.Context(), input)

	httpx.WriteJSON(w, http.StatusOK, response)
}

func (h *NotesHandler) databaseError(w http.ResponseWriter, operation string, err error) {
	h.logger.Error(operation+" failed", "error", err)
	httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Database operation failed."))
}
