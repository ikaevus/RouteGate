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

// Called by an isolated Manager/PostgreSQL integration test, never by Runner.
// Env opt-in and literal loopback guard prohibit accidentally forwarding a
// bearer or historical proof to any real Manager or VPN node.
func TestRecoveryAdmissionReleaseAgainstManagerPostgreSQL(t *testing.T) {
	endpoint, token, source := os.Getenv("ROUTEGATE_RG140_CLIENT_URL"),
		os.Getenv("ROUTEGATE_RG140_CLIENT_TOKEN"), os.Getenv("ROUTEGATE_RG140_CLIENT_WIRE_B64")
	if endpoint == "" && token == "" && source == "" {
		t.Skip("requires isolated Manager/PostgreSQL parent test")
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Hostname() != "127.0.0.1" && u.Hostname() != "::1") || u.Port() == "" || token == "" || source == "" {
		t.Fatal("isolated loopback Manager endpoint required")
	}
	wire, err := base64.RawStdEncoding.DecodeString(source)
	if err != nil || len(wire) == 0 || len(wire) > 8192 {
		t.Fatal("bounded immutable proof required")
	}
	var p tasks.RuntimeRecoveryAdmissionReleaseProof
	if err = json.Unmarshal(wire, &p); err != nil {
		t.Fatal("invalid shared post-release wire fixture")
	}
	canonical, err := json.Marshal(p)
	if err != nil || !bytes.Equal(canonical, wire) || p.SchemaVersion != 1 ||
		p.Purpose != tasks.RuntimeRecoveryAdmissionReleasePurpose {
		t.Fatal("noncanonical Manager/Agent wire")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	c := New(endpoint)
	receipt, err := c.readRecoveryAdmissionReleaseReceipt(ctx, token, p)
	if err != nil || !validAdmissionReleaseReceipt(receipt, p, recoveryReadHash(wire)) {
		t.Fatal("real Manager/PostgreSQL historical receipt was not verified", err)
	}

	// A foreign token and a changed historical claim cannot pass through the
	// same HTTP service. This client NEVER POSTs a new evidence acceptance.
	if other, err := c.readRecoveryAdmissionReleaseReceipt(ctx, "rg_agent_wrong_isolated", p); err == nil ||
		other != (RecoveryAdmissionReleaseReceipt{}) {
		t.Fatal("foreign Agent bearer verified retained Manager proof")
	}
	changed := p
	changed.CleanedArchiveHash = strings.Repeat("f", 64)
	if changed.CleanedArchiveHash == p.CleanedArchiveHash {
		changed.CleanedArchiveHash = strings.Repeat("a", 64)
	}
	if other, err := c.readRecoveryAdmissionReleaseReceipt(ctx, token, changed); err == nil ||
		other != (RecoveryAdmissionReleaseReceipt{}) {
		t.Fatal("changed Agent claim verified unrelated immutable ledger row")
	}
}
