package agents

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/ikaevus/routegate/backend/internal/httpx"
	"github.com/jackc/pgx/v5"
)

type recoveryCommitTransport interface {
	AcceptRecoveryCommit(context.Context, string, string, []byte) (json.RawMessage, error)
	VerifyRecoveryCommit(context.Context, string, string, []byte) (json.RawMessage, error)
}

func (h *Handler) RecoveryCommit(w http.ResponseWriter, r *http.Request) {
	h.recoveryCommit(w, r, false)
}
func (h *Handler) VerifyRecoveryCommit(w http.ResponseWriter, r *http.Request) {
	h.recoveryCommit(w, r, true)
}

// Agent bearer authentication is shared with existing evidence/result routes.
// Neither recording nor readback authorizes Agent mutation/fence release.
func (h *Handler) recoveryCommit(w http.ResponseWriter, r *http.Request, verify bool) {
	w.Header().Set("Cache-Control", "no-store")
	token, ok := agentBearerToken(r.Header.Get("Authorization"))
	if !ok {
		writeAgentUnauthorized(w)
		return
	}
	t, ok := h.recovery.(recoveryCommitTransport)
	if !ok {
		httpx.WriteJSON(w, 501, httpx.Error("unsupported", "Commit evidence is unavailable."))
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8192))
	if err != nil || !json.Valid(data) {
		httpx.WriteJSON(w, 400, httpx.Error("invalid_request", "A bounded commit proof is required."))
		return
	}
	var result json.RawMessage
	if verify {
		result, err = t.VerifyRecoveryCommit(r.Context(), HashToken(token), r.PathValue("operation_id"), data)
	} else {
		result, err = t.AcceptRecoveryCommit(r.Context(), HashToken(token), r.PathValue("operation_id"), data)
	}
	if errors.Is(err, ErrObservationRejected) || errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteJSON(w, 409, httpx.Error("commit_evidence_rejected", "Commit evidence was not confirmed."))
		return
	}
	if err != nil {
		h.logger.Error("recovery commit evidence failed")
		httpx.WriteJSON(w, 500, httpx.Error("commit_evidence_failed", "Commit evidence was not confirmed."))
		return
	}
	httpx.WriteJSON(w, 200, result)
}
