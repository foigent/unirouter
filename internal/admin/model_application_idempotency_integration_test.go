//go:build integration

package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/testutil"
)

type canonicalCreateRoutingInvalidator struct{ calls int }

func (*canonicalCreateRoutingInvalidator) Preview(context.Context, string) (routingpolicy.Preview, error) {
	return routingpolicy.Preview{}, nil
}

func (invalidator *canonicalCreateRoutingInvalidator) Invalidate() { invalidator.calls++ }

func TestCanonicalModelApplicationCreateHasPersistentActorScopedIdempotencyIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, actorID, otherActorID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('canonical-idempotency-customer','active','customer') RETURNING id`).Scan(&orgID))
	for index, target := range []*int64{&actorID, &otherActorID} {
		require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
VALUES ($1,$2,'fixture','org_admin','org_admin','active') RETURNING id`,
			orgID, fmt.Sprintf("canonical-idempotency-admin-%d", index)).Scan(target))
	}

	handler := NewHandlerWithAPIKeyHashSecret(
		pool, "canonical-idempotency-jwt", "canonical-idempotency-hmac",
		[]byte("0123456789abcdef01234567"), nil,
	)
	invalidator := &canonicalCreateRoutingInvalidator{}
	handler.SetRoutingController(invalidator)
	authorized := canonicalApplicationAuthorizedRequest(orgID, actorID)
	request := canonicalIdempotencyCreateRequest("persistent-idempotency", "provider-secret-one")
	body, err := json.Marshal(request)
	require.NoError(t, err)
	const key = "model-deployment-idempotency-key"

	first := httptest.NewRecorder()
	serveCanonicalApplicationCreateForTest(
		t, handler, first,
		authorized(http.MethodPost, "/api/v1/model-deployments", string(body)), key,
	)
	require.Equal(t, http.StatusCreated, first.Code, first.Body.String())
	var created canonicalModelApplicationCreateResult
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &created))
	require.False(t, created.IdempotencyReplay)
	require.Empty(t, first.Header().Get(middleware.IdempotencyReplayHeader))
	require.Equal(t, 1, invalidator.calls)

	// JSON member ordering is not mutation identity. The normalized struct and
	// canonical adapter_config serialization produce the same request HMAC.
	reordered := fmt.Sprintf(`{
  "operation":{"operation_id":%q},
  "credential":{"api_key":%q},
  "deployment":{"upstream_model":%q},
  "public_model":{"lifecycle":"ga","display_name":%q,"model_name":%q,"provider_family":"openai"},
  "connection":{"adapter_config":{},"adapter_type":"openai_compatible","base_url":%q,"provider":"openai","name":%q}
}`,
		modeloperation.IDChatCompletions, "provider-secret-one", request.Deployment.UpstreamModel,
		request.PublicModel.DisplayName, request.PublicModel.ModelName,
		request.Connection.BaseURL, request.Connection.Name,
	)
	replay := httptest.NewRecorder()
	serveCanonicalApplicationCreateForTest(
		t, handler, replay,
		authorized(http.MethodPost, "/api/v1/model-deployments", reordered), key,
	)
	require.Equal(t, http.StatusOK, replay.Code, replay.Body.String())
	var replayed canonicalModelApplicationCreateResult
	require.NoError(t, json.Unmarshal(replay.Body.Bytes(), &replayed))
	require.True(t, replayed.IdempotencyReplay)
	require.Equal(t, created.Application.Deployment.ID, replayed.Application.Deployment.ID)
	require.Equal(t, created.Reused, replayed.Reused)
	require.Equal(t, "true", replay.Header().Get(middleware.IdempotencyReplayHeader))
	require.Equal(t, first.Header().Get(middleware.OperationIDHeader), replay.Header().Get(middleware.OriginalOperationHeader))
	require.Equal(t, 1, invalidator.calls, "an exact replay must not invalidate routing snapshots")

	conflictingRequest := request
	conflictingRequest.Credential.APIKey = "provider-secret-two"
	conflictingBody, err := json.Marshal(conflictingRequest)
	require.NoError(t, err)
	conflict := httptest.NewRecorder()
	serveCanonicalApplicationCreateForTest(
		t, handler, conflict,
		authorized(http.MethodPost, "/api/v1/model-deployments", string(conflictingBody)), key,
	)
	require.Equal(t, http.StatusConflict, conflict.Code, conflict.Body.String())
	require.Contains(t, conflict.Body.String(), "canonical_application_idempotency_conflict")
	require.NotContains(t, conflict.Body.String(), "provider-secret")

	missing := httptest.NewRecorder()
	middleware.OperationIdentityWithSecret(handler.apiKeyHashSecret)(
		http.HandlerFunc(handler.CreateCanonicalModelApplication),
	).ServeHTTP(missing, authorized(http.MethodPost, "/api/v1/model-deployments", string(body)))
	require.Equal(t, http.StatusBadRequest, missing.Code, missing.Body.String())
	require.Contains(t, missing.Body.String(), "canonical_application_idempotency_required")

	var receiptCount, auditCount, bindingCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM model_deployment_create_receipts`).Scan(&receiptCount))
	require.Equal(t, 1, receiptCount)
	_, err = pool.Exec(ctx, `
