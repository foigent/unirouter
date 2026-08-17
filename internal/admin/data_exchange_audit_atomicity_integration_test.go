//go:build integration

package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestDataExchangeMutationAuditAtomicityIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID, userID, pendingItemID, approvedItemID, existingExchangeID := seedDataExchangeAuditFixture(t, pool)
	require.NoError(t, installRejectDataExchangeAuditTrigger(ctx, pool))
	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	t.Run("audit failure rolls back every covered mutation", func(t *testing.T) {
		itemCreate := serveDataExchangeHandler(h.CreateDataExchangeItem,
			dataExchangeIntegrationRequest(http.MethodPost, "/api/v1/data-exchange/items", `{
				"approval_mode":"controlled_scenario",
				"scenario_label":"rollback item",
				"source_instance":"dev-a",
				"data_classes":["request_trace_events"]
			}`, orgID, userID, "data-exchange-reject-item-create"))
		assertDataExchangeAuditFailure(t, itemCreate)
		assertDataExchangeCount(t, pool, `SELECT COUNT(*) FROM integration_exchange_items WHERE scenario_label = 'rollback item'`, 0)

		itemUpdateReq := dataExchangeIntegrationRequest(http.MethodPut, fmt.Sprintf("/api/v1/data-exchange/items/%d", pendingItemID), `{
			"approval_status":"unavailable",
			"reason":"rollback reason"
		}`, orgID, userID, "data-exchange-reject-item-update")
		itemUpdateReq.SetPathValue("id", fmt.Sprintf("%d", pendingItemID))
		itemUpdate := serveDataExchangeHandler(h.UpdateDataExchangeItem, itemUpdateReq)
		assertDataExchangeAuditFailure(t, itemUpdate)
		var itemStatus, itemReason string
		require.NoError(t, pool.QueryRow(ctx, `SELECT approval_status, reason FROM integration_exchange_items WHERE id = $1`, pendingItemID).Scan(&itemStatus, &itemReason))
		require.Equal(t, "pending", itemStatus)
		require.Equal(t, "original reason", itemReason)

		exchangeCreate := serveDataExchangeHandler(h.CreateDataExchange,
			dataExchangeIntegrationRequest(http.MethodPost, "/api/v1/data-exchange/exchanges", fmt.Sprintf(`{
				"exchange_id":"uxchg_rollback_001",
				"source_instance":"dev-a",
				"schema_version":"unirouter.prd5.xinsight.snapshot.v1",
				"approval_manifest_sha256":"sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
				"artifact_sha256":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"artifact_ref":"evidence/rollback.json",
				"item_ids":[%d],
				"request_count":1,
				"input_event_count":1,
				"generated_at":"2026-07-10T08:00:00Z"
			}`, approvedItemID), orgID, userID, "data-exchange-reject-exchange-create"))
		assertDataExchangeAuditFailure(t, exchangeCreate)
		assertDataExchangeCount(t, pool, `SELECT COUNT(*) FROM integration_exchanges WHERE exchange_id = 'uxchg_rollback_001'`, 0)
		assertDataExchangeCount(t, pool, `SELECT COUNT(*) FROM integration_exchange_members m JOIN integration_exchanges e ON e.id = m.exchange_record_id WHERE e.exchange_id = 'uxchg_rollback_001'`, 0)

		receiptReq := dataExchangeIntegrationRequest(http.MethodPost, fmt.Sprintf("/api/v1/data-exchange/exchanges/%d/receipts", existingExchangeID), dataExchangeReceiptBody("2026-07-10T08:01:00Z"), orgID, userID, "data-exchange-reject-receipt")
		receiptReq.SetPathValue("id", fmt.Sprintf("%d", existingExchangeID))
		receipt := serveDataExchangeHandler(h.CreateDataExchangeReceipt, receiptReq)
		assertDataExchangeAuditFailure(t, receipt)
		assertDataExchangeCount(t, pool, `SELECT COUNT(*) FROM integration_exchange_receipts WHERE exchange_record_id = $1`, 0, existingExchangeID)

		for _, action := range []string{"handed_off", "redelivered"} {
			deliveryReq := dataExchangeIntegrationRequest(http.MethodPost, fmt.Sprintf("/api/v1/data-exchange/exchanges/%d/deliveries", existingExchangeID), fmt.Sprintf(`{
				"action":%q,
				"handoff_ref":"handoff/rollback-%s",
				"note":"rollback delivery"
			}`, action, action), orgID, userID, "data-exchange-reject-delivery-"+action)
			deliveryReq.SetPathValue("id", fmt.Sprintf("%d", existingExchangeID))
			delivery := serveDataExchangeHandler(h.CreateDataExchangeDelivery, deliveryReq)
			assertDataExchangeAuditFailure(t, delivery)
		}
		assertDataExchangeCount(t, pool, `SELECT COUNT(*) FROM integration_exchange_deliveries WHERE exchange_record_id = $1`, 0, existingExchangeID)
		var workflowStatus string
		require.NoError(t, pool.QueryRow(ctx, `SELECT workflow_status FROM integration_exchanges WHERE id = $1`, existingExchangeID).Scan(&workflowStatus))
		require.Equal(t, "generated", workflowStatus)

		artifactDeleteReq := dataExchangeIntegrationRequest(http.MethodPost, fmt.Sprintf("/api/v1/data-exchange/exchanges/%d/artifact-delete", existingExchangeID), "", orgID, userID, "data-exchange-reject-artifact-delete")
		artifactDeleteReq.SetPathValue("id", fmt.Sprintf("%d", existingExchangeID))
		artifactDelete := serveDataExchangeHandler(h.DeleteDataExchangeArtifact, artifactDeleteReq)
		assertDataExchangeAuditFailure(t, artifactDelete)
		require.NoError(t, pool.QueryRow(ctx, `SELECT workflow_status FROM integration_exchanges WHERE id = $1`, existingExchangeID).Scan(&workflowStatus))
		require.Equal(t, "generated", workflowStatus)
		assertDataExchangeCount(t, pool, `SELECT COUNT(*) FROM audit_logs WHERE request_id LIKE 'data-exchange-reject-%'`, 0)
	})

	t.Run("successful controls commit mutation and scoped audit", func(t *testing.T) {
		itemCreate := serveDataExchangeHandler(h.CreateDataExchangeItem,
			dataExchangeIntegrationRequest(http.MethodPost, "/api/v1/data-exchange/items", `{
				"approval_mode":"controlled_scenario",
				"scenario_label":"success item",
				"source_instance":"dev-a",
				"data_classes":["request_trace_events"]
			}`, orgID, userID, "data-exchange-success-item-create"))
		require.Equal(t, http.StatusCreated, itemCreate.Code, itemCreate.Body.String())
		createdItemID := dataExchangeResponseID(t, itemCreate)
		assertDataExchangeAudit(t, pool, "data-exchange-success-item-create", orgID, userID, "data_exchange.item.create", "integration_exchange_item", createdItemID)

		itemUpdateReq := dataExchangeIntegrationRequest(http.MethodPut, fmt.Sprintf("/api/v1/data-exchange/items/%d", pendingItemID), `{
			"approval_status":"unavailable",
			"reason":"success reason"
		}`, orgID, userID, "data-exchange-success-item-update")
		itemUpdateReq.SetPathValue("id", fmt.Sprintf("%d", pendingItemID))
		itemUpdate := serveDataExchangeHandler(h.UpdateDataExchangeItem, itemUpdateReq)
		require.Equal(t, http.StatusOK, itemUpdate.Code, itemUpdate.Body.String())
		assertDataExchangeAudit(t, pool, "data-exchange-success-item-update", orgID, userID, "data_exchange.item.update", "integration_exchange_item", pendingItemID)

		exchangeCreate := serveDataExchangeHandler(h.CreateDataExchange,
			dataExchangeIntegrationRequest(http.MethodPost, "/api/v1/data-exchange/exchanges", fmt.Sprintf(`{
				"exchange_id":"uxchg_success_001",
				"source_instance":"dev-a",
				"schema_version":"unirouter.prd5.xinsight.snapshot.v1",
				"approval_manifest_sha256":"sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
				"artifact_sha256":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"artifact_ref":"evidence/success.json",
				"item_ids":[%d],
				"request_count":1,
				"input_event_count":1,
				"generated_at":"2026-07-10T08:00:00Z"
			}`, approvedItemID), orgID, userID, "data-exchange-success-exchange-create"))
		require.Equal(t, http.StatusCreated, exchangeCreate.Code, exchangeCreate.Body.String())
		createdExchangeID := dataExchangeResponseID(t, exchangeCreate)
		assertDataExchangeCount(t, pool, `SELECT COUNT(*) FROM integration_exchange_members WHERE exchange_record_id = $1 AND item_id = $2`, 1, createdExchangeID, approvedItemID)
		assertDataExchangeAudit(t, pool, "data-exchange-success-exchange-create", orgID, userID, "data_exchange.exchange.create", "integration_exchange", createdExchangeID)

		receiptReq := dataExchangeIntegrationRequest(http.MethodPost, fmt.Sprintf("/api/v1/data-exchange/exchanges/%d/receipts", existingExchangeID), dataExchangeReceiptBody("2026-07-10T08:02:00Z"), orgID, userID, "data-exchange-success-receipt")
		receiptReq.SetPathValue("id", fmt.Sprintf("%d", existingExchangeID))
		receipt := serveDataExchangeHandler(h.CreateDataExchangeReceipt, receiptReq)
		require.Equal(t, http.StatusCreated, receipt.Code, receipt.Body.String())
		assertDataExchangeAudit(t, pool, "data-exchange-success-receipt", orgID, userID, "data_exchange.receipt.record", "integration_exchange", existingExchangeID)

		for _, action := range []string{"handed_off", "redelivered"} {
			requestID := "data-exchange-success-delivery-" + action
			deliveryReq := dataExchangeIntegrationRequest(http.MethodPost, fmt.Sprintf("/api/v1/data-exchange/exchanges/%d/deliveries", existingExchangeID), fmt.Sprintf(`{
				"action":%q,
				"handoff_ref":"handoff/success-%s",
				"note":"success delivery"
			}`, action, action), orgID, userID, requestID)
			deliveryReq.SetPathValue("id", fmt.Sprintf("%d", existingExchangeID))
			delivery := serveDataExchangeHandler(h.CreateDataExchangeDelivery, deliveryReq)
			require.Equal(t, http.StatusCreated, delivery.Code, delivery.Body.String())
			assertDataExchangeAudit(t, pool, requestID, orgID, userID, "data_exchange."+action, "integration_exchange", existingExchangeID)
		}
		assertDataExchangeCount(t, pool, `SELECT COUNT(*) FROM integration_exchange_deliveries WHERE exchange_record_id = $1`, 2, existingExchangeID)

		artifactDeleteReq := dataExchangeIntegrationRequest(http.MethodPost, fmt.Sprintf("/api/v1/data-exchange/exchanges/%d/artifact-delete", existingExchangeID), "", orgID, userID, "data-exchange-success-artifact-delete")
		artifactDeleteReq.SetPathValue("id", fmt.Sprintf("%d", existingExchangeID))
		artifactDelete := serveDataExchangeHandler(h.DeleteDataExchangeArtifact, artifactDeleteReq)
		require.Equal(t, http.StatusOK, artifactDelete.Code, artifactDelete.Body.String())
		var workflowStatus string
		require.NoError(t, pool.QueryRow(ctx, `SELECT workflow_status FROM integration_exchanges WHERE id = $1`, existingExchangeID).Scan(&workflowStatus))
		require.Equal(t, "artifact_deleted", workflowStatus)
		assertDataExchangeAudit(t, pool, "data-exchange-success-artifact-delete", orgID, userID, "data_exchange.artifact.mark_deleted", "integration_exchange", existingExchangeID)
	})
}

