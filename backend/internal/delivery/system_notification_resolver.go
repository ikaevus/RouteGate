package delivery

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SystemNotificationResolver struct {
	pool *pgxpool.Pool
}

type systemNotificationIntent struct {
	ID           string
	Kind         string
	Severity     string
	RuleKey      string
	ResourceType string
	ResourceID   string
	ReasonCode   string
	Summary      string
}

func NewSystemNotificationResolver(pool *pgxpool.Pool) *SystemNotificationResolver {
	return &SystemNotificationResolver{pool: pool}
}

func (r *SystemNotificationResolver) Resolve(ctx context.Context, item Delivery) (ResolvedMaterial, error) {
	if item.TemplateKey != TemplateSystemNotification {
		return ResolvedMaterial{}, Failure{Class: ErrorClassPermanent, Code: "material_type_unsupported"}
	}
	intent, err := r.intentForDelivery(ctx, item)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ResolvedMaterial{}, Failure{Class: ErrorClassTransient, Code: "notification_intent_unavailable"}
		}
		return ResolvedMaterial{}, Failure{Class: ErrorClassTransient, Code: "notification_resolution_failed"}
	}
	label := r.resourceLabel(ctx, intent)
	title, message := renderAlertNotification(item.Locale, intent, label)
	return ResolvedMaterial{TemplateData: TemplateData{Title: title, Message: message}}, nil
}

func (r *SystemNotificationResolver) intentForDelivery(ctx context.Context, item Delivery) (systemNotificationIntent, error) {
	var intent systemNotificationIntent
	err := r.pool.QueryRow(ctx, `
		SELECT i.id::text, i.kind, i.severity, i.rule_key, i.resource_type,
		       i.resource_id, COALESCE(i.reason_code,''), i.summary
		FROM observability_notification_deliveries d
		JOIN observability_notification_intents i ON i.id=d.intent_id
		WHERE d.delivery_id=$1::uuid
	`, item.ID).Scan(
		&intent.ID, &intent.Kind, &intent.Severity, &intent.RuleKey,
		&intent.ResourceType, &intent.ResourceID, &intent.ReasonCode, &intent.Summary,
	)
	if err == nil {
		return intent, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return systemNotificationIntent{}, err
	}

	intentID, ok := notificationIntentIDFromIdempotencyKey(item.IdempotencyKey)
	if !ok {
		return systemNotificationIntent{}, pgx.ErrNoRows
	}
	return r.intentByID(ctx, intentID)
}

func (r *SystemNotificationResolver) intentByID(ctx context.Context, intentID string) (systemNotificationIntent, error) {
	var intent systemNotificationIntent
	err := r.pool.QueryRow(ctx, `
		SELECT id::text, kind, severity, rule_key, resource_type,
		       resource_id, COALESCE(reason_code,''), summary
		FROM observability_notification_intents
		WHERE id=$1::uuid
	`, intentID).Scan(
		&intent.ID, &intent.Kind, &intent.Severity, &intent.RuleKey,
		&intent.ResourceType, &intent.ResourceID, &intent.ReasonCode, &intent.Summary,
	)
	return intent, err
}

func (r *SystemNotificationResolver) resourceLabel(ctx context.Context, intent systemNotificationIntent) string {
	if intent.ResourceType == "server" {
		var name string
		if err := r.pool.QueryRow(ctx, `SELECT name FROM servers WHERE id=$1::uuid`, intent.ResourceID).Scan(&name); err == nil && strings.TrimSpace(name) != "" {
			return strings.TrimSpace(name)
		}
	}
	return fmt.Sprintf("%s %s", strings.TrimSpace(intent.ResourceType), strings.TrimSpace(intent.ResourceID))
}

func notificationIntentIDFromIdempotencyKey(value string) (string, bool) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 3 || parts[0] != "alert-notification" || strings.TrimSpace(parts[1]) == "" {
		return "", false
	}
	return strings.TrimSpace(parts[1]), true
}

func renderAlertNotification(locale string, intent systemNotificationIntent, label string) (string, string) {
	if strings.EqualFold(strings.TrimSpace(locale), "ru") {
		return renderAlertNotificationRU(intent, label)
	}
	return renderAlertNotificationEN(intent, label)
}

func renderAlertNotificationEN(intent systemNotificationIntent, label string) (string, string) {
	condition := alertReasonTextEN(intent.ReasonCode, intent.Summary)
	switch intent.Kind {
	case "resolved":
		return "RouteGate alert resolved", fmt.Sprintf("%s: %s The incident is resolved.", label, condition)
	case "escalated":
		return "RouteGate alert escalated", fmt.Sprintf("%s: %s Severity is now critical.", label, condition)
	default:
		if intent.Severity == "critical" {
			return "Critical RouteGate alert", fmt.Sprintf("%s: %s", label, condition)
		}
		return "RouteGate warning", fmt.Sprintf("%s: %s", label, condition)
	}
}

func renderAlertNotificationRU(intent systemNotificationIntent, label string) (string, string) {
	condition := alertReasonTextRU(intent.ReasonCode)
	switch intent.Kind {
	case "resolved":
		return "Инцидент RouteGate устранён", fmt.Sprintf("%s: %s Инцидент устранён.", label, condition)
	case "escalated":
		return "Инцидент RouteGate стал критическим", fmt.Sprintf("%s: %s Уровень повышен до критического.", label, condition)
	default:
		if intent.Severity == "critical" {
			return "Критический инцидент RouteGate", fmt.Sprintf("%s: %s", label, condition)
		}
		return "Предупреждение RouteGate", fmt.Sprintf("%s: %s", label, condition)
	}
}

func alertReasonTextEN(reasonCode, fallback string) string {
	switch strings.TrimSpace(reasonCode) {
	case "telemetry_stale":
		return "Agent telemetry is stale."
	case "memory_available_critical":
		return "Available memory is critically low."
	case "memory_available_low":
		return "Available memory is low."
	case "disk_free_critical":
		return "Root filesystem free space is critically low."
	case "disk_free_low":
		return "Root filesystem free space is low."
	case "vpn_core_not_running":
		return "VPN Core service is not running."
	default:
		if strings.TrimSpace(fallback) != "" {
			return strings.TrimSpace(fallback)
		}
		return "A RouteGate health condition requires attention."
	}
}

func alertReasonTextRU(reasonCode string) string {
	switch strings.TrimSpace(reasonCode) {
	case "telemetry_stale":
		return "Телеметрия Agent устарела: Manager не получает актуальные данные."
	case "memory_available_critical":
		return "Свободной оперативной памяти критически мало."
	case "memory_available_low":
		return "Свободной оперативной памяти мало."
	case "disk_free_critical":
		return "Свободное место в корневой файловой системе критически заканчивается."
	case "disk_free_low":
		return "В корневой файловой системе мало свободного места."
	case "vpn_core_not_running":
		return "Служба VPN Core не работает."
	default:
		return "Состояние RouteGate требует внимания."
	}
}
