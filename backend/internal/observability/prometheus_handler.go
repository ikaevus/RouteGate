package observability

import (
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ikaevus/routegate/backend/internal/buildinfo"
)

const prometheusContentType = "text/plain; version=0.0.4; charset=utf-8"

type PrometheusHandler struct {
	repository      PrometheusRepository
	enabled         bool
	tokenHash       [sha256.Size]byte
	tokenConfigured bool
	now             func() time.Time
	buildInfo       func() buildinfo.Info
}

func NewPrometheusHandler(repository PrometheusRepository, enabled bool, token string) *PrometheusHandler {
	token = strings.TrimSpace(token)
	handler := &PrometheusHandler{
		repository:      repository,
		enabled:         enabled,
		tokenConfigured: token != "",
		now:             time.Now,
		buildInfo:       buildinfo.Current,
	}
	if token != "" {
		handler.tokenHash = sha256.Sum256([]byte(token))
	}
	return handler
}

func (h *PrometheusHandler) Manager(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) {
		return
	}
	w.Header().Set("Content-Type", prometheusContentType)
	writer := newPrometheusTextWriter(w)
	info := h.buildInfo()
	snapshot := h.repository.ManagerSnapshot(r.Context())

	writer.header("routegate_manager_info", "RouteGate Manager build information.", "gauge")
	writer.sampleFloat("routegate_manager_info", map[string]string{
		"version":    info.Version,
		"git_commit": info.GitCommit,
	}, 1)
	writer.header("routegate_manager_up", "Whether the RouteGate Manager HTTP process is serving metrics.", "gauge")
	writer.sampleFloat("routegate_manager_up", nil, 1)
	writer.header("routegate_postgresql_up", "Whether RouteGate Manager can reach PostgreSQL.", "gauge")
	writer.sampleBool("routegate_postgresql_up", nil, snapshot.PostgreSQLUp)
	writer.header("routegate_metrics_collection_success", "Whether the most recent Manager metrics collection completed without a database query error.", "gauge")
	writer.sampleBool("routegate_metrics_collection_success", nil, snapshot.CollectionSuccessful)
	writer.header("routegate_database_schema_version", "Latest applied RouteGate database schema version.", "gauge")
	writer.sampleInt("routegate_database_schema_version", nil, int64(snapshot.AppliedSchemaVersion))
	writer.header("routegate_database_schema_expected_version", "Database schema version expected by this RouteGate Manager build.", "gauge")
	writer.sampleInt("routegate_database_schema_expected_version", nil, int64(info.ExpectedDatabaseSchemaVersion))

	writer.header("routegate_agents", "Number of registered RouteGate Agents by current status.", "gauge")
	for _, item := range snapshot.Agents {
		writer.sampleInt("routegate_agents", item.Labels, item.Value)
	}
	writer.header("routegate_alerts_active", "Number of active RouteGate alert episodes by severity and lifecycle state.", "gauge")
	for _, item := range snapshot.Alerts {
		writer.sampleInt("routegate_alerts_active", item.Labels, item.Value)
	}
	writer.header("routegate_diagnostic_runs", "Number of RouteGate diagnostic runs by profile and status.", "gauge")
	for _, item := range snapshot.Diagnostics {
		writer.sampleInt("routegate_diagnostic_runs", item.Labels, item.Value)
	}
	writer.header("routegate_delivery_requests", "Number of durable RouteGate Delivery requests by lifecycle status.", "gauge")
	for _, item := range snapshot.Deliveries {
		writer.sampleInt("routegate_delivery_requests", item.Labels, item.Value)
	}
}