UPDATE model_deployment_create_receipts
   SET reused_operation=NOT reused_operation
 WHERE customer_org_id=$1 AND actor_user_id=$2`, orgID, actorID)
	require.ErrorContains(t, err, "receipt is terminal", "the original replay result must be immutable")
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs WHERE action='model_deployment.create'`).Scan(&auditCount))
	require.Equal(t, 1, auditCount)
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM provider_credential_bindings`).Scan(&bindingCount))
	require.Equal(t, 1, bindingCount)
	var receiptWire string
	require.NoError(t, pool.QueryRow(ctx, `
SELECT row_to_json(receipt)::text FROM model_deployment_create_receipts receipt`).Scan(&receiptWire))
	require.NotContains(t, receiptWire, key)
	require.NotContains(t, receiptWire, "provider-secret-one")
	require.NotContains(t, receiptWire, string(body))

	// The same raw key is independently scoped to the authenticated actor. Use
	// explicit IDs so this second authorized mutation reuses, rather than
	// duplicating, the canonical resources.
	actorScoped := canonicalModelApplicationCreateRequest{
		Connection:  canonicalConnectionInput{ID: created.Application.Connection.ID},
		PublicModel: canonicalPublicModelInput{ID: created.Application.PublicModel.ID},
		Deployment: canonicalDeploymentInput{
			UpstreamModel: created.Application.Deployment.UpstreamModel,
		},
		Credential: canonicalCredentialInput{BindingID: created.Application.Credential.Bindings[0].ID},
		Operation:  canonicalOperationInput{OperationID: string(modeloperation.IDChatCompletions)},
	}
	actorScopedBody, err := json.Marshal(actorScoped)
	require.NoError(t, err)
	otherActor := httptest.NewRecorder()
	serveCanonicalApplicationCreateForTest(
		t, handler, otherActor,
		canonicalApplicationAuthorizedRequest(orgID, otherActorID)(
			http.MethodPost, "/api/v1/model-deployments", string(actorScopedBody),
		), key,
	)
	require.Equal(t, http.StatusCreated, otherActor.Code, otherActor.Body.String())
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM model_deployment_create_receipts`).Scan(&receiptCount))
	require.Equal(t, 2, receiptCount)
}

