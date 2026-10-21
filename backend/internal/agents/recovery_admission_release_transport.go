package agents

import (
	"context"
	"encoding/json"
	"net/http"
)

// Agent-authenticated transport only. The returned receipt is historical storage,
// not Manager reservation completion or runtime credential revocation.
type recoveryAdmissionReleaseTransport interface {
	AcceptRecoveryAdmissionRelease(context.Context, string, string, []byte) (json.RawMessage, error)
	VerifyRecoveryAdmissionRelease(context.Context, string, string, []byte) (json.RawMessage, error)
}

func (h *Handler) RecoveryAdmissionRelease(w http.ResponseWriter, r *http.Request) {
	h.recoveryAdmissionRelease(w, r, false)
}
func (h *Handler) VerifyRecoveryAdmissionRelease(w http.ResponseWriter, r *http.Request) {
	h.recoveryAdmissionRelease(w, r, true)
}
func (h *Handler) recoveryAdmissionRelease(w http.ResponseWriter, r *http.Request, verify bool) {
	var call func(context.Context, string, string, []byte) (json.RawMessage, error)
	if t, ok := h.recovery.(recoveryAdmissionReleaseTransport); ok {
		if verify {
			call = t.VerifyRecoveryAdmissionRelease
		} else {
			call = t.AcceptRecoveryAdmissionRelease
		}
	}
	h.recoveryEvidence(w, r, call)
}
