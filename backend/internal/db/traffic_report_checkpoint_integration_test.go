package db

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/dashboard"
	"github.com/ikaevus/routegate/backend/internal/traffic"
)

func TestTrafficBatchCheckpointPreventsDuplicateRollups(t *testing.T) {
	url := os.Getenv("ROUTEGATE_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("ROUTEGATE_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	pool, err := Connect(ctx, url, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	resetPublicSchema(t, ctx, pool)
	if err := Migrate(ctx, pool, "../../migrations", logger); err != nil {
		t.Fatal(err)
	}
	var serverID, accountID string
	if err := pool.QueryRow(ctx, `INSERT INTO servers(name,status) VALUES('traffic-checkpoint','active') RETURNING id::text`).Scan(&serverID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO agents(server_id,agent_version,token_hash,status) VALUES($1::uuid,'test','traffic-checkpoint-token','online')`, serverID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO vpn_accounts(server_id,username,display_name,protocol,status) VALUES($1::uuid,'checkpoint-account','Checkpoint account','sing-box','active') RETURNING id::text`, serverID).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	repo := traffic.NewRepository(pool)
	const stream = "11111111-1111-4111-8111-111111111111"
	event := []traffic.CreateUsageEventInput{{VPNAccountID: accountID, RxBytes: 100, TxBytes: 200, ObservedAt: time.Now().UTC().Truncate(time.Microsecond)}}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repo.ReportUsageBatch(ctx, "traffic-checkpoint-token", event, stream, 1)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	assertTotals := func(rx, tx int64) {
		t.Helper()
		var actualRX, actualTX int64
		if err := pool.QueryRow(ctx, `SELECT COALESCE(SUM(rx_bytes),0),COALESCE(SUM(tx_bytes),0) FROM traffic_usage_daily`).Scan(&actualRX, &actualTX); err != nil {
			t.Fatal(err)
		}
		if actualRX != rx || actualTX != tx {
			t.Fatalf("rollup=%d/%d want %d/%d", actualRX, actualTX, rx, tx)
		}
	}
	assertTotals(100, 200)
	snapshot, err := dashboard.NewRepository(pool).GetTrafficSnapshot(ctx, time.Now().UTC(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.DailyAvailable || !snapshot.MonthlyAvailable || snapshot.Monthly.TotalBytes != 300 {
		t.Fatalf("Dashboard did not receive usage: %+v", snapshot)
	}
	event[0].RxBytes = 999
	if _, err := repo.ReportUsageBatch(ctx, "traffic-checkpoint-token", event, stream, 1); !errors.Is(err, traffic.ErrReportSequenceConflict) {
		t.Fatalf("changed replay accepted: %v", err)
	}
	if _, err := repo.ReportUsageBatch(ctx, "traffic-checkpoint-token", event, stream, 3); !errors.Is(err, traffic.ErrReportSequenceConflict) {
		t.Fatalf("sequence gap accepted: %v", err)
	}
	assertTotals(100, 200)
	// A failed account-scope check must roll back its sequence reservation.
	account := event[0].VPNAccountID
	event[0].VPNAccountID = "22222222-2222-4222-8222-222222222222"
	if _, err := repo.ReportUsageBatch(ctx, "traffic-checkpoint-token", event, stream, 2); !errors.Is(err, traffic.ErrVPNAccountNotFound) {
		t.Fatalf("unknown account: %v", err)
	}
	event[0].VPNAccountID = account
	event[0].RxBytes = 10
	event[0].TxBytes = 20
	if _, err := repo.ReportUsageBatch(ctx, "traffic-checkpoint-token", event, stream, 2); err != nil {
		t.Fatal(err)
	}
	assertTotals(110, 220)
	// Retaining the small high-water mark prevents replay after raw-history cleanup.
	archive, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Rollback(ctx)
	if _, err := archive.Exec(ctx, `SELECT set_config('routegate.preserve_traffic_rollup','on',true)`); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Exec(ctx, `DELETE FROM traffic_usage_events`); err != nil {
		t.Fatal(err)
	}
	if err := archive.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ReportUsageBatch(ctx, "traffic-checkpoint-token", event, stream, 2); err != nil {
		t.Fatal(err)
	}
	assertTotals(110, 220)
}
