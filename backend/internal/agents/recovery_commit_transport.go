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

type recoveryFinalizationTransport interface {
	AcceptRecoveryFinalization(context.Context, string, string, []byte) (json.RawMessage, error)
	VerifyRecoveryFinalization(context.Context, string, string, []byte) (json.RawMessage, error)
}

func (h *Handler) RecoveryCommit(w http.ResponseWriter, r *http.Request) {
	h.recoveryCommit(w, r, false, false)
}
func (h *Handler) VerifyRecoveryCommit(w http.ResponseWriter, r *http.Request) {
	h.recoveryCommit(w, r, true, false)
}

func (h *Handler) RecoveryFinalization(w http.ResponseWriter, r *http.Request) {
	h.recoveryCommit(w, r, false, true)
}
func (h *Handler) VerifyRecoveryFinalization(w http.ResponseWriter, r *http.Request) {
	h.recoveryCommit(w, r, true, true)
}

// Agent bearer authentication is shared with existing evidence/result routes.
// Neither recording nor readback authorizes Agent mutation/fence release.
func (h *Handler) recoveryCommit(w http.ResponseWriter, r *http.Request, verify, final bool) {
	w.Header().Set("Cache-Control", "no-store")
	token, ok := agentBearerToken(r.Header.Get("Authorization"))
	if !ok {
		writeAgentUnauthorized(w)
		return
	}
	var call func(context.Context, string, string, []byte) (json.RawMessage, error)
	if final {
		if t, supported := h.recovery.(recoveryFinalizationTransport); supported {
			if verify {
				call = t.VerifyRecoveryFinalization
			} else {
				call = t.AcceptRecoveryFinalization
			}
		}
	} else {
		if t, supported := h.recovery.(recoveryCommitTransport); supported {
			if verify {
				call = t.VerifyRecoveryCommit
			} else {
				call = t.AcceptRecoveryCommit
			}
		}
	}
	if call == nil {
		httpx.WriteJSON(w, 501, httpx.Error("unsupported", "Commit evidence is unavailable."))
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8192))
	if err != nil || !json.Valid(data) {
		httpx.WriteJSON(w, 400, httpx.Error("invalid_request", "A bounded commit proof is required."))
		return
	}
	result, err := call(r.Context(), HashToken(token), r.PathValue("operation_id"), data)
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
