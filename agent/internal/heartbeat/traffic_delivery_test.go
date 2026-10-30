package heartbeat

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ikaevus/routegate/agent/internal/config"
	"github.com/ikaevus/routegate/agent/internal/traffic"
)

type trafficFixture struct {
	calls int
	rx    int64
}

func (c *trafficFixture) Collect(context.Context) ([]traffic.CounterSnapshot, error) {
	c.calls++
	return []traffic.CounterSnapshot{{VPNAccountID: "a", RxBytes: c.rx, TxBytes: 100, ObservedAt: time.Now().UTC()}}, nil
}

func TestTrafficRetriesImmutableBatchAfterLostAcknowledgementAndRestart(t *testing.T) {
	var requests [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		requests = append(requests, data)
		if len(requests) == 1 {
			w.WriteHeader(500)
			return
		} // The Manager may already have committed.
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true,"agentId":"agent-1","accepted":1}`))
	}))
	defer server.Close()
	cfg := config.Config{ManagerURL: server.URL, AgentID: "agent-1", AgentToken: "test", TrafficCollectionEnabled: true}
	path := filepath.Join(t.TempDir(), "agent.yaml")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	runner := NewRunner(cfg, path, logger)
	fixture := &trafficFixture{rx: 100}
	runner.trafficCollector = fixture
	if err := runner.reportTrafficUsage(context.Background()); err != nil {
		t.Fatal(err)
	}
	fixture.rx = 180
	runner.lastTrafficReport = time.Time{}
	if err := runner.reportTrafficUsage(context.Background()); err == nil {
		t.Fatal("expected send failure")
	}
	restarted := NewRunner(cfg, path, logger)
	replacement := &trafficFixture{rx: 999}
	restarted.trafficCollector = replacement
	if err := restarted.reportTrafficUsage(context.Background()); err != nil {
		t.Fatal(err)
	}
	if replacement.calls != 0 {
		t.Fatal("collected new counters before retrying pending batch")
	}
	if len(requests) != 2 || string(requests[0]) != string(requests[1]) {
		t.Fatal("retry changed batch payload")
	}
	box, err := traffic.OpenOutbox(path+".traffic-state.json", "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	if box.Sequence != 2 || len(box.Pending) != 0 {
		t.Fatal("checkpoint was not acknowledged")
	}
}