func TestCanonicalModelApplicationConcurrentCreateCommitsExactlyOneMutationIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, actorID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('canonical-idempotency-concurrent','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
VALUES ($1,'canonical-concurrent-admin','fixture','org_admin','org_admin','active') RETURNING id`, orgID).Scan(&actorID))

	secondPool, err := pgxpool.New(ctx, pool.Config().ConnString())
	require.NoError(t, err)
	require.NoError(t, secondPool.Ping(ctx))
	defer secondPool.Close()
	secret := "canonical-concurrent-hmac"
	encryptionKey := []byte("0123456789abcdef01234567")
	handlers := []*Handler{
		NewHandlerWithAPIKeyHashSecret(pool, "canonical-concurrent-jwt", secret, encryptionKey, nil),
		NewHandlerWithAPIKeyHashSecret(secondPool, "canonical-concurrent-jwt", secret, encryptionKey, nil),
	}
	request := canonicalIdempotencyCreateRequest("concurrent-idempotency", "concurrent-provider-secret")
	body, err := json.Marshal(request)
	require.NoError(t, err)

	start := make(chan struct{})
	responses := make(chan *httptest.ResponseRecorder, 2)
	for _, handler := range handlers {
		handler := handler
		go func() {
			<-start
			req := canonicalApplicationAuthorizedRequest(orgID, actorID)(
				http.MethodPost, "/api/v1/model-deployments", string(body),
			)
			req.Header.Set(middleware.IdempotencyKeyHeader, "same-concurrent-create-key")
			response := httptest.NewRecorder()
			middleware.OperationIdentityWithSecret(secret)(
				http.HandlerFunc(handler.CreateCanonicalModelApplication),
			).ServeHTTP(response, req)
			responses <- response
		}()
	}
	close(start)
	first, second := <-responses, <-responses
	statuses := map[int]int{first.Code: 1}
	statuses[second.Code]++
	require.Equal(t, 1, statuses[http.StatusCreated], "first=%s second=%s", first.Body.String(), second.Body.String())
	require.Equal(t, 1, statuses[http.StatusOK], "first=%s second=%s", first.Body.String(), second.Body.String())

	var firstResult, secondResult canonicalModelApplicationCreateResult
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &firstResult))
	require.NoError(t, json.Unmarshal(second.Body.Bytes(), &secondResult))
	require.Equal(t, firstResult.Application.Deployment.ID, secondResult.Application.Deployment.ID)
	require.Equal(t, firstResult.Reused, secondResult.Reused)
	for table, want := range map[string]int{
		"provider_connections": 1, "model_registry": 1, "model_deployments": 1,
		"provider_credential_bindings": 1, "model_deployment_operations": 1,
		"model_deployment_create_receipts": 1,
	} {
		var count int
		require.NoError(t, pool.QueryRow(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count), table)
		require.Equal(t, want, count, table)
	}
	var audits int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs WHERE action='model_deployment.create'`).Scan(&audits))
	require.Equal(t, 1, audits)
}

func TestCanonicalModelApplicationReadbackFailureRollsBackReceiptAndDomainIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, actorID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('canonical-idempotency-readback','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
VALUES ($1,'canonical-readback-admin','fixture','org_admin','org_admin','active') RETURNING id`, orgID).Scan(&actorID))
	handler := NewHandlerWithAPIKeyHashSecret(
		pool, "canonical-readback-jwt", "canonical-readback-hmac",
		[]byte("0123456789abcdef01234567"), nil,
	)
	request := canonicalIdempotencyCreateRequest("readback-rollback", "readback-provider-secret")

	var identified *http.Request
	raw := canonicalApplicationAuthorizedRequest(orgID, actorID)(http.MethodPost, "/api/v1/model-deployments", "")
	raw.Header.Set(middleware.IdempotencyKeyHeader, "readback-rollback-key")
	middleware.OperationIdentityWithSecret(handler.apiKeyHashSecret)(http.HandlerFunc(
		func(_ http.ResponseWriter, request *http.Request) { identified = request },
	)).ServeHTTP(httptest.NewRecorder(), raw)
	require.NotNil(t, identified)
	idempotency, normalized, err := handler.canonicalCreateIdempotency(identified.Context(), orgID, request)
	require.NoError(t, err)
	injected := errors.New("injected idempotent readback failure")
	_, err = handler.createCanonicalModelApplicationWithReadbackAndIdempotency(
		identified.Context(), orgID, normalized, requestAuditMeta(identified, orgID, 0),
		func(context.Context, canonicalApplicationReadStore, int64, int64, string, time.Time) (canonicalModelApplicationProjection, error) {
			return canonicalModelApplicationProjection{}, injected
		},
		&idempotency,
	)
	require.ErrorIs(t, err, injected)
	for _, table := range []string{
		"model_deployment_create_receipts", "provider_connections", "model_registry",
		"model_deployments", "provider_credential_bindings", "model_deployment_operations", "audit_logs",
	} {
		var count int
		require.NoError(t, pool.QueryRow(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count), table)
		require.Zero(t, count, table)
	}
}

func TestCanonicalModelApplicationCommitAmbiguityReconstructsFromReceiptIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, actorID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('canonical-idempotency-commit','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
VALUES ($1,'canonical-commit-admin','fixture','org_admin','org_admin','active') RETURNING id`, orgID).Scan(&actorID))
	database := &canonicalAmbiguousCommitDB{Pool: pool, inject: true}
	handler := NewHandlerWithAPIKeyHashSecret(
		database, "canonical-commit-jwt", "canonical-commit-hmac",
		[]byte("0123456789abcdef01234567"), nil,
	)
	invalidator := &canonicalCreateRoutingInvalidator{}
	handler.SetRoutingController(invalidator)
	request := canonicalIdempotencyCreateRequest("commit-recovery", "commit-provider-secret")
	body, err := json.Marshal(request)
	require.NoError(t, err)
	response := httptest.NewRecorder()
	serveCanonicalApplicationCreateForTest(
		t, handler, response,
		canonicalApplicationAuthorizedRequest(orgID, actorID)(
			http.MethodPost, "/api/v1/model-deployments", string(body),
		), "commit-ambiguity-key",
	)
	require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
	require.Empty(t, response.Header().Get(middleware.IdempotencyReplayHeader))
	var result canonicalModelApplicationCreateResult
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	require.False(t, result.IdempotencyReplay)
	require.Positive(t, result.Application.Deployment.ID)
	require.Equal(t, 1, invalidator.calls, "a receipt-confirmed first commit must invalidate routing snapshots exactly once")

	database.mu.Lock()
	database.inject = true
	database.mu.Unlock()
	replay := httptest.NewRecorder()
	serveCanonicalApplicationCreateForTest(
		t, handler, replay,
		canonicalApplicationAuthorizedRequest(orgID, actorID)(
			http.MethodPost, "/api/v1/model-deployments", string(body),
		), "commit-ambiguity-key",
	)
	require.Equal(t, http.StatusOK, replay.Code, replay.Body.String())
	require.Equal(t, "true", replay.Header().Get(middleware.IdempotencyReplayHeader))
	require.Equal(t, response.Header().Get(middleware.OperationIDHeader), replay.Header().Get(middleware.OriginalOperationHeader))
	require.Equal(t, 1, invalidator.calls, "a receipt-confirmed replay commit must not invalidate routing snapshots")
	var replayResult canonicalModelApplicationCreateResult
	require.NoError(t, json.Unmarshal(replay.Body.Bytes(), &replayResult))
	require.True(t, replayResult.IdempotencyReplay)
	require.Equal(t, result.Application.Deployment.ID, replayResult.Application.Deployment.ID)
	require.Equal(t, result.Reused, replayResult.Reused)
	for table, want := range map[string]int{
		"model_deployment_create_receipts": 1, "model_deployments": 1, "audit_logs": 1,
	} {
		var count int
		require.NoError(t, pool.QueryRow(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count), table)
		require.Equal(t, want, count, table)
	}
}

