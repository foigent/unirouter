package admin

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestP6F20AuditOutboxMaterializesExactlyOnceIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	var orgID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name, status, org_type)
		 VALUES ('p6f20-audit', 'active', 'standard') RETURNING id`,
	).Scan(&orgID))

	options := outbox.DefaultOptions()
	options.WorkerCount = 1
	service, err := outbox.New(pool, options)
	require.NoError(t, err)
	handler := NewHandler(pool, "jwt-secret", nil, nil)
	handler.SetOutbox(service)
	require.NoError(t, service.RegisterHandler(outbox.EventAudit, handler.ProcessAuditOutbox))

	auditCtx := context.WithValue(ctx, middleware.CtxOrgID, orgID)
	auditCtx = context.WithValue(auditCtx, middleware.CtxUserID, int64(0))
	auditCtx = context.WithValue(auditCtx, middleware.CtxRequestID, "op_p6f20_audit")
	detail := map[string]interface{}{"after": map[string]interface{}{"enabled": true}}
	handler.recordAudit(auditCtx, "setting.update", "setting", "feature.flag", detail, "127.0.0.1")

	var queued, rows int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM internal_outbox_events`).Scan(&queued))
	require.Equal(t, 1, queued)
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs`).Scan(&rows))
	require.Zero(t, rows)
	processed, err := service.DrainOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs`).Scan(&rows))
	require.Equal(t, 1, rows)

	handler.recordAudit(auditCtx, "setting.update", "setting", "feature.flag", detail, "127.0.0.1")
	processed, err = service.DrainOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs`).Scan(&rows))
	require.Equal(t, 1, rows)

	background := auditOutboxPayload{
		OrgID:       orgID,
		Action:      "background.reconcile",
		Resource:    "runtime",
		ResourceID:  "same-resource",
		Detail:      json.RawMessage(`{"status":"same"}`),
		BeforeState: json.RawMessage(`{}`),
		AfterState:  json.RawMessage(`{"status":"same"}`),
		Source:      "system",
		OccurredAt:  time.Unix(1_700_000_000, 1).UTC(),
	}
	require.NoError(t, enqueueAuditPayload(ctx, service, background))
	background.OccurredAt = background.OccurredAt.Add(time.Nanosecond)
	require.NoError(t, enqueueAuditPayload(ctx, service, background))
	processed, err = service.DrainOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, processed, "identical uncorrelated background events are distinct facts")
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs`).Scan(&rows))
	require.Equal(t, 3, rows)
}
