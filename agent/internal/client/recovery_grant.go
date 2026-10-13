package client

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"time"

	"github.com/ikaevus/routegate/agent/internal/tasks"
)

var errRecoveryGrantRead = errors.New("recovery grant verification failed")

// VerifiedRecoveryGrant is read-only transport output, not a commit receipt.
// Fetch never consumes authority or touches the Agent's durable fence.
type VerifiedRecoveryGrant struct {
	Binding   tasks.RuntimeRecoveryGrantBinding
	Grant     tasks.RuntimeRecoveryMutationGrant
	GrantHash string
}

type credentialIdentityRequest struct {
	SchemaVersion int    `json:"schemaVersion"`
	AgentID       string `json:"agentId"`
	ServerID      string `json:"serverId"`
	Nonce         string `json:"nonce"`
}
type credentialIdentityReply struct {
	SchemaVersion             int    `json:"schemaVersion"`
	Verified                  bool   `json:"verified"`
	AgentID                   string `json:"agentId"`
	ServerID                  string `json:"serverId"`
	AgentCredentialGeneration int64  `json:"agentCredentialGeneration"`
	RequestHash               string `json:"requestHash"`
}
type recoveryGrantRequest struct {
	SchemaVersion             int    `json:"schemaVersion"`
	OperationID               string `json:"operationId"`
	AgentID                   string `json:"agentId"`
	ServerID                  string `json:"serverId"`
	AgentCredentialGeneration int64  `json:"agentCredentialGeneration"`
	Nonce                     string `json:"nonce"`
}
type recoveryGrantReply struct {
	SchemaVersion             int                                `json:"schemaVersion"`
	OperationID               string                             `json:"operationId"`
	AgentID                   string                             `json:"agentId"`
	ServerID                  string                             `json:"serverId"`
	AgentCredentialGeneration int64                              `json:"agentCredentialGeneration"`
	RequestHash               string                             `json:"requestHash"`
	GrantHash                 string                             `json:"grantHash"`
	Grant                     tasks.RuntimeRecoveryMutationGrant `json:"grant"`
}

func recoveryReadNonce() (string, error) {
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", errRecoveryGrantRead
	}
	return hex.EncodeToString(nonce[:]), nil
}
func recoveryReadHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Separate nonce-bound identity exchange authenticates generation independently
// of grant JSON. Manager rechecks the same current generation during delivery.
// No Runner invokes this method or BeginRuntimeRecoveryCommit automatically.
func (c *Client) FetchRecoveryGrant(ctx context.Context, token, agentID, serverID, operationID string) (VerifiedRecoveryGrant, error) {
	zero := VerifiedRecoveryGrant{}
	binding, err := c.verifyRecoveryIdentity(ctx, token, agentID, serverID)
	if err != nil {
		return zero, errRecoveryGrantRead
	}
	nonce, err := recoveryReadNonce()
	if err != nil {
		return zero, errRecoveryGrantRead
	}
	data, _ := json.Marshal(recoveryGrantRequest{1, operationID, agentID, serverID, binding.AgentCredentialGeneration, nonce})
	var reply recoveryGrantReply
	if c.recoveryRead(ctx, "/api/v1/agent/recovery-observations/"+url.PathEscape(operationID)+"/grant", token, data, &reply) != nil || ctx.Err() != nil || reply.SchemaVersion != 1 || reply.OperationID != operationID || reply.AgentID != agentID || reply.ServerID != serverID || reply.AgentCredentialGeneration != binding.AgentCredentialGeneration || reply.RequestHash != recoveryReadHash(data) {
		return zero, errRecoveryGrantRead
	}
	wire, err := json.Marshal(reply.Grant)
	if err != nil {
		return zero, errRecoveryGrantRead
	}
	g, hash, err := tasks.ValidateRuntimeRecoveryGrant(wire, binding, time.Now())
	if err != nil || g.OperationID != operationID || hash != reply.GrantHash {
		return zero, errRecoveryGrantRead
	}
	return VerifiedRecoveryGrant{binding, g, hash}, nil
}