func seedDataExchangeAuditFixture(t *testing.T, pool *pgxpool.Pool) (orgID, userID, pendingItemID, approvedItemID, exchangeID int64) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs (name, status, org_type) VALUES ('data-exchange-audit-org', 'active', 'standard') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO users (org_id, username, password_hash, role, status) VALUES ($1, 'data-exchange-auditor', 'test-hash', 'platform_admin', 'active') RETURNING id`, orgID).Scan(&userID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO integration_exchange_items
		(org_id, approval_mode, scenario_label, source_instance, data_classes, approval_status, reason, created_by)
		VALUES ($1, 'controlled_scenario', 'pending item', 'dev-a', ARRAY['request_trace_events'], 'pending', 'original reason', $2) RETURNING id`, orgID, userID).Scan(&pendingItemID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO integration_exchange_items
		(org_id, approval_mode, request_id, scenario_label, source_instance, data_classes, approval_status, reason, approved_by, approved_at, created_by)
		VALUES ($1, 'exact_request', 'req-approved-001', 'approved item', 'dev-a', ARRAY['request_trace_events'], 'approved', 'approved', $2, NOW(), $2) RETURNING id`, orgID, userID).Scan(&approvedItemID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO integration_exchanges
		(org_id, exchange_id, downstream_system, source_instance, schema_version, approval_manifest_sha256,
		 artifact_sha256, artifact_ref, workflow_status, request_count, input_event_count, generated_by, generated_at)
		VALUES ($1, 'uxchg_existing_001', 'x-insight', 'dev-a', 'unirouter.prd5.xinsight.snapshot.v1',
		 'sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc',
		 'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
		 'evidence/existing.json', 'generated', 1, 1, $2, '2026-07-10T08:00:00Z') RETURNING id`, orgID, userID).Scan(&exchangeID))
	return orgID, userID, pendingItemID, approvedItemID, exchangeID
}

