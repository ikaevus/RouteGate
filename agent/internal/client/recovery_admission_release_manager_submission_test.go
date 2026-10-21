package client

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ikaevus/routegate/agent/internal/tasks"
)

// Test-only cross-module submission of a FROZEN statement from an isolated
// Manager fixture. This deliberately does not impersonate a live-owner flock
// or execute release: those are separate tests and a remaining E2E boundary.
func TestRecoveryAdmissionReleaseSubmitAgainstManagerPostgreSQL(t *testing.T) {
	endpoint := os.Getenv("ROUTEGATE_RG140_SUBMIT_URL")
	token := os.Getenv("ROUTEGATE_RG140_SUBMIT_TOKEN")
	proof64 := os.Getenv("ROUTEGATE_RG140_SUBMIT_PROOF_B64")
	authority64 := os.Getenv("ROUTEGATE_RG140_SUBMIT_AUTHORITY_B64")
	if endpoint == "" && token == "" && proof64 == "" && authority64 == "" {
		t.Skip("requires isolated Manager/PostgreSQL test-only caller")
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Path != "" || u.RawQuery != "" ||
		u.Fragment != "" || (u.Hostname() != "127.0.0.1" && u.Hostname() != "::1") ||
		u.Port() == "" || !strings.HasPrefix(token, "rg_agent_") {
		t.Fatal("cross-module submission requires literal loopback and test Agent bearer")
	}
	wire, e1 := base64.RawStdEncoding.DecodeString(proof64)
	authWire, e2 := base64.RawStdEncoding.DecodeString(authority64)
	if e1 != nil || e2 != nil || len(wire) == 0 || len(wire) > 8192 ||
		len(authWire) == 0 || len(authWire) > 8192 {
		t.Fatal("missing or unbounded frozen historical input")
	}
	var p tasks.RuntimeRecoveryAdmissionReleaseProof
	var authority tasks.RuntimeRecoveryReleaseAuthorization
	if json.Unmarshal(wire, &p) != nil || json.Unmarshal(authWire, &authority) != nil {
		t.Fatal("invalid typed proof or original human authority")
	}
	canonical, err := json.Marshal(p)
	aCanonical, aErr := json.Marshal(authority)
	if err != nil || aErr != nil || !bytes.Equal(canonical, wire) ||
		!bytes.Equal(aCanonical, authWire) ||
		recoveryReadHash(authWire) != p.AuthorizationHash ||
		p.Purpose != tasks.RuntimeRecoveryAdmissionReleasePurpose ||
		p.SchemaVersion != 1 || p.Observation != authority.Expected {
		t.Fatal("Manager/Agent frozen statement or authority drift")
	}
	binding := tasks.RuntimeRecoveryGrantBinding{
		RuntimeResultBinding: tasks.RuntimeResultBinding{
			ManagerURL: endpoint,
			AgentID: p.Observation.AgentID,
			ServerID: p.Observation.ServerID,
		},
		AgentCredentialGeneration: p.AgentCredentialGeneration,
	}
	v := VerifiedRecoveryReleaseAuthorization{
		Binding: binding, Authorization: authority, AuthorizationHash: p.AuthorizationHash,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	c := New(endpoint)
	var checks int
	refresh := func(ctx context.Context) (tasks.RuntimeRecoveryAdmissionReleaseProof, error) {
		checks++
		return p, ctx.Err()
	}
	receipt, err := c.submitRecoveryAdmissionRelease(ctx, token, v, p, refresh)
	if err != nil || checks < 2 || !validAdmissionReleaseReceipt(receipt, p, recoveryReadHash(wire)) {
		t.Fatal("Agent POST was not durably acknowledged by real Manager/PostgreSQL", err)
	}
	verified, err := c.readRecoveryAdmissionReleaseReceipt(ctx, token, p)
	if err != nil || verified != receipt {
		t.Fatal("newly inserted proof was not historically verifiable", err)
	}
	// A changed frozen statement and foreign bearer cannot be acknowledged.
	changed := p
	changed.CleanedArchiveHash = strings.Repeat("f", 64)
	if changed.CleanedArchiveHash == p.CleanedArchiveHash {
		changed.CleanedArchiveHash = strings.Repeat("a", 64)
	}
	if got, err := c.submitRecoveryAdmissionRelease(ctx, token, v, changed,
		func(context.Context) (tasks.RuntimeRecoveryAdmissionReleaseProof, error) { return changed, nil }); err == nil ||
		got != (RecoveryAdmissionReleaseReceipt{}) {
		t.Fatal("different release statement replaced the stored proof")
	}
	if got, err := c.submitRecoveryAdmissionRelease(ctx, "rg_agent_foreign_isolated", v, p, refresh); err == nil ||
		got != (RecoveryAdmissionReleaseReceipt{}) {
		t.Fatal("foreign Agent bearer was accepted")
	}
}
