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

type recoveryPreparedReleaseTransport interface {
	AcceptRecoveryPreparedRelease(context.Context, string, string, []byte) (json.RawMessage, error)
	VerifyRecoveryPreparedRelease(context.Context, string, string, []byte) (json.RawMessage, error)
}

type recoveryRetentionTransport interface {
	AcceptRecoveryRetention(context.Context, string, string, []byte) (json.RawMessage, error)
	VerifyRecoveryRetention(context.Context, string, string, []byte) (json.RawMessage, error)
}

func (h *Handler) RecoveryRetention(w http.ResponseWriter, r *http.Request) {
	h.recoveryRetention(w, r, false)
}
func (h *Handler) VerifyRecoveryRetention(w http.ResponseWriter, r *http.Request) {
	h.recoveryRetention(w, r, true)
}
func (h *Handler) recoveryRetention(w http.ResponseWriter, r *http.Request, verify bool) {
	var call func(context.Context, string, string, []byte) (json.RawMessage, error)
	if t, ok := h.recovery.(recoveryRetentionTransport); ok {
		if verify {
			call = t.VerifyRecoveryRetention
		} else {
			call = t.AcceptRecoveryRetention
		}
	}
	h.recoveryEvidence(w, r, call)
}

func (h *Handler) RecoveryPreparedRelease(w http.ResponseWriter, r *http.Request) {
	h.recoveryPreparedRelease(w, r, false)
}
func (h *Handler) VerifyRecoveryPreparedRelease(w http.ResponseWriter, r *http.Request) {
	h.recoveryPreparedRelease(w, r, true)
}
func (h *Handler) recoveryPreparedRelease(w http.ResponseWriter, r *http.Request, verify bool) {
	var call func(context.Context, string, string, []byte) (json.RawMessage, error)
	if t, ok := h.recovery.(recoveryPreparedReleaseTransport); ok {
		if verify {
			call = t.VerifyRecoveryPreparedRelease
		} else {
			call = t.AcceptRecoveryPreparedRelease
		}
	}
	h.recoveryEvidence(w, r, call)
}

type recoveryTerminalTransport interface {
	AcceptRecoveryTerminal(context.Context, string, string, []byte) (json.RawMessage, error)
	VerifyRecoveryTerminal(context.Context, string, string, []byte) (json.RawMessage, error)
}

func (h *Handler) RecoveryTerminal(w http.ResponseWriter, r *http.Request) {
	h.recoveryTerminal(w, r, false)
}
func (h *Handler) VerifyRecoveryTerminal(w http.ResponseWriter, r *http.Request) {
	h.recoveryTerminal(w, r, true)
}
func (h *Handler) recoveryTerminal(w http.ResponseWriter, r *http.Request, verify bool) {
	var call func(context.Context, string, string, []byte) (json.RawMessage, error)
	if t, ok := h.recovery.(recoveryTerminalTransport); ok {
		if verify {
			call = t.VerifyRecoveryTerminal
		} else {
			call = t.AcceptRecoveryTerminal
		}
	}
	h.recoveryEvidence(w, r, call)
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
	h.recoveryEvidence(w, r, call)
}

func (h *Handler) recoveryEvidence(w http.ResponseWriter, r *http.Request, call func(context.Context, string, string, []byte) (json.RawMessage, error)) {
	w.Header().Set("Cache-Control", "no-store")
	token, ok := agentBearerToken(r.Header.Get("Authorization"))
	if !ok {
		writeAgentUnauthorized(w)
		return
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