func (c *Client) verifyRecoveryIdentity(ctx context.Context, token, agentID, serverID string) (tasks.RuntimeRecoveryGrantBinding, error) {
	zero := tasks.RuntimeRecoveryGrantBinding{}
	nonce, err := recoveryReadNonce()
	if err != nil || token == "" {
		return zero, errRecoveryGrantRead
	}
	identityData, _ := json.Marshal(credentialIdentityRequest{1, agentID, serverID, nonce})
	var identity credentialIdentityReply
	if c.recoveryRead(ctx, "/api/v1/agent/identity/verify", token, identityData, &identity) != nil || identity.SchemaVersion != 1 || !identity.Verified || identity.AgentID != agentID || identity.ServerID != serverID || identity.AgentCredentialGeneration < 1 || identity.RequestHash != recoveryReadHash(identityData) {
		return zero, errRecoveryGrantRead
	}
	binding := tasks.RuntimeRecoveryGrantBinding{RuntimeResultBinding: tasks.RuntimeResultBinding{ManagerURL: c.managerURL, AgentID: agentID, ServerID: serverID}, AgentCredentialGeneration: identity.AgentCredentialGeneration}
	return binding, nil
}

// Reuse configured transport/timeout but reject all redirects. Grant reads
// require bounded, exact JSON; server errors and network URLs are never logged
// or returned to the caller because they may contain credentials or nonces.
func (c *Client) recoveryRead(ctx context.Context, path, token string, data []byte, out any) error {
	_, err := c.recoveryExchange(ctx, path, token, data, out)
	return err
}

// Preserve HTTP status for bounded commit retries without exposing error bodies.
func (c *Client) recoveryExchange(ctx context.Context, path, token string, data []byte, out any) (int, error) {
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, c.managerURL+path, bytes.NewReader(data))
	if err != nil {
		return 0, errRecoveryGrantRead
	}
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Cache-Control", "no-store")
	h := *c.httpClient
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := h.Do(r)
	if err != nil {
		return 0, errRecoveryGrantRead
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, errRecoveryGrantRead
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16385))
	if err != nil || len(body) > 16384 {
		return resp.StatusCode, errRecoveryGrantRead
	}
	if strictRecoveryReply(body, out) != nil {
		return resp.StatusCode, errRecoveryGrantRead
	}
	return resp.StatusCode, nil
}

// Recursively reject duplicate keys, then compare with typed JSON to reject
// aliases/missing fields/nulls rather than silently accepting decoder defaults.
func strictRecoveryReply(data []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var walk func() error
	walk = func() error {
		t, err := d.Token()
		if err != nil {
			return err
		}
		if delim, ok := t.(json.Delim); ok {
			switch delim {
			case '{':
				seen := map[string]bool{}
				for d.More() {
					key, e := d.Token()
					k, ok := key.(string)
					if e != nil || !ok || seen[k] {
						return errRecoveryGrantRead
					}
					seen[k] = true
					if e = walk(); e != nil {
						return e
					}
				}
				end, e := d.Token()
				if e != nil || end != json.Delim('}') {
					return errRecoveryGrantRead
				}
			case '[':
				for d.More() {
					if e := walk(); e != nil {
						return e
					}
				}
				end, e := d.Token()
				if e != nil || end != json.Delim(']') {
					return errRecoveryGrantRead
				}
			default:
				return errRecoveryGrantRead
			}
		}
		return nil
	}
	if walk() != nil {
		return errRecoveryGrantRead
	}
	if _, err := d.Token(); err != io.EOF {
		return errRecoveryGrantRead
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil {
		return errRecoveryGrantRead
	}
	wire, err := json.Marshal(out)
	if err != nil {
		return errRecoveryGrantRead
	}
	decode := func(b []byte) any {
		var v any
		j := json.NewDecoder(bytes.NewReader(b))
		j.UseNumber()
		if j.Decode(&v) != nil {
			return nil
		}
		return v
	}
	if !reflect.DeepEqual(decode(data), decode(wire)) {
		return errRecoveryGrantRead
	}
	return nil
}
