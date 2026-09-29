package db

// Deployment rehearsal for the applied-client-settings checks in
// docs/operations/applied-client-settings. It runs only from rehearse.sh in
// that directory (REHEARSAL_DATABASE_URL and REHEARSAL_PHASE set) against a
// disposable database the script seeded with the schema-155 base build:
//
//   predict: P2/P3/P4 of preflight-schema-155.sql on schema 155
//   upgrade: db.Migrate and BackfillClientSettings, as Manager does at start
//   verify:  Q4/Q5 of postflight-schema-158.sql against the prediction and
//            against vpnaccounts.BuildClientConnection for every active account
//
// The statements are read from the committed SQL files, so the rehearsal
// checks exactly what an operator runs. Output lists node names, account ids,
// states and version numbers only.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/configs"
	"github.com/ikaevus/routegate/backend/internal/vpnaccounts"
)

const rehearsalChecksDir = "../../../docs/operations/applied-client-settings"

type rehearsalRecord struct {
	// node -> state -> active accounts (Go connection states)
	Counts map[string]map[string]int `json:"counts"`
	// account id -> Go connection state, for accounts not served a link
	Withheld map[string]string `json:"withheld"`
	// "node/version" of versions whose snapshot cannot be derived
	NoSnapshot []string `json:"noSnapshot"`
}

