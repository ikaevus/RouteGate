package traffic

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/agents"
	"github.com/ikaevus/routegate/backend/internal/httpx"
)

type repository interface {
	ReportUsage(context.Context, string, []CreateUsageEventInput) (TrafficUsageReport, error)
	GetUsageSummary(context.Context, string, time.Time, time.Time) (TrafficUsageSummary, error)
	UpsertLimit(context.Context, string, UpsertTrafficLimitInput) (TrafficLimit, error)
}

type Handler struct {
	logger  *slog.Logger
	traffic repository
	now     func() time.Time
}

func NewHandler(logger *slog.Logger, pool *pgxpool.Pool) *Handler {
	return &Handler{
		logger:  logger,
		traffic: NewRepository(pool),
		now:     time.Now,
	}
}

func (h *Handler) ReportUsage(w http.ResponseWriter, r *http.Request) {
	token, ok := bearerToken(r.Header.Get("Authorization"))
	if !ok {
		writeUnauthorized(w)
		return
	}

	var request ReportUsageRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeInvalidRequest(w, "Request body must be valid JSON.")
		return
	}
	if len(request.Events) == 0 {
		writeInvalidRequest(w, "events must contain at least one usage event")
		return
	}
	if len(request.Events) > MaxUsageReportEvents {
		writeInvalidRequest(w, "too many usage events in one report")
		return
	}

	now := h.now().UTC()
	inputs := make([]CreateUsageEventInput, 0, len(request.Events))
	for index, event := range request.Events {
		vpnAccountID := strings.TrimSpace(event.VPNAccountID)
		if vpnAccountID == "" {
			writeInvalidRequest(w, eventError(index, "vpnAccountId is required"))
			return
		}
		if event.RxBytes < 0 || event.TxBytes < 0 {
			writeInvalidRequest(w, eventError(index, "rxBytes and txBytes must be greater than or equal to zero"))
			return
		}

		observedAt := now
		if event.ObservedAt != nil {
			observedAt = event.ObservedAt.UTC()
		}

		inputs = append(inputs, CreateUsageEventInput{
			VPNAccountID: vpnAccountID,
			RxBytes:      event.RxBytes,
			TxBytes:      event.TxBytes,
			ObservedAt:   observedAt,
			Metadata:     event.Metadata,
		})
	}

	result, err := h.traffic.ReportUsage(r.Context(), agents.HashToken(token), inputs)
	if errors.Is(err, ErrUnauthorizedAgent) {
		writeUnauthorized(w)
		return
	}
	if errors.Is(err, ErrAgentServerRequired) {
		httpx.WriteJSON(w, http.StatusForbidden, httpx.Error("agent_server_required", "Agent must be bound to a server before reporting traffic usage."))
		return
	}
	if errors.Is(err, ErrVPNAccountNotFound) {
		writeInvalidRequest(w, "usage report references an unknown VPN account")
		return
	}
	if err != nil {
		h.databaseError(w, "report traffic usage", err)
		return
	}

	httpx.WriteJSON(w, http.StatusAccepted, result)
}

func (h *Handler) GetAccountUsage(w http.ResponseWriter, r *http.Request) {
	from, to, err := h.usagePeriod(r)
	if err != nil {
		writeInvalidRequest(w, err.Error())
		return
	}

	summary, err := h.traffic.GetUsageSummary(r.Context(), r.PathValue("id"), from, to)
	if errors.Is(err, ErrVPNAccountNotFound) || errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("vpn_account_not_found", "VPN account not found."))
		return
	}
	if err != nil {
		h.databaseError(w, "get traffic usage summary", err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, summary)
}

func (h *Handler) UpdateAccountLimit(w http.ResponseWriter, r *http.Request) {
	var request UpdateTrafficLimitRequest
	if err := decodeOptionalJSON(r.Body, &request); err != nil {
		writeInvalidRequest(w, "Request body must be valid JSON.")
		return
	}

	resetDay := DefaultResetDay
	if request.ResetDay != nil {
		resetDay = *request.ResetDay
	}
	input := UpsertTrafficLimitInput{
		MonthlyLimitBytes: request.MonthlyLimitBytes,
		HardLimitEnabled:  request.HardLimitEnabled,
		SpeedLimitBps:     request.SpeedLimitBps,
		ResetDay:          resetDay,
	}
	if err := validateLimitInput(input); err != nil {
		writeInvalidRequest(w, err.Error())
		return
	}

	limit, err := h.traffic.UpsertLimit(r.Context(), r.PathValue("id"), input)
	if errors.Is(err, ErrVPNAccountNotFound) || errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("vpn_account_not_found", "VPN account not found."))
		return
	}
	if err != nil {
		h.databaseError(w, "update traffic limit", err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, limit)
}

func (h *Handler) usagePeriod(r *http.Request) (time.Time, time.Time, error) {
	now := h.now().UTC()
	defaultFrom := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	defaultTo := defaultFrom.AddDate(0, 1, 0)

	from, err := parseOptionalTime(r.URL.Query().Get("from"), defaultFrom)
	if err != nil {
		return time.Time{}, time.Time{}, errors.New("from must be RFC3339 or YYYY-MM-DD")
	}
	to, err := parseOptionalTime(r.URL.Query().Get("to"), defaultTo)
	if err != nil {
		return time.Time{}, time.Time{}, errors.New("to must be RFC3339 or YYYY-MM-DD")
	}
	if !to.After(from) {
		return time.Time{}, time.Time{}, errors.New("to must be after from")
	}
	return from, to, nil
}

func validateLimitInput(input UpsertTrafficLimitInput) error {
	if input.MonthlyLimitBytes != nil && *input.MonthlyLimitBytes < 0 {
		return errors.New("monthlyLimitBytes must be greater than or equal to zero")
	}
	if input.SpeedLimitBps != nil && *input.SpeedLimitBps <= 0 {
		return errors.New("speedLimitBps must be greater than zero")
	}
	if input.ResetDay < 1 || input.ResetDay > 28 {
		return errors.New("resetDay must be between 1 and 28")
	}
	return nil
}

func parseOptionalTime(value string, fallback time.Time) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback, nil
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UTC(), nil
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}

func bearerToken(header string) (string, bool) {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", false
	}
	return parts[1], true
}

func eventError(index int, message string) string {
	return "events[" + strconvItoa(index) + "]: " + message
}

func strconvItoa(value int) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	digits := make([]byte, 0, 10)
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	if negative {
		digits = append([]byte{'-'}, digits...)
	}
	return string(digits)
}

func decodeOptionalJSON(body io.Reader, target any) error {
	if body == nil {
		return nil
	}
	err := json.NewDecoder(body).Decode(target)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func writeInvalidRequest(w http.ResponseWriter, message string) {
	httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_request", message))
}

func writeUnauthorized(w http.ResponseWriter) {
	httpx.WriteJSON(w, http.StatusUnauthorized, httpx.Error("unauthorized", "A valid agent bearer token is required."))
}

func (h *Handler) databaseError(w http.ResponseWriter, operation string, err error) {
	h.logger.Error(operation+" failed", "error", err)
	httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Database operation failed."))
}
