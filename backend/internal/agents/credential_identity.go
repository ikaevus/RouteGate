package agents

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"

	"github.com/ikaevus/routegate/backend/internal/httpx"
	"github.com/jackc/pgx/v5"
)

var recoveryIdentityID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
var recoveryIdentityNonce = regexp.MustCompile(`^[0-9a-f]{64}$`)

type CredentialIdentityInput struct {
	SchemaVersion int    `json:"schemaVersion"`
	AgentID       string `json:"agentId"`
	ServerID      string `json:"serverId"`
	Nonce         string `json:"nonce"`
}
type CredentialIdentity struct {
	SchemaVersion             int    `json:"schemaVersion"`
	Verified                  bool   `json:"verified"`
	AgentID                   string `json:"agentId"`
	ServerID                  string `json:"serverId"`
	AgentCredentialGeneration int64  `json:"agentCredentialGeneration"`
	RequestHash               string `json:"requestHash"`
}
type credentialIdentityReader interface {
	ReadCredentialIdentity(context.Context, string, CredentialIdentityInput) (CredentialIdentity, error)
}

// Current bearer identity only: no heartbeat update, task claim or VPN health.
func (r *Repository) ReadCredentialIdentity(ctx context.Context, hash string, in CredentialIdentityInput) (CredentialIdentity, error) {
	var out CredentialIdentity
	err := r.pool.QueryRow(ctx, `SELECT id::text,server_id::text,credential_generation FROM agents WHERE token_hash=$1 AND id=$2::uuid AND server_id=$3::uuid AND credential_generation>0`, hash, in.AgentID, in.ServerID).Scan(&out.AgentID, &out.ServerID, &out.AgentCredentialGeneration)
	return out, err
}

// Bounded flat request parser rejects duplicates, aliases and trailing data.
func decodeCredentialIdentity(data []byte) (CredentialIdentityInput, error) {
	var in CredentialIdentityInput
	d := json.NewDecoder(bytes.NewReader(data))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return in, errors.New("invalid identity request")
	}
	seen := map[string]bool{}
	for d.More() {
		key, e := d.Token()
		k, ok := key.(string)
		if e != nil || !ok || seen[k] {
			return in, errors.New("invalid identity request")
		}
		seen[k] = true
		if k != "schemaVersion" && k != "agentId" && k != "serverId" && k != "nonce" {
			return in, errors.New("invalid identity request")
		}
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return in, errors.New("invalid identity request")
		}
	}
	if _, err = d.Token(); err != nil {
		return in, err
	}
	if _, err = d.Token(); err != io.EOF {
		return in, errors.New("invalid identity request")
	}
	if json.Unmarshal(data, &in) != nil || in.SchemaVersion != 1 || !recoveryIdentityID.MatchString(in.AgentID) || !recoveryIdentityID.MatchString(in.ServerID) || !recoveryIdentityNonce.MatchString(in.Nonce) {
		return CredentialIdentityInput{}, errors.New("invalid identity request")
	}
	return in, nil
}

func (h *Handler) VerifyCredentialIdentity(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	token, ok := agentBearerToken(r.Header.Get("Authorization"))
	if !ok {
		writeAgentUnauthorized(w)
		return
	}
	reader, ok := h.repository.(credentialIdentityReader)
	if !ok {
		httpx.WriteJSON(w, 501, httpx.Error("unsupported", "Credential verification is unavailable."))
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	in, parseErr := decodeCredentialIdentity(data)
	if err != nil || parseErr != nil {
		httpx.WriteJSON(w, 400, httpx.Error("invalid_request", "A bounded credential identity request is required."))
		return
	}
	out, err := reader.ReadCredentialIdentity(r.Context(), HashToken(token), in)
	if errors.Is(err, pgx.ErrNoRows) {
		writeAgentUnauthorized(w)
		return
	}
	if err != nil {
		h.logger.Error("credential identity verification failed")
		httpx.WriteJSON(w, 500, httpx.Error("verification_failed", "Credential identity verification failed."))
		return
	}
	sum := sha256.Sum256(data)
	out.SchemaVersion = 1
	out.Verified = true
	out.RequestHash = hex.EncodeToString(sum[:])
	httpx.WriteJSON(w, 200, out)
}
