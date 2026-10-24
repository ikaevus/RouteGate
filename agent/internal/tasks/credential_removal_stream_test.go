package tasks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// Preserve the real-traffic client's existing deadline. Completion caused by
// this deadline is a failed observation, never evidence of server-side teardown.
const removalStreamClientTimeout = 12 * time.Second

type removalObservedStream struct {
	started time.Time
	done    <-chan removalStreamTermination
}

type removalStreamTermination struct {
	err        error
	contextErr error
	finished   time.Time
}

func (r removalStreamTermination) validate(started, mutationStarted time.Time) error {
	var networkErr net.Error
	if r.contextErr != nil || errors.Is(r.err, context.Canceled) ||
		errors.Is(r.err, context.DeadlineExceeded) ||
		(errors.As(r.err, &networkErr) && networkErr.Timeout()) {
		return fmt.Errorf("old session ended through client cancellation or timeout")
	}
	if r.finished.Before(mutationStarted) || !r.finished.Before(started.Add(removalStreamClientTimeout)) {
		return fmt.Errorf("old session termination outside the valid observation window")
	}
	return nil
}

func TestCredentialRemovalStreamTerminationRejectsClientExpiry(t *testing.T) {
	started := time.Now()
	mutation := started.Add(time.Second)
	for _, tc := range []struct {
		name       string
		err        error
		contextErr error
		finished   time.Time
		wantValid  bool
	}{
		{"remote EOF", nil, nil, mutation.Add(time.Second), true},
		{"remote truncated stream", io.ErrUnexpectedEOF, nil, mutation.Add(time.Second), true},
		{"client deadline", context.DeadlineExceeded, nil, mutation.Add(time.Second), false},
		{"wrapped cancellation", fmt.Errorf("read: %w", context.Canceled), nil, mutation.Add(time.Second), false},
		{"socket deadline", &net.OpError{Op: "read", Net: "tcp", Err: os.ErrDeadlineExceeded}, nil, mutation.Add(time.Second), false},
		{"expired parent with EOF", nil, context.DeadlineExceeded, mutation.Add(time.Second), false},
		{"closure before execution", io.ErrUnexpectedEOF, nil, mutation.Add(-time.Nanosecond), false},
		{"EOF at client deadline", nil, nil, started.Add(removalStreamClientTimeout), false},
		{"truncation after client deadline", io.ErrUnexpectedEOF, nil, started.Add(removalStreamClientTimeout + time.Nanosecond), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := removalStreamTermination{err: tc.err, contextErr: tc.contextErr, finished: tc.finished}
			if valid := result.validate(started, mutation) == nil; valid != tc.wantValid {
				t.Fatalf("accepted=%v, want %v", valid, tc.wantValid)
			}
		})
	}
}

func TestCredentialRemovalStreamRejectsActualHTTPCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "open")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer origin.Close()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, origin.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	response, err := (&http.Client{Timeout: removalStreamClientTimeout}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	first := make([]byte, 4)
	if _, err := io.ReadFull(response.Body, first); err != nil || string(first) != "open" {
		t.Fatal("stream did not open", err)
	}
	mutation := time.Now()
	cancel()
	_, readErr := io.ReadAll(response.Body)
	result := removalStreamTermination{err: readErr, contextErr: ctx.Err(), finished: time.Now()}
	if readErr == nil || result.validate(started, mutation) == nil {
		t.Fatal("client-closed HTTP stream counted as VPN session termination")
	}
}
