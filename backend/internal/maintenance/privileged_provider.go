package maintenance

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"time"
)

const (
	maintenanceDispatchSocket = "/run/routegate/maintenance-dispatch.sock"
	maintenanceResponseLimit  = 4096
)

var maintenanceDispatchToken = regexp.MustCompile(`^[0-9a-f]{32}$`)

type privilegedDispatcher interface {
	Analyze(context.Context, string) (dispatchResponse, error)
	Cleanup(context.Context, string) (dispatchResponse, error)
	Verify(context.Context, string) (dispatchResponse, error)
}

type unixPrivilegedDispatcher struct {
	socketPath string
}

type dispatchResponse struct {
	OK             bool   `json:"ok"`
	Token          string `json:"token,omitempty"`
	CandidateCount int64  `json:"candidateCount,omitempty"`
	EstimatedBytes int64  `json:"estimatedBytes,omitempty"`
	DeletedCount   int64  `json:"deletedCount,omitempty"`
	ReclaimedBytes int64  `json:"reclaimedBytes,omitempty"`
	RemainingCount int64  `json:"remainingCount,omitempty"`
}

func newPlatformRollbackProvider() Provider {
	return privilegedProvider{
		category:   Category{ID: "platform_rollback_backups", Scope: "manager", Selectable: true, RetentionDays: 30},
		dispatcher: unixPrivilegedDispatcher{socketPath: maintenanceDispatchSocket},
	}
}

func newPrometheusRetentionProvider() Provider {
	return privilegedProvider{
		category:   Category{ID: "prometheus_tsdb_retention", Scope: "prometheus", Selectable: true, RetentionDays: 90},
		dispatcher: unixPrivilegedDispatcher{socketPath: maintenanceDispatchSocket},
	}
}

type privilegedProvider struct {
	category   Category
	dispatcher privilegedDispatcher
}

func (p privilegedProvider) Category() Category { return p.category }

func (p privilegedProvider) Analyze(ctx context.Context, cutoff time.Time) (PlanItem, error) {
	response, err := p.dispatcher.Analyze(ctx, p.category.ID)
	if err != nil {
		return PlanItem{}, err
	}
	return PlanItem{
		CategoryID: p.category.ID, Scope: p.category.Scope, RetentionDays: p.category.RetentionDays,
		Cutoff: cutoff, CandidateCount: response.CandidateCount, EstimatedBytes: response.EstimatedBytes,
		Metadata: map[string]any{"dispatchToken": response.Token},
	}, nil
}

func (p privilegedProvider) Cleanup(ctx context.Context, item PlanItem) (CleanupResult, error) {
	token, err := dispatchToken(item.Metadata)
	if err != nil {
		return CleanupResult{}, err
	}
	response, err := p.dispatcher.Cleanup(ctx, token)
	if err != nil {
		return CleanupResult{}, err
	}
	return CleanupResult{DeletedCount: response.DeletedCount, ReclaimedBytes: response.ReclaimedBytes}, nil
}

func (p privilegedProvider) Verify(ctx context.Context, item PlanItem) (int64, error) {
	token, err := dispatchToken(item.Metadata)
	if err != nil {
		return 0, err
	}
	response, err := p.dispatcher.Verify(ctx, token)
	if err != nil {
		return 0, err
	}
	return response.RemainingCount, nil
}

func (d unixPrivilegedDispatcher) Analyze(ctx context.Context, category string) (dispatchResponse, error) {
	response, err := d.call(ctx, "analyze:"+category)
	if err != nil {
		return dispatchResponse{}, err
	}
	if !maintenanceDispatchToken.MatchString(response.Token) {
		return dispatchResponse{}, ErrCategoryUnavailable
	}
	return response, nil
}

func (d unixPrivilegedDispatcher) Cleanup(ctx context.Context, token string) (dispatchResponse, error) {
	if !maintenanceDispatchToken.MatchString(token) {
		return dispatchResponse{}, ErrUnsafeOwnership
	}
	return d.call(ctx, "cleanup:"+token)
}

func (d unixPrivilegedDispatcher) Verify(ctx context.Context, token string) (dispatchResponse, error) {
	if !maintenanceDispatchToken.MatchString(token) {
		return dispatchResponse{}, ErrUnsafeOwnership
	}
	return d.call(ctx, "verify:"+token)
}

func (d unixPrivilegedDispatcher) call(ctx context.Context, request string) (dispatchResponse, error) {
	dialer := net.Dialer{}
	connection, err := dialer.DialContext(ctx, "unix", d.socketPath)
	if err != nil {
		return dispatchResponse{}, fmt.Errorf("%w: privileged dispatcher unavailable", ErrCategoryUnavailable)
	}
	defer connection.Close()
	if deadline, ok := ctx.Deadline(); ok {
		if err := connection.SetDeadline(deadline); err != nil {
			return dispatchResponse{}, err
		}
	}
	if _, err := connection.Write([]byte(request + "\n")); err != nil {
		return dispatchResponse{}, fmt.Errorf("write privileged maintenance request: %w", err)
	}
	closeWriter, ok := connection.(interface{ CloseWrite() error })
	if !ok {
		return dispatchResponse{}, errors.New("privileged dispatcher does not support request framing")
	}
	if err := closeWriter.CloseWrite(); err != nil {
		return dispatchResponse{}, fmt.Errorf("close privileged maintenance request: %w", err)
	}

	reader := bufio.NewReaderSize(connection, maintenanceResponseLimit+1)
	raw, err := reader.ReadBytes('\n')
	if err != nil || len(raw) > maintenanceResponseLimit {
		return dispatchResponse{}, errors.New("invalid privileged maintenance response")
	}
	if extra, err := reader.ReadByte(); !errors.Is(err, io.EOF) || len(raw) == 0 {
		_ = extra
		return dispatchResponse{}, errors.New("invalid privileged maintenance response framing")
	}
	var response dispatchResponse
	decoderErr := json.Unmarshal(raw, &response)
	if decoderErr != nil || !response.OK || response.CandidateCount < 0 || response.EstimatedBytes < 0 ||
		response.DeletedCount < 0 || response.ReclaimedBytes < 0 || response.RemainingCount < 0 {
		return dispatchResponse{}, ErrCategoryUnavailable
	}
	return response, nil
}

func dispatchToken(metadata map[string]any) (string, error) {
	token, ok := metadata["dispatchToken"].(string)
	if !ok || !maintenanceDispatchToken.MatchString(token) {
		return "", ErrUnsafeOwnership
	}
	return token, nil
}
