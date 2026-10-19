package agents

import (
	"context"
	"encoding/json"
	"net/http"
)

type recoveryCleanedCommitTransport interface {
	AcceptRecoveryCleanedCommit(context.Context, string, string, []byte) (json.RawMessage, error)
	VerifyRecoveryCleanedCommit(context.Context, string, string, []byte) (json.RawMessage, error)
}

func (h *Handler) RecoveryCleanedCommit(w http.ResponseWriter, r *http.Request) {
	h.recoveryCleanedCommit(w, r, false)
}
func (h *Handler) VerifyRecoveryCleanedCommit(w http.ResponseWriter, r *http.Request) {
	h.recoveryCleanedCommit(w, r, true)
}
func (h *Handler) recoveryCleanedCommit(w http.ResponseWriter, r *http.Request, verify bool) {
	var call func(context.Context, string, string, []byte) (json.RawMessage, error)
	if t, ok := h.recovery.(recoveryCleanedCommitTransport); ok {
		if verify {
			call = t.VerifyRecoveryCleanedCommit
		} else {
			call = t.AcceptRecoveryCleanedCommit
		}
	}
	h.recoveryEvidence(w, r, call)
}
