package delivery

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type SystemNotificationResolver struct {
	pool *pgxpool.Pool
}

func NewSystemNotificationResolver(pool *pgxpool.Pool) *SystemNotificationResolver {
	return &SystemNotificationResolver{pool: pool}
}

func (r *SystemNotificationResolver) Resolve(ctx context.Context, delivery Delivery) (ResolvedMaterial, error) {
	if delivery.TemplateKey != TemplateSystemNotification {
		return ResolvedMaterial{}, Failure{Class: ErrorClassPermanent, Code: "unsupported_system_notification_template"}
	}

	var (
		toState      string
		toSeverity   string
		transitionReason string
		ruleKey      string
		resourceType string
		resourceID   string
		alertSummary string
		alertReason  string
	)
	if err := r.pool.QueryRow(ctx, `
		SELECT
			t.to_state,
			COALESCE(t.to_severity, ''),
			COALESCE(t.reason_code, ''),
			a.rule_key,
			a.resource_type,
			a.resource_id,
			a.summary,
			COALESCE(a.reason_code, '')
		FROM delivery_system_notifications dsn
		JOIN observability_alert_transitions t ON t.id = dsn.alert_transition_id
		JOIN observability_alerts a ON a.id = t.alert_id
		WHERE dsn.delivery_id = $1::uuid
	`, delivery.ID).Scan(
		&toState,
		&toSeverity,
		&transitionReason,
		&ruleKey,
		&resourceType,
		&resourceID,
		&alertSummary,
		&alertReason,
	); err != nil {
		return ResolvedMaterial{}, err
	}

	locale := strings.ToLower(strings.TrimSpace(delivery.Locale))
	if locale != "ru" {
		locale = "en"
	}
	title, message := systemNotificationText(locale, toState, toSeverity, resourceType, resourceID, alertSummary, firstNonBlank(transitionReason, alertReason, ruleKey))
	return ResolvedMaterial{TemplateData: TemplateData{Title: title, Message: message}}, nil
}

func systemNotificationText(locale, state, severity, resourceType, resourceID, summary, reason string) (string, string) {
	severity = strings.ToLower(strings.TrimSpace(severity))
	state = strings.ToLower(strings.TrimSpace(state))
	resource := strings.TrimSpace(resourceType + " " + resourceID)
	if resource == "" {
		resource = "RouteGate"
	}
	if locale == "ru" {
		if state == "resolved" {
			return "RouteGate: проблема устранена", fmt.Sprintf("%s\nРесурс: %s\nПричина: %s", summary, resource, reason)
		}
		label := "Предупреждение"
		if severity == "critical" {
			label = "Критическая проблема"
		}
		return "RouteGate: " + label, fmt.Sprintf("%s\nРесурс: %s\nПричина: %s", summary, resource, reason)
	}
	if state == "resolved" {
		return "RouteGate: issue resolved", fmt.Sprintf("%s\nResource: %s\nReason: %s", summary, resource, reason)
	}
	label := "Warning"
	if severity == "critical" {
		label = "Critical issue"
	}
	return "RouteGate: " + label, fmt.Sprintf("%s\nResource: %s\nReason: %s", summary, resource, reason)
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return "unknown"
}
