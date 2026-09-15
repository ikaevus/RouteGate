package maintenance

import (
	"bufio"
	"context"
	"errors"
	"net"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

type fakePrivilegedDispatcher struct {
	analyzeResponse dispatchResponse
	cleanupResponse dispatchResponse
	verifyResponse  dispatchResponse
	analyzed        string
	cleanedToken    string
	verifiedToken   string
}

func (d *fakePrivilegedDispatcher) Analyze(_ context.Context, category string) (dispatchResponse, error) {
	d.analyzed = category
	return d.analyzeResponse, nil
}
func (d *fakePrivilegedDispatcher) Cleanup(_ context.Context, token string) (dispatchResponse, error) {
	d.cleanedToken = token
	return d.cleanupResponse, nil
}
func (d *fakePrivilegedDispatcher) Verify(_ context.Context, token string) (dispatchResponse, error) {
	d.verifiedToken = token
	return d.verifyResponse, nil
}

func TestPrivilegedProviderPreservesOpaquePlanToken(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"
	dispatcher := &fakePrivilegedDispatcher{
		analyzeResponse: dispatchResponse{OK: true, Token: token, CandidateCount: 2, EstimatedBytes: 128},
		cleanupResponse: dispatchResponse{OK: true, DeletedCount: 2, ReclaimedBytes: 96},
		verifyResponse:  dispatchResponse{OK: true},
	}
	provider := privilegedProvider{
		category:   Category{ID: "platform_rollback_backups", Scope: "manager", Selectable: true, RetentionDays: 30},
		dispatcher: dispatcher,
	}
	cutoff := fixedNow().Add(-30 * 24 * time.Hour)
	item, err := provider.Analyze(context.Background(), cutoff)
	if err != nil {
		t.Fatal(err)
	}
	if item.CandidateCount != 2 || item.Metadata["dispatchToken"] != token || dispatcher.analyzed != provider.category.ID {
		t.Fatalf("unexpected plan item: %#v", item)
	}
	result, err := provider.Cleanup(context.Background(), item)
	if err != nil {
		t.Fatal(err)
	}
	remaining, err := provider.Verify(context.Background(), item)
	if err != nil {
		t.Fatal(err)
	}
	if result.DeletedCount != 2 || result.ReclaimedBytes != 96 || remaining != 0 || dispatcher.cleanedToken != token || dispatcher.verifiedToken != token {
		t.Fatalf("unexpected privileged lifecycle: result=%#v remaining=%d", result, remaining)
	}
}

func TestUnixPrivilegedDispatcherUsesHalfClosedBoundedProtocol(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "maintenance.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
			t.Skipf("unix sockets are unavailable in this test environment: %v", err)
		}
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan string, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			done <- "accept failed"
			return
		}
		defer connection.Close()
		reader := bufio.NewReader(connection)
		request, readErr := reader.ReadString('\n')
		if readErr != nil {
			done <- "read failed"
			return
		}
		if _, readErr = reader.ReadByte(); readErr == nil {
			done <- "request was not half-closed"
			return
		}
		_, _ = connection.Write([]byte(`{"ok":true,"token":"0123456789abcdef0123456789abcdef","candidateCount":1}` + "\n"))
		done <- request
	}()

	dispatcher := unixPrivilegedDispatcher{socketPath: socketPath}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	response, err := dispatcher.Analyze(ctx, "platform_rollback_backups")
	if err != nil {
		t.Fatal(err)
	}
	if response.CandidateCount != 1 {
		t.Fatalf("response=%#v", response)
	}
	if request := <-done; request != "analyze:platform_rollback_backups\n" {
		t.Fatalf("request=%q", request)
	}
}

func TestPublicPlanRedactsPrivilegedMetadata(t *testing.T) {
	plan := Plan{Payload: PlanPayload{SchemaVersion: 1, Items: []PlanItem{{
		CategoryID: "platform_rollback_backups",
		Metadata:   map[string]any{"dispatchToken": "secret"},
	}}}}
	public := publicPlan(plan)
	if public.Payload.Items[0].Metadata != nil {
		t.Fatalf("public metadata was not redacted: %#v", public.Payload.Items[0].Metadata)
	}
	if plan.Payload.Items[0].Metadata["dispatchToken"] != "secret" {
		t.Fatal("redaction mutated the persisted plan")
	}
}