func canonicalIdempotencyCreateRequest(suffix, apiKey string) canonicalModelApplicationCreateRequest {
	return canonicalModelApplicationCreateRequest{
		Connection: canonicalConnectionInput{
			Name: "Canonical " + suffix, Provider: "openai",
			BaseURL: "https://" + suffix + ".example/v1", AdapterType: "openai_compatible",
			AdapterConfig: json.RawMessage(`{}`),
		},
		PublicModel: canonicalPublicModelInput{
			ModelName: "canonical-" + suffix, DisplayName: "Canonical " + suffix,
			ProviderFamily: "openai", Lifecycle: "ga",
		},
		Deployment: canonicalDeploymentInput{UpstreamModel: "upstream-" + suffix},
		Credential: canonicalCredentialInput{APIKey: apiKey},
		Operation:  canonicalOperationInput{OperationID: string(modeloperation.IDChatCompletions)},
	}
}

type canonicalAmbiguousCommitDB struct {
	Pool   *pgxpool.Pool
	mu     sync.Mutex
	inject bool
}

func (db *canonicalAmbiguousCommitDB) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	return db.Pool.Query(ctx, sql, args...)
}

func (db *canonicalAmbiguousCommitDB) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	return db.Pool.QueryRow(ctx, sql, args...)
}

func (db *canonicalAmbiguousCommitDB) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	return db.Pool.Exec(ctx, sql, args...)
}

func (db *canonicalAmbiguousCommitDB) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	db.mu.Lock()
	inject := db.inject
	db.inject = false
	db.mu.Unlock()
	if inject {
		return &canonicalAmbiguousCommitTx{Tx: tx}, nil
	}
	return tx, nil
}

type canonicalAmbiguousCommitTx struct{ pgx.Tx }

func (tx *canonicalAmbiguousCommitTx) Commit(ctx context.Context) error {
	if err := tx.Tx.Commit(ctx); err != nil {
		return err
	}
	return errors.New("injected ambiguous commit result")
}
