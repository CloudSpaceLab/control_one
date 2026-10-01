package storage

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
)

func BenchmarkExecutiveAttentionSummary100k(b *testing.B) {
	ctx := context.Background()
	store := setupPostgresStoreFull(b, ctx)

	tenant, err := store.CreateTenant(ctx, &Tenant{
		ID: uuid.New(), Name: "executive-attention-bench-" + uuid.NewString()[:6],
	})
	if err != nil {
		b.Fatal(err)
	}
	now := time.Now().UTC()

	if _, err := store.db.ExecContext(ctx, `
		INSERT INTO alerts (
			id, tenant_id, source, severity, title, state, context, opened_at
		)
		SELECT
			('10000000-0000-0000-0000-' || lpad(i::text, 12, '0'))::uuid,
			$1,
			'rule',
			CASE WHEN i % 100 = 0 THEN 'critical' ELSE 'medium' END,
			'Benchmark alert ' || i::text,
			'open',
			'{}'::jsonb,
			$2::timestamptz - ((i % 3600)::text || ' seconds')::interval
		FROM generate_series(1, 100000) AS i
	`, tenant.ID, now); err != nil {
		b.Fatal(err)
	}

	if _, err := store.db.ExecContext(ctx, `
		INSERT INTO ip_blocklist_entries (
			tenant_id, ip_cidr, scope, target_type, server_group, app, vhost,
			enforcement, protected_override, protected_override_reason,
			status, reason, score, created_at, updated_at
		)
		SELECT
			$1,
			'10.' ||
				((i / 65536) % 256)::text || '.' ||
				((i / 256) % 256)::text || '.' ||
				(i % 256)::text || '/32',
			'fleet',
			'fleet',
			'',
			'',
			'',
			'firewall',
			false,
			'',
			'proposed',
			'Correlation response: rule=benchmark; alert_id=' ||
				('10000000-0000-0000-0000-' || lpad(i::text, 12, '0')) ||
				'; mode=proposal',
			80,
			$2,
			$2
		FROM generate_series(1, 10000) AS i
	`, tenant.ID, now); err != nil {
		b.Fatal(err)
	}

	if _, err := store.db.ExecContext(ctx, `
		INSERT INTO action_plans (
			id, tenant_id, domain, action_kind, state, risk,
			scope, diff, required_approvals, maintenance_window,
			rollback_plan, verification_plan, source_ref, created_at, updated_at
		)
		SELECT
			('20000000-0000-0000-0000-' || lpad(i::text, 12, '0'))::uuid,
			$1,
			'firewall',
			'block',
			'succeeded',
			'high',
			'{}'::jsonb,
			jsonb_build_object(
				'auto_triggered', true,
				'reason',
				'Correlation response: rule=benchmark; alert_id=' ||
					('10000000-0000-0000-0000-' || lpad((i + 10000)::text, 12, '0')) ||
					'; mode=auto_temporary_block'
			),
			'{}'::jsonb,
			'{}'::jsonb,
			'{}'::jsonb,
			'{}'::jsonb,
			'{}'::jsonb,
			$2,
			$2
		FROM generate_series(1, 10000) AS i
	`, tenant.ID, now); err != nil {
		b.Fatal(err)
	}

	if _, err := store.db.ExecContext(ctx, `
		INSERT INTO action_receipts (
			id, action_plan_id, tenant_id, state, receipt, verification,
			rollback_ref, error, created_at
		)
		SELECT
			('30000000-0000-0000-0000-' || lpad(i::text, 12, '0'))::uuid,
			('20000000-0000-0000-0000-' || lpad(i::text, 12, '0'))::uuid,
			$1,
			'succeeded',
			'{"success":true}'::jsonb,
			'{"verified":true}'::jsonb,
			'',
			'',
			$2
		FROM generate_series(1, 10000) AS i
	`, tenant.ID, now); err != nil {
		b.Fatal(err)
	}

	since := now.Add(-24 * time.Hour)
	until := now.Add(time.Hour)
	summary, err := store.GetExecutiveAttentionSummary(ctx, tenant.ID, since, until, 8)
	if err != nil {
		b.Fatal(err)
	}
	if summary.Reviews != 80000 || summary.Approvals != 10000 || summary.Total != 90000 {
		b.Fatalf("unexpected seeded summary: reviews=%d approvals=%d total=%d",
			summary.Reviews, summary.Approvals, summary.Total)
	}
	if len(summary.Items) != 8 {
		b.Fatalf("top item sample=%d, want 8", len(summary.Items))
	}

	b.ReportMetric(100000, "alerts")
	b.ReportMetric(10000, "approvals")
	b.ReportMetric(10000, "receipts")
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		result, err := store.GetExecutiveAttentionSummary(ctx, tenant.ID, since, until, 8)
		if err != nil {
			b.Fatal(err)
		}
		if result.Total != 90000 {
			b.Fatal(fmt.Errorf("attention total changed: %d", result.Total))
		}
	}
}