func installRejectDataExchangeAuditTrigger(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		CREATE FUNCTION reject_data_exchange_audit_for_test() RETURNS trigger AS $$
		BEGIN
			IF NEW.request_id LIKE 'data-exchange-reject-%'
			   AND NEW.action IN (
				'data_exchange.item.create', 'data_exchange.item.update',
				'data_exchange.exchange.create', 'data_exchange.receipt.record',
				'data_exchange.handed_off', 'data_exchange.redelivered',
				'data_exchange.artifact.mark_deleted'
			   ) THEN
				RAISE EXCEPTION 'forced data-exchange audit failure';
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER reject_data_exchange_audit_for_test
		BEFORE INSERT ON audit_logs
		FOR EACH ROW EXECUTE FUNCTION reject_data_exchange_audit_for_test();`)
	return err
}

func dataExchangeIntegrationRequest(method, target, body string, orgID, userID int64, requestID string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, orgID)
	ctx = context.WithValue(ctx, middleware.CtxUserID, userID)
	ctx = context.WithValue(ctx, middleware.CtxUserRole, "platform_admin")
	ctx = context.WithValue(ctx, middleware.CtxRequestID, requestID)
	return req.WithContext(ctx)
}

func dataExchangeReceiptBody(checkedAt string) string {
	return fmt.Sprintf(`{
		"exchange_id":"uxchg_existing_001",
		"schema_version":"xinsight.import-receipt.v1",
		"source_system":"unirouter",
		"source_instance":"dev-a",
		"artifact_schema_version":"unirouter.prd5.xinsight.snapshot.v1",
		"artifact_sha256":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"approval_manifest_sha256":"sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		"result_status":"persisted",
		"checked_at":%q
	}`, checkedAt)
}

func serveDataExchangeHandler(handler http.HandlerFunc, req *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	handler(recorder, req)
	return recorder
}

func assertDataExchangeAuditFailure(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	require.Equal(t, http.StatusInternalServerError, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), "Audit failed")
}

func assertDataExchangeCount(t *testing.T, pool *pgxpool.Pool, query string, want int, args ...any) {
	t.Helper()
	var got int
	require.NoError(t, pool.QueryRow(context.Background(), query, args...).Scan(&got))
	require.Equal(t, want, got)
}

func dataExchangeResponseID(t *testing.T, recorder *httptest.ResponseRecorder) int64 {
	t.Helper()
	var response struct {
		ID int64 `json:"id"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Positive(t, response.ID)
	return response.ID
}

func assertDataExchangeAudit(t *testing.T, pool *pgxpool.Pool, requestID string, orgID, userID int64, action, resource string, resourceID int64) {
	t.Helper()
	var gotOrgID, gotUserID int64
	var gotProjectID *int64
	var gotAction, gotResource, gotResourceID string
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT org_id, user_id, project_id, action, resource, resource_id
		FROM audit_logs WHERE request_id = $1`, requestID).Scan(&gotOrgID, &gotUserID, &gotProjectID, &gotAction, &gotResource, &gotResourceID))
	require.Equal(t, orgID, gotOrgID)
	require.Equal(t, userID, gotUserID)
	require.Nil(t, gotProjectID)
	require.Equal(t, action, gotAction)
	require.Equal(t, resource, gotResource)
	require.Equal(t, fmt.Sprintf("%d", resourceID), gotResourceID)
}
