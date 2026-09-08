package routingprofiles

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const maxManagedRuleSetBytes = 32 << 20

type ManagedRuleSetRefresher struct {
	repository managedRefreshRepository
	client     *http.Client
}

type managedRefreshRepository interface {
	GetManagedRuleSet(context.Context, string) (managedRuleSetRecord, error)
	MarkManagedRuleSetRefreshFailed(context.Context, string, string) (ManagedRuleSet, error)
	MarkManagedRuleSetRefreshSucceeded(context.Context, string, json.RawMessage) (ManagedRuleSet, error)
}

func NewManagedRuleSetRefresher(repository managedRefreshRepository) *ManagedRuleSetRefresher {
	return &ManagedRuleSetRefresher{repository: repository, client: managedRuleSetHTTPClient()}
}

type RefreshWorker struct {
	logger     *slog.Logger
	repository *Repository
	refresher  *ManagedRuleSetRefresher
}

func NewRefreshWorker(logger *slog.Logger, pool *pgxpool.Pool) *RefreshWorker {
	repository := NewRepository(pool)
	return &RefreshWorker{logger: logger, repository: repository, refresher: NewManagedRuleSetRefresher(repository)}
}

func (w *RefreshWorker) Run(ctx context.Context) error {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if err := w.refreshDue(ctx); err != nil && !errors.Is(err, context.Canceled) {
			w.logger.Error("refresh managed routing rule sets", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (w *RefreshWorker) refreshDue(ctx context.Context) error {
	items, err := w.repository.ListManagedRuleSetsDue(ctx, 10)
	if err != nil {
		return err
	}
	for _, item := range items {
		if _, err := w.refresher.Refresh(ctx, item.ID); err != nil {
			w.logger.Warn("managed routing rule set refresh failed", "id", item.ID, "error", err)
		}
	}
	return nil
}

func (s *ManagedRuleSetRefresher) Refresh(ctx context.Context, id string) (ManagedRuleSet, error) {
	record, err := s.repository.GetManagedRuleSet(ctx, id)
	if err != nil {
		return ManagedRuleSet{}, err
	}
	snapshot, err := s.download(ctx, record.SourceURL)
	if err != nil {
		failed, markErr := s.repository.MarkManagedRuleSetRefreshFailed(ctx, id, truncateRefreshError(err.Error()))
		if markErr != nil {
			return ManagedRuleSet{}, fmt.Errorf("refresh failed: %v; record failure: %w", err, markErr)
		}
		return failed, err
	}
	return s.repository.MarkManagedRuleSetRefreshSucceeded(ctx, id, snapshot)
}

func (s *ManagedRuleSetRefresher) download(ctx context.Context, sourceURL string) (json.RawMessage, error) {
	if err := validateManagedSourceURL(ctx, sourceURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download source: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("source returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxManagedRuleSetBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read source: %w", err)
	}
	if len(body) > maxManagedRuleSetBytes {
		return nil, errors.New("source exceeds 32 MiB limit")
	}
	return parseManagedRuleSetSnapshot(body)
}

func parseManagedRuleSetSnapshot(body []byte) (json.RawMessage, error) {
	var snapshot ManagedRuleSetSnapshot
	if err := json.Unmarshal(body, &snapshot); err != nil {
		return nil, fmt.Errorf("invalid sing-box source rule set: %w", err)
	}
	if snapshot.Version != 1 {
		return nil, fmt.Errorf("unsupported rule-set version %d", snapshot.Version)
	}
	if len(snapshot.Rules) == 0 {
		return nil, errors.New("rule set contains no rules")
	}
	return json.Marshal(snapshot)
}

func managedRuleSetHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
			if err != nil {
				return nil, err
			}
			for _, addressIP := range addresses {
				if !isPublicAddress(addressIP) {
					continue
				}
				return dialer.DialContext(ctx, network, net.JoinHostPort(addressIP.String(), port))
			}
			return nil, errors.New("source host resolves only to private or reserved addresses")
		},
	}
	return &http.Client{
		Transport: transport,
		Timeout:   20 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			return validateManagedSourceURL(req.Context(), req.URL.String())
		},
	}
}

func validateManagedSourceURL(ctx context.Context, value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Hostname() == "" || parsed.User != nil {
		return errors.New("sourceUrl must be an HTTP(S) URL without credentials")
	}
	if parsed.Scheme == "http" && parsed.Port() != "" {
		if _, err := strconv.Atoi(parsed.Port()); err != nil {
			return errors.New("sourceUrl has an invalid port")
		}
	}
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", parsed.Hostname())
	if err != nil {
		return fmt.Errorf("resolve source host: %w", err)
	}
	for _, address := range addresses {
		if !isPublicAddress(address) {
			return errors.New("sourceUrl must not resolve to a private or reserved address")
		}
	}
	return nil
}

func isPublicAddress(address netip.Addr) bool {
	return address.IsValid() && !address.IsPrivate() && !address.IsLoopback() && !address.IsLinkLocalUnicast() &&
		!address.IsLinkLocalMulticast() && !address.IsMulticast() && !address.IsUnspecified()
}

func truncateRefreshError(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 1000 {
		return value[:1000]
	}
	return value
}
