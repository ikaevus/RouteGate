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

var ErrObservationRejected = errors.New("recovery observation rejected")

// Injected by the HTTP composition root to avoid coupling Agent repositories
// to config rendering/revocation services. Payloads are typed by that service.
type RecoveryTransport interface {
	NextRecoveryChallenge(context.Context, string) (json.RawMessage, error)
	AcceptRecoveryEvidence(context.Context, string, string, []byte) (json.RawMessage, error)
}

func (h *Handler) SetRecoveryTransport(transport RecoveryTransport) {
	h.recovery = transport
}

func (h *Handler) RecoveryEvidence(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	token, ok := agentBearerToken(r.Header.Get("Authorization"))
	if !ok {
		writeAgentUnauthorized(w)
		return
	}
	if h.recovery == nil {
		httpx.WriteJSON(w, http.StatusNotImplemented, httpx.Error("unsupported", "Observation evidence is unavailable."))
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8192))
	if err != nil || !json.Valid(data) {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_request", "A bounded observation response is required."))
		return
	}
	ack, err := h.recovery.AcceptRecoveryEvidence(r.Context(), HashToken(token), r.PathValue("operation_id"), data)
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, ErrObservationRejected) {
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("observation_rejected", "Observation evidence was not accepted."))
		return
	}
	if err != nil {
		// Database errors may contain evidence. Never log raw errors or bodies.
		h.logger.Error("accept recovery observation failed")
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Observation evidence was not accepted."))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ack)
}
