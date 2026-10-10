package gateway

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/guardrail"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestGuardrailTenantIsolationMigration(t *testing.T) {
	pool := testutil.SetupPreRetirementTestDB(t)

	t.Run("platform cache and tenant-scope quotas are independently atomic", func(t *testing.T) {
		ctx := t.Context()
		orgA := insertGuardrailTestOrg(t, pool, "guardrail-independent-quota-a")
		orgB := insertGuardrailTestOrg(t, pool, "guardrail-independent-quota-b")
		orgC := insertGuardrailTestOrg(t, pool, "guardrail-independent-quota-c")
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(context.Background())
		if _, err := tx.Exec(ctx, `SELECT
			set_config('unirouter.guardrail_org_active_policy_limit','1',TRUE),
			set_config('unirouter.guardrail_platform_active_policy_limit','1',TRUE),
			set_config('unirouter.guardrail_cached_policy_limit','4',TRUE),
			set_config('unirouter.guardrail_cached_tenant_limit','2',TRUE)`); err != nil {
			t.Fatal(err)
		}
		insert := func(orgID any, name string) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO guardrail_policies (org_id,name,stage,detector_ref,action,mode,failure_mode,config)
				VALUES ($1,$2,'input','builtin:pii_secrets','audit','shadow','degraded_allow','{}')`, orgID, name)
			return err
		}
		if err := insert(nil, "independent-platform"); err != nil {
			t.Fatal(err)
		}
		if err := insert(orgA, "independent-org-a"); err != nil {
			t.Fatal(err)
		}
		if err := insert(orgB, "independent-org-b"); err != nil {
			t.Fatal(err)
		}

		assertRejected := func(savepoint, constraint string, insertFn func() error) {
			t.Helper()
			if _, err := tx.Exec(ctx, "SAVEPOINT "+savepoint); err != nil {
				t.Fatal(err)
			}
			assertGuardrailQuotaConstraint(t, insertFn(), constraint)
			if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT "+savepoint); err != nil {
				t.Fatal(err)
			}
		}
		assertRejected("platform_limit", "guardrail_platform_active_policy_quota", func() error {
			return insert(nil, "independent-platform-overflow")
		})
		assertRejected("org_limit", "guardrail_org_active_policy_quota", func() error {
			return insert(orgA, "independent-org-overflow")
		})
		assertRejected("tenant_limit", "guardrail_active_tenant_scope_quota", func() error {
			return insert(orgC, "independent-tenant-overflow")
		})
		if _, err := tx.Exec(ctx, `SELECT
			set_config('unirouter.guardrail_org_active_policy_limit','2',TRUE),
			set_config('unirouter.guardrail_platform_active_policy_limit','2',TRUE),
			set_config('unirouter.guardrail_cached_policy_limit','3',TRUE),
			set_config('unirouter.guardrail_cached_tenant_limit','3',TRUE)`); err != nil {
			t.Fatal(err)
		}
		assertRejected("cache_limit", "guardrail_active_policy_cache_quota", func() error {
			return insert(orgA, "independent-cache-overflow")
		})
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("default and concurrent policy quotas are atomic without deleting history", func(t *testing.T) {
		ctx := t.Context()
		defaultOrgID := insertGuardrailTestOrg(t, pool, "guardrail-default-quota")
		for index := 0; index < guardrail.DefaultOptions().MaxActivePoliciesPerOrg; index++ {
			_, err := pool.Exec(ctx, `
				INSERT INTO guardrail_policies (org_id,name,stage,detector_ref,action,mode,failure_mode,config)
				VALUES ($1,$2,'input','builtin:pii_secrets','audit','shadow','degraded_allow','{}')`,
				defaultOrgID, fmt.Sprintf("default-policy-%03d", index))
			if err != nil {
				t.Fatalf("insert default policy %d: %v", index, err)
			}
		}
		_, err := pool.Exec(ctx, `
			INSERT INTO guardrail_policies (org_id,name,stage,detector_ref,action,mode,failure_mode,config)
			VALUES ($1,'default-policy-overflow','input','builtin:pii_secrets','audit','shadow','degraded_allow','{}')`, defaultOrgID)
		assertGuardrailQuotaConstraint(t, err, "guardrail_org_active_policy_quota")
		_, err = pool.Exec(ctx, `
			INSERT INTO guardrail_policies (org_id,name,stage,detector_ref,action,mode,failure_mode,config)
			VALUES ($1,'disabled-policy-history','input','builtin:pii_secrets','audit','disabled','degraded_allow','{}')`, defaultOrgID)
		if err != nil {
			t.Fatalf("disabled history was incorrectly charged to active quota: %v", err)
		}
		var activeCount, totalCount int
		if err := pool.QueryRow(ctx, `
			SELECT COUNT(*) FILTER (WHERE mode <> 'disabled'),COUNT(*)
			FROM guardrail_policies WHERE org_id=$1`, defaultOrgID).Scan(&activeCount, &totalCount); err != nil {
			t.Fatal(err)
		}
		if activeCount != 256 || totalCount != 257 {
			t.Fatalf("active/total policies = %d/%d, want 256/257", activeCount, totalCount)
		}

		concurrentOrgID := insertGuardrailTestOrg(t, pool, "guardrail-concurrent-quota")
		ready := make(chan struct{}, 2)
		start := make(chan struct{})
		results := make(chan error, 2)
		var workers sync.WaitGroup
		for index := 0; index < 2; index++ {
			workers.Add(1)
			go func(index int) {
				defer workers.Done()
				tx, err := pool.Begin(context.Background())
				if err != nil {
					results <- err
					return
				}
				defer tx.Rollback(context.Background())
				_, err = tx.Exec(context.Background(), `SELECT
					set_config('unirouter.guardrail_org_active_policy_limit','1',TRUE),
					set_config('unirouter.guardrail_platform_active_policy_limit','256',TRUE),
					set_config('unirouter.guardrail_cached_policy_limit','4096',TRUE),
					set_config('unirouter.guardrail_cached_tenant_limit','4096',TRUE)`)
				if err != nil {
					results <- err
					return
				}
				ready <- struct{}{}
				<-start
				_, err = tx.Exec(context.Background(), `
					INSERT INTO guardrail_policies (org_id,name,stage,detector_ref,action,mode,failure_mode,config)
					VALUES ($1,$2,'input','builtin:pii_secrets','audit','shadow','degraded_allow','{}')`,
					concurrentOrgID, fmt.Sprintf("concurrent-policy-%d", index))
				if err == nil {
					err = tx.Commit(context.Background())
				}
				results <- err
			}(index)
		}
		<-ready
		<-ready
		close(start)
		workers.Wait()
		close(results)
		successes, quotaFailures := 0, 0
		for result := range results {
			if result == nil {
				successes++
				continue
			}
			var pgErr *pgconn.PgError
			if errors.As(result, &pgErr) && pgErr.ConstraintName == "guardrail_org_active_policy_quota" {
				quotaFailures++
				continue
			}
			t.Fatalf("unexpected concurrent insert result: %v", result)
		}
		if successes != 1 || quotaFailures != 1 {
			t.Fatalf("concurrent outcomes success/quota = %d/%d, want 1/1", successes, quotaFailures)
		}
	})

	t.Run("real refresh isolates an invalid tenant without blocking a healthy tenant", func(t *testing.T) {
		ctx := t.Context()
		badOrgID := insertGuardrailTestOrg(t, pool, "guardrail-refresh-bad")
		goodOrgID := insertGuardrailTestOrg(t, pool, "guardrail-refresh-good")
		if _, err := pool.Exec(ctx, `
			INSERT INTO guardrail_policies (org_id,name,stage,detector_ref,action,mode,failure_mode,config)
			VALUES ($1,'invalid-tenant-policy','input','external:not-registered','audit','shadow','degraded_allow','{}')`, badOrgID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO guardrail_policies (org_id,name,stage,detector_ref,action,mode,failure_mode,config)
			VALUES ($1,'healthy-tenant-policy','input','builtin:pii_secrets','block','enforce','degraded_allow','{"entities":["email"]}')`, goodOrgID); err != nil {
			t.Fatal(err)
		}
		service, err := guardrail.NewService(pool, nil, []byte("0123456789abcdef0123456789abcdef"), guardrail.DefaultOptions())
		if err != nil {
			t.Fatal(err)
		}
		if err := service.Refresh(ctx); err != nil {
			t.Fatalf("tenant-local invalid policy aborted real refresh: %v", err)
		}
		badResult := service.Pin(guardrail.Subject{OrgID: badOrgID, Model: "gpt"}).Evaluate(
			ctx, guardrail.StageInput, []guardrail.Text{{Content: "safe"}},
		)
		if badResult.Decision != guardrail.DecisionBlock || badResult.BlockedBy != "guardrail:snapshot:tenant" {
			t.Fatalf("invalid tenant did not fail closed locally: %+v", badResult)
		}
		goodResult := service.Pin(guardrail.Subject{OrgID: goodOrgID, Model: "gpt"}).Evaluate(
			ctx, guardrail.StageInput, []guardrail.Text{{Content: "alice@example.com"}},
		)
		if goodResult.Decision != guardrail.DecisionBlock || goodResult.BlockedBy == "guardrail:snapshot:tenant" {
			t.Fatalf("healthy tenant was not independently refreshed: %+v", goodResult)
		}
	})

	t.Run("gap batches are metadata-only exact and idempotent", func(t *testing.T) {
		ctx := t.Context()
		orgID := insertGuardrailTestOrg(t, pool, "guardrail-gap-evidence")
		relay := &Relay{db: pool}
		occurredAt := time.Now().UTC().Truncate(streamAdvisoryGapWindow).Add(time.Second)
		event := streamAdvisoryGapEvent{
			relay: relay, orgID: orgID, requestID: "req-gap-exact",
			policies: []guardrail.PolicyIdentity{{Ref: "guardrail:advisory", Version: 5}},
			reason:   streamAdvisoryGapOverload, occurredAt: occurredAt,
		}
		batch := buildStreamAdvisoryGapBatches([]streamAdvisoryGapEvent{event, event, event})[0]
		if err := persistStreamAdvisoryGapBatch(ctx, batch); err != nil {
			t.Fatal(err)
		}
		if err := persistStreamAdvisoryGapBatch(ctx, batch); err != nil {
			t.Fatalf("idempotent batch retry failed: %v", err)
		}
		secondBatch := buildStreamAdvisoryGapBatches([]streamAdvisoryGapEvent{event, event})[0]
		if err := persistStreamAdvisoryGapBatch(ctx, secondBatch); err != nil {
			t.Fatal(err)
		}
		var receiptRows, batchRows int
		var droppedCount int64
		if err := pool.QueryRow(ctx, `
			SELECT COUNT(*),COALESCE(SUM(dropped_count),0)
			FROM guardrail_advisory_gap_receipts
			WHERE org_id=$1 AND request_id=$2 AND policy_ref=$3`,
			orgID, event.requestID, event.policies[0].Ref).Scan(&receiptRows, &droppedCount); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM guardrail_advisory_gap_batches WHERE batch_id=$1 OR batch_id=$2`,
			batch.id, secondBatch.id).Scan(&batchRows); err != nil {
			t.Fatal(err)
		}
		if receiptRows != 1 || batchRows != 2 || droppedCount != 5 {
			t.Fatalf("receipt rows/batches/drops = %d/%d/%d, want 1/2/5", receiptRows, batchRows, droppedCount)
		}
		var rawColumns int
		if err := pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM information_schema.columns
			WHERE table_schema='public' AND table_name='guardrail_advisory_gap_receipts'
			  AND column_name = ANY(ARRAY['content','text','payload','body','findings','fingerprints'])`).Scan(&rawColumns); err != nil {
			t.Fatal(err)
		}
		if rawColumns != 0 {
			t.Fatalf("gap receipt schema has %d raw-content columns", rawColumns)
		}
	})

	t.Run("rollback refuses to delete durable gap history", func(t *testing.T) {
		ctx := t.Context()
		downSQL, err := os.ReadFile(filepath.Join(testutil.PreRetirementMigrationsDir(t), "000039_v1_guardrail_tenant_isolation.down.sql"))
		if err != nil {
			t.Fatal(err)
		}
		conn, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Release()
		_, err = conn.Exec(ctx, string(downSQL))
		if err == nil || !strings.Contains(err.Error(), "rollback refused") {
			t.Fatalf("durable gap rollback error = %v, want explicit refusal", err)
		}
		if _, rollbackErr := conn.Exec(ctx, "ROLLBACK"); rollbackErr != nil {
			t.Fatalf("clear refused rollback transaction: %v", rollbackErr)
		}
		var migrationRows, receiptRows int
		if err := conn.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version='000039_v1_guardrail_tenant_isolation'`).Scan(&migrationRows); err != nil {
			t.Fatal(err)
		}
		if err := conn.QueryRow(ctx, `SELECT COUNT(*) FROM guardrail_advisory_gap_receipts`).Scan(&receiptRows); err != nil {
			t.Fatal(err)
		}
		if migrationRows != 1 || receiptRows == 0 {
			t.Fatalf("refused rollback changed migration/history rows = %d/%d", migrationRows, receiptRows)
		}
	})
}

func insertGuardrailTestOrg(t *testing.T, database db.DBQuerier, name string) int64 {
	t.Helper()
	var orgID int64
	if err := database.QueryRow(t.Context(), `
		INSERT INTO orgs (name,status,org_type) VALUES ($1,'active','standard') RETURNING id`, name).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	return orgID
}

func assertGuardrailQuotaConstraint(t *testing.T, err error, constraint string) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.ConstraintName != constraint {
		t.Fatalf("quota error = %v, want constraint %s", err, constraint)
	}
}
