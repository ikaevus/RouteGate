package db

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/delivery"
	"github.com/ikaevus/routegate/backend/internal/observability"
)

func TestAlertNotificationExpandsIntoLocalizedDeliveries(t *testing.T) {
	databaseURL := os.Getenv("ROUTEGATE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ROUTEGATE_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	pool, err := Connect(ctx, databaseURL, logger)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	resetPublicSchema(t, ctx, pool)
	if err := Migrate(ctx, pool, "../../migrations", logger); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var serverID string
	if err := pool.QueryRow(ctx, `INSERT INTO servers (name,status) VALUES ('Moscow Edge','active') RETURNING id::text`).Scan(&serverID); err != nil {
		t.Fatalf("create server: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO delivery_recipients (channel,provider,address,display_name,metadata_json)
		VALUES
		('telegram','telegram','100100','Ops Telegram','{"locale":"ru"}'::jsonb),
		('email','smtp','ops@example.com','Ops Email','{"locale":"en"}'::jsonb)
	`); err != nil {
		t.Fatalf("create recipients: %v", err)
	}

	base := time.Date(2026, time.August, 14, 2, 0, 0, 0, time.UTC)
	expiresAt := base.Add(10 * time.Minute)
	check := observability.HealthCheck{
		Key: observability.CheckHostDiskCapacity,
		Resource: observability.ResourceRef{Type: "server", ID: serverID},
		Component: "host", State: observability.HealthUnhealthy, Required: true,
		ReasonCode: "disk_free_critical", Summary: "Root filesystem free space is critically low.",
		ObservedAt: base, ExpiresAt: &expiresAt,
	}
	health := observability.NewHealthRepository(pool)
	if err := health.ApplyChecks(ctx, []observability.HealthCheck{check}); err != nil {
		t.Fatalf("apply health: %v", err)
	}
	engine := observability.NewAlertEngine(logger, pool)
	if err := engine.EvaluateOnce(ctx, base); err != nil {
		t.Fatalf("create pending alert: %v", err)
	}
	if err := engine.EvaluateOnce(ctx, base.Add(observability.AlertCriticalFireDelay+time.Second)); err != nil {
		t.Fatalf("fire alert: %v", err)
	}

	var intentID, kind string
	if err := pool.QueryRow(ctx, `SELECT id::text, kind FROM observability_notification_intents`).Scan(&intentID, &kind); err != nil {
		t.Fatalf("read firing intent: %v", err)
	}
	if kind != "firing" {
		t.Fatalf("intent kind=%q, want firing", kind)
	}

	worker := observability.NewNotificationWorker(
		observability.NewNotificationRepository(pool),
		delivery.NewConfiguredSystemNotificationCreator(logger, pool),
		logger,
	)
	processed, err := worker.ProcessNext(ctx)
	if err != nil || !processed {
		t.Fatalf("expand notification: processed=%v err=%v", processed, err)
	}

	rows, err := pool.Query(ctx, `
		SELECT id::text, template_key, locale, idempotency_key
		FROM deliveries
		ORDER BY locale
	`)
	if err != nil {
		t.Fatalf("list deliveries: %v", err)
	}
	defer rows.Close()
	resolver := delivery.NewSystemNotificationResolver(pool)
	count := 0
	for rows.Next() {
		var item delivery.Delivery
		if err := rows.Scan(&item.ID, &item.TemplateKey, &item.Locale, &item.IdempotencyKey); err != nil {
			t.Fatalf("scan delivery: %v", err)
		}
		if item.TemplateKey != delivery.TemplateSystemNotification {
			t.Fatalf("template=%q", item.TemplateKey)
		}
		material, err := resolver.Resolve(ctx, item)
		if err != nil {
			t.Fatalf("resolve notification material: %v", err)
		}
		if item.Locale == "ru" {
			if !strings.Contains(material.TemplateData.Title, "Критический") || !strings.Contains(material.TemplateData.Message, "критически") {
				t.Fatalf("unexpected RU notification: %+v", material.TemplateData)
			}
		} else if item.Locale == "en" {
			if !strings.Contains(material.TemplateData.Title, "Critical") || !strings.Contains(material.TemplateData.Message, "critically") {
				t.Fatalf("unexpected EN notification: %+v", material.TemplateData)
			}
		} else {
			t.Fatalf("unexpected locale %q", item.Locale)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate deliveries: %v", err)
	}
	if count != 2 {
		t.Fatalf("deliveries=%d, want 2", count)
	}

	var linked int
	var expanded bool
	if err := pool.QueryRow(ctx, `
		SELECT (SELECT COUNT(*) FROM observability_notification_deliveries WHERE intent_id=$1::uuid),
		       expanded_at IS NOT NULL
		FROM observability_notification_intents WHERE id=$1::uuid
	`, intentID).Scan(&linked, &expanded); err != nil {
		t.Fatalf("read expansion state: %v", err)
	}
	if linked != 2 || !expanded {
		t.Fatalf("linked=%d expanded=%v, want 2/true", linked, expanded)
	}
	processed, err = worker.ProcessNext(ctx)
	if err != nil || processed {
		t.Fatalf("second expansion processed=%v err=%v, want false/nil", processed, err)
	}
}