func (h *PrometheusHandler) Fleet(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) {
		return
	}
	nodes, err := h.repository.FleetSnapshot(r.Context())
	if err != nil {
		http.Error(w, "metrics collection failed", http.StatusServiceUnavailable)
		return
	}
	checks, err := h.repository.CurrentHealth(r.Context())
	if err != nil {
		http.Error(w, "metrics collection failed", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", prometheusContentType)
	writer := newPrometheusTextWriter(w)
	now := h.now().UTC()

	writer.header("routegate_agent_up", "Whether the RouteGate Agent is online and its latest observation is fresh.", "gauge")
	writer.header("routegate_agent_observation_age_seconds", "Age in seconds of the latest Agent observation as seen by Manager.", "gauge")
	writer.header("routegate_agent_observation_fresh", "Whether the latest Agent observation is within RouteGate freshness policy.", "gauge")
	writer.header("routegate_host_load1", "Host one-minute load average from the latest Agent observation.", "gauge")
	writer.header("routegate_host_load5", "Host five-minute load average from the latest Agent observation.", "gauge")
	writer.header("routegate_host_load15", "Host fifteen-minute load average from the latest Agent observation.", "gauge")
	writer.header("routegate_host_logical_cpus", "Host logical CPU count from the latest Agent observation.", "gauge")
	writer.header("routegate_host_memory_total_bytes", "Host total memory in bytes.", "gauge")
	writer.header("routegate_host_memory_available_bytes", "Host available memory in bytes.", "gauge")
	writer.header("routegate_host_memory_usage_ratio", "Host memory usage ratio from zero to one.", "gauge")
	writer.header("routegate_host_root_fs_total_bytes", "Host root filesystem total capacity in bytes.", "gauge")
	writer.header("routegate_host_root_fs_free_bytes", "Host root filesystem free capacity in bytes.", "gauge")
	writer.header("routegate_host_root_fs_usage_ratio", "Host root filesystem usage ratio from zero to one.", "gauge")
	writer.header("routegate_host_uptime_seconds", "Host uptime in seconds from the latest Agent observation.", "gauge")
	writer.header("routegate_vpn_core_info", "VPN Core identity reported by the latest Agent observation.", "gauge")
	writer.header("routegate_vpn_core_up", "Whether the configured VPN Core is installed and its service is active.", "gauge")

	for _, node := range nodes {
		labels := map[string]string{"server_id": node.ServerID}
		age := now.Sub(node.ReceivedAt.UTC()).Seconds()
		if age < 0 {
			age = 0
		}
		fresh := age <= AgentTelemetryHealthTTL.Seconds()
		writer.sampleBool("routegate_agent_up", labels, fresh && strings.EqualFold(node.AgentStatus, "online"))
		writer.sampleFloat("routegate_agent_observation_age_seconds", labels, age)
		writer.sampleBool("routegate_agent_observation_fresh", labels, fresh)
		writeOptionalFloat(writer, "routegate_host_load1", labels, node.Load1)
		writeOptionalFloat(writer, "routegate_host_load5", labels, node.Load5)
		writeOptionalFloat(writer, "routegate_host_load15", labels, node.Load15)
		writeOptionalInt(writer, "routegate_host_logical_cpus", labels, node.LogicalCPUs)
		writeOptionalInt(writer, "routegate_host_memory_total_bytes", labels, node.MemoryTotalBytes)
		writeOptionalInt(writer, "routegate_host_memory_available_bytes", labels, node.MemoryAvailableBytes)
		if node.MemoryTotalBytes != nil && node.MemoryAvailableBytes != nil && *node.MemoryTotalBytes > 0 {
			writer.sampleFloat("routegate_host_memory_usage_ratio", labels, usageRatio(*node.MemoryAvailableBytes, *node.MemoryTotalBytes))
		}
		writeOptionalInt(writer, "routegate_host_root_fs_total_bytes", labels, node.RootFSTotalBytes)
		writeOptionalInt(writer, "routegate_host_root_fs_free_bytes", labels, node.RootFSFreeBytes)
		if node.RootFSTotalBytes != nil && node.RootFSFreeBytes != nil && *node.RootFSTotalBytes > 0 {
			writer.sampleFloat("routegate_host_root_fs_usage_ratio", labels, usageRatio(*node.RootFSFreeBytes, *node.RootFSTotalBytes))
		}
		writeOptionalInt(writer, "routegate_host_uptime_seconds", labels, node.UptimeSeconds)
		coreLabels := map[string]string{
			"server_id": node.ServerID,
			"core":      strings.TrimSpace(node.VPNCoreType),
			"version":   strings.TrimSpace(node.VPNCoreVersion),
		}
		writer.sampleFloat("routegate_vpn_core_info", coreLabels, 1)
		writer.sampleBool("routegate_vpn_core_up", map[string]string{
			"server_id": node.ServerID,
			"core":      strings.TrimSpace(node.VPNCoreType),
		}, node.VPNCoreInstalled && strings.EqualFold(strings.TrimSpace(node.VPNCoreServiceState), "active"))
	}

	h.writeHealthMetrics(writer, checks, now)
}

func (h *PrometheusHandler) writeHealthMetrics(writer *prometheusTextWriter, checks []HealthCheck, now time.Time) {
	writer.header("routegate_health_check", "Current RouteGate health state for documented infrastructure checks.", "gauge")
	writer.header("routegate_server_health", "Aggregate current RouteGate health state for a managed server.", "gauge")

	byServer := make(map[string][]HealthCheck)
	for _, check := range checks {
		if check.Resource.Type != "server" || strings.TrimSpace(check.Resource.ID) == "" {
			continue
		}
		byServer[check.Resource.ID] = append(byServer[check.Resource.ID], check)
		if prometheusHealthCheckAllowed(check.Key) {
			writer.sampleFloat("routegate_health_check", map[string]string{
				"server_id": check.Resource.ID,
				"check":     check.Key,
				"state":     string(check.EffectiveState(now)),
			}, 1)
		}
	}

	serverIDs := make([]string, 0, len(byServer))
	for serverID := range byServer {
		serverIDs = append(serverIDs, serverID)
	}
	sort.Strings(serverIDs)
	for _, serverID := range serverIDs {
		aggregate := AggregateRequiredHealth(byServer[serverID], now)
		writer.sampleFloat("routegate_server_health", map[string]string{
			"server_id": serverID,
			"state":     string(aggregate.State),
		}, 1)
	}
}

func (h *PrometheusHandler) authorize(w http.ResponseWriter, r *http.Request) bool {
	if !h.enabled || !h.tokenConfigured {
		http.NotFound(w, r)
		return false
	}
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	candidate := sha256.Sum256([]byte(parts[1]))
	if subtle.ConstantTimeCompare(candidate[:], h.tokenHash[:]) != 1 {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

func prometheusHealthCheckAllowed(key string) bool {
	switch key {
	case CheckAgentTelemetryFreshness, CheckHostMemoryCapacity, CheckHostDiskCapacity, CheckVPNCoreService:
		return true
	default:
		return false
	}
}

func usageRatio(free, total int64) float64 {
	if total <= 0 {
		return 0
	}
	ratio := 1 - (float64(free) / float64(total))
	if ratio < 0 {
		return 0
	}
	if ratio > 1 {
		return 1
	}
	return ratio
}

func writeOptionalFloat(writer *prometheusTextWriter, name string, labels map[string]string, value *float64) {
	if value != nil {
		writer.sampleFloat(name, labels, *value)
	}
}

func writeOptionalInt(writer *prometheusTextWriter, name string, labels map[string]string, value *int64) {
	if value != nil {
		writer.sampleInt(name, labels, *value)
	}
}

type prometheusTextWriter struct {
	writer http.ResponseWriter
}

func newPrometheusTextWriter(writer http.ResponseWriter) *prometheusTextWriter {
	return &prometheusTextWriter{writer: writer}
}

func (w *prometheusTextWriter) header(name, help, metricType string) {
	fmt.Fprintf(w.writer, "# HELP %s %s\n", name, help)
	fmt.Fprintf(w.writer, "# TYPE %s %s\n", name, metricType)
}

func (w *prometheusTextWriter) sampleBool(name string, labels map[string]string, value bool) {
	if value {
		w.sampleInt(name, labels, 1)
		return
	}
	w.sampleInt(name, labels, 0)
}

func (w *prometheusTextWriter) sampleInt(name string, labels map[string]string, value int64) {
	w.sample(name, labels, strconv.FormatInt(value, 10))
}

func (w *prometheusTextWriter) sampleFloat(name string, labels map[string]string, value float64) {
	w.sample(name, labels, strconv.FormatFloat(value, 'g', -1, 64))
}

func (w *prometheusTextWriter) sample(name string, labels map[string]string, value string) {
	fmt.Fprint(w.writer, name)
	if len(labels) > 0 {
		keys := make([]string, 0, len(labels))
		for key := range labels {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		fmt.Fprint(w.writer, "{")
		for i, key := range keys {
			if i > 0 {
				fmt.Fprint(w.writer, ",")
			}
			fmt.Fprintf(w.writer, "%s=\"%s\"", key, prometheusEscapeLabel(labels[key]))
		}
		fmt.Fprint(w.writer, "}")
	}
	fmt.Fprintf(w.writer, " %s\n", value)
}

func prometheusEscapeLabel(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\n", "\\n")
	return strings.ReplaceAll(value, "\"", "\\\"")
}