func TestAppliedClientSettingsRehearsal(t *testing.T) {
	url, phase, out := os.Getenv("REHEARSAL_DATABASE_URL"), os.Getenv("REHEARSAL_PHASE"), os.Getenv("REHEARSAL_OUT")
	if url == "" || phase == "" || out == "" {
		t.Skip("run through docs/operations/applied-client-settings/rehearsal/rehearse.sh")
	}
	if !strings.Contains(url, "rehearsal") {
		t.Fatal("REHEARSAL_DATABASE_URL must name a disposable rehearsal database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	pool, err := Connect(ctx, url, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	switch phase {
	case "predict":
		predicted := rehearsalRecord{
			Counts:     preflightStateCounts(t, ctx, pool, rehearsalBlock(t, "preflight-schema-155.sql", "P2")),
			Withheld:   withheldAccounts(t, ctx, pool, rehearsalBlock(t, "preflight-schema-155.sql", "P3"), "state_after_update"),
			NoSnapshot: preflightNoSnapshot(t, ctx, pool, rehearsalBlock(t, "preflight-schema-155.sql", "P4")),
		}
		writeRehearsal(t, filepath.Join(out, "predicted.json"), predicted)
		t.Logf("predicted: %s", summarize(predicted))
	case "upgrade":
		if err := Migrate(ctx, pool, "../../migrations", logger); err != nil {
			t.Fatal(err)
		}
		filled, failures, err := configs.NewRepository(pool).BackfillClientSettings(ctx)
		if err != nil {
			t.Fatal(err)
		}
		names := serverNames(t, ctx, pool)
		record := rehearsalRecord{}
		for _, failure := range failures {
			record.NoSnapshot = append(record.NoSnapshot, fmt.Sprintf("%s/%d", names[failure.ServerID], failure.Version))
		}
		sort.Strings(record.NoSnapshot)
		writeRehearsal(t, filepath.Join(out, "backfill.json"), record)
		t.Logf("backfill filled %d versions; without snapshot: %v", filled, record.NoSnapshot)
	case "verify":
		var predicted, backfill rehearsalRecord
		readRehearsal(t, filepath.Join(out, "predicted.json"), &predicted)
		readRehearsal(t, filepath.Join(out, "backfill.json"), &backfill)
		if !reflect.DeepEqual(predicted.NoSnapshot, backfill.NoSnapshot) {
			t.Errorf("P4 predicted versions without snapshot %v, backfill left %v", predicted.NoSnapshot, backfill.NoSnapshot)
		}
		q4, q5 := rehearsalBlock(t, "postflight-schema-158.sql", "Q4"), rehearsalBlock(t, "postflight-schema-158.sql", "Q5")
		before := rehearsalRecord{
			Counts:   postflightStateCounts(t, ctx, pool, q4),
			Withheld: withheldAccounts(t, ctx, pool, q5, "state"),
		}
		// BuildClientConnection seeds protocol rows on first read; the model
		// accounts for that, so Q4/Q5 must not change afterwards.
		served := servedByBackend(t, ctx, pool)
		after := rehearsalRecord{
			Counts:   postflightStateCounts(t, ctx, pool, q4),
			Withheld: withheldAccounts(t, ctx, pool, q5, "state"),
		}
		for name, got := range map[string]rehearsalRecord{"P2/P3 prediction": predicted, "Q4/Q5": before, "Q4/Q5 after backend reads": after} {
			if !reflect.DeepEqual(got.Counts, served.Counts) {
				t.Errorf("%s counts %v, backend %v", name, got.Counts, served.Counts)
			}
			if !reflect.DeepEqual(got.Withheld, served.Withheld) {
				t.Errorf("%s withheld %v, backend %v", name, got.Withheld, served.Withheld)
			}
		}
		t.Logf("backend: %s", summarize(served))
	default:
		t.Fatalf("unknown REHEARSAL_PHASE %q", phase)
	}
}

// rehearsalBlock returns the SQL statement that follows `\echo '== <id>.` in
// a committed check file, without psql meta-commands.
func rehearsalBlock(t *testing.T, file, id string) string {
	t.Helper()
	handle, err := os.Open(filepath.Join(rehearsalChecksDir, file))
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	var block strings.Builder
	inside := false
	scanner := bufio.NewScanner(handle)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, `\echo '== `) {
			if inside {
				break
			}
			inside = strings.HasPrefix(line, `\echo '== `+id+`.`)
			continue
		}
		if !inside || strings.HasPrefix(line, `\`) || strings.HasPrefix(line, "ROLLBACK;") {
			continue
		}
		block.WriteString(line)
		block.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(block.String()) == "" {
		t.Fatalf("%s: block %s not found", file, id)
	}
	return block.String()
}

// queryRows runs a check statement read-only and returns its rows by column.
func queryRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string) []map[string]string {
	t.Helper()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, sql)
	if err != nil {
		t.Fatalf("check statement failed: %v", err)
	}
	defer rows.Close()
	var result []map[string]string
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			t.Fatal(err)
		}
		row := map[string]string{}
		for i, field := range rows.FieldDescriptions() {
			if id, ok := values[i].([16]byte); ok {
				row[field.Name] = fmt.Sprintf("%x-%x-%x-%x-%x", id[0:4], id[4:6], id[6:8], id[8:10], id[10:16])
				continue
			}
			row[field.Name] = fmt.Sprint(values[i])
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("check statement failed: %v", err)
	}
	return result
}

var preflightToConnectionState = map[string]string{
	"served":                    "ready",
	"served_compatibility_mode": "ready",
	"unchecked_no_snapshot":     "ready",
	"withheld_until_apply":      "awaiting_apply",
	"withheld_first_apply":      "awaiting_first_apply",
}

var postflightToConnectionState = map[string]string{
	"ready":                        "ready",
	"served_unchecked_no_snapshot": "ready",
	"awaiting_apply":               "awaiting_apply",
	"awaiting_first_apply":         "awaiting_first_apply",
}

func preflightStateCounts(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string) map[string]map[string]int {
	return stateCounts(t, queryRows(t, ctx, pool, sql), "state_after_update", preflightToConnectionState)
}

func postflightStateCounts(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string) map[string]map[string]int {
	return stateCounts(t, queryRows(t, ctx, pool, sql), "state", postflightToConnectionState)
}

func stateCounts(t *testing.T, rows []map[string]string, column string, mapping map[string]string) map[string]map[string]int {
	t.Helper()
	counts := map[string]map[string]int{}
	for _, row := range rows {
		state, ok := mapping[row[column]]
		if !ok {
			t.Fatalf("unexpected state %q", row[column])
		}
		var n int
		if _, err := fmt.Sscan(row["active_accounts"], &n); err != nil {
			t.Fatal(err)
		}
		if counts[row["name"]] == nil {
			counts[row["name"]] = map[string]int{}
		}
		counts[row["name"]][state] += n
	}
	return counts
}

func withheldAccounts(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql, column string) map[string]string {
	withheld := map[string]string{}
	for _, row := range queryRows(t, ctx, pool, sql) {
		state := preflightToConnectionState[row[column]]
		if state == "" {
			state = postflightToConnectionState[row[column]]
		}
		withheld[row["account_id"]] = state
	}
	return withheld
}

func preflightNoSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string) []string {
	versions := []string{}
	for _, row := range queryRows(t, ctx, pool, sql) {
		if row["snapshot_derive_failure"] != "" && row["snapshot_derive_failure"] != "<nil>" {
			versions = append(versions, row["name"]+"/"+row["version"])
		}
	}
	sort.Strings(versions)
	return versions
}

// servedByBackend classifies every active account through the shared client
// connection path used by GET /client-connection, /sub/ and the JSON subscription.
func servedByBackend(t *testing.T, ctx context.Context, pool *pgxpool.Pool) rehearsalRecord {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT a.id::text, s.name FROM vpn_accounts a JOIN servers s ON s.id = a.server_id
		WHERE a.status = 'active' ORDER BY s.name, a.id`)
	if err != nil {
		t.Fatal(err)
	}
	type account struct{ id, node string }
	var list []account
	for rows.Next() {
		var item account
		if err := rows.Scan(&item.id, &item.node); err != nil {
			t.Fatal(err)
		}
		list = append(list, item)
	}
	rows.Close()
	repository := vpnaccounts.NewRepository(pool)
	record := rehearsalRecord{Counts: map[string]map[string]int{}, Withheld: map[string]string{}}
	for _, item := range list {
		_, err := vpnaccounts.BuildClientConnection(ctx, repository, item.id)
		state := "ready"
		switch {
		case err == nil:
		case errors.Is(err, vpnaccounts.ErrNodeConfigNotApplied):
			state = "awaiting_first_apply"
		case errors.Is(err, vpnaccounts.ErrAccountProtocolNotDeployed):
			state = "awaiting_apply"
		default:
			// Not a deployment decision; the checks do not model it.
			t.Fatalf("account %s on %s: connection refused for another reason: %v", item.id, item.node, err)
		}
		if record.Counts[item.node] == nil {
			record.Counts[item.node] = map[string]int{}
		}
		record.Counts[item.node][state]++
		if state != "ready" {
			record.Withheld[item.id] = state
		}
	}
	return record
}

func serverNames(t *testing.T, ctx context.Context, pool *pgxpool.Pool) map[string]string {
	t.Helper()
	names := map[string]string{}
	for _, row := range queryRows(t, ctx, pool, `SELECT id::text AS id, name FROM servers`) {
		names[row["id"]] = row["name"]
	}
	return names
}

func summarize(record rehearsalRecord) string {
	encoded, _ := json.Marshal(record)
	return string(encoded)
}

func writeRehearsal(t *testing.T, path string, record rehearsalRecord) {
	t.Helper()
	encoded, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readRehearsal(t *testing.T, path string, record *rehearsalRecord) {
	t.Helper()
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, record); err != nil {
		t.Fatal(err)
	}
}
