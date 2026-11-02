//go:build integration

package admin

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/testutil"
)

// This fixture installs the real published DeepSeek1.1.0 bytes under the existing
// immutable publication owner. It is synthetic local storage, not Target evidence.
func TestPublishedRegistryMetadataIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	d := registryPublishedDefinition(t)
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	plan := []byte(`{"fixture":"catalogue metadata retention"}`)
	_, err = tx.Exec(ctx, `INSERT INTO capability_publications(publication_id,kind,legacy_through,legacy_projection_digest,expected_projection_digest,result_projection_digest,plan_digest,plan_bytes,materializer_contract,target_identity,publisher_identity)
 VALUES('registry-metadata-fixture','legacy_import','000113_glm_stream_function_tools_qualification',$1,$1,$1,$2,$3,'fixture','{}','{}')`, strings.Repeat("0", 64), fmt.Sprintf("%x", sha256.Sum256(plan)), plan)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `SELECT set_config('unirouter.capability_publication_id','registry-metadata-fixture',true)`)
	require.NoError(t, err)
	// Preserve the real predecessor chain under the same fixture publication owner.
	for _, definition := range []modelprofile.Definition{registryProfileDefinition(t, "deepseek-v41-flash.json"), d} {
		_, err = tx.Exec(ctx, `INSERT INTO model_profile_definitions(definition_digest,profile_id,profile_version,product_lineage_id,predecessor_digest,schema_version,canonical_bytes,publication_id)
 VALUES($1,$2,$3,$4,NULLIF($5,''),$6,$7,'registry-metadata-fixture')`, definition.Digest(), definition.Profile().ID, definition.Profile().Version, definition.ProductLineageID(), definition.PredecessorDigest(), modelprofile.DefinitionSchemaV1, definition.CanonicalBytes())
		require.NoError(t, err)
	}
	require.NoError(t, tx.Commit(ctx))
	org := testutil.NewTestOrg(t, pool)
	other := testutil.NewTestOrg(t, pool)
	var actor, model, connection, deployment int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO users(org_id,username,password_hash,organization_role,role,status) VALUES($1,'registry-editor','fixture','org_admin','org_admin','active') RETURNING id`, org).Scan(&actor))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO model_registry(org_id,model_name,display_name,provider_family,lifecycle,pricing_model_name,context_length,output_limit,execution_profile_id,execution_profile_version,product_lineage_id)
 VALUES($1,'deepseek-flash','Flash','deepseek','staging','deepseek-flash',1000000,384000,$2,$3,$4) RETURNING id`, org, d.Profile().ID, d.Profile().Version, d.ProductLineageID()).Scan(&model))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO provider_connections(name,provider,base_url,adapter_type,adapter_config,admin_state) VALUES('registry-fixture','deepseek','https://provider.invalid','openai_compatible','{}','enabled') RETURNING id`).Scan(&connection))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO model_deployments(customer_org_id,public_model_id,connection_id,upstream_model,max_context,default_max_output,profile_definition_digest) VALUES($1,$2,$3,'deepseek-flash',1000000,64,$4) RETURNING id`, org, model, connection, d.Digest()).Scan(&deployment))
	h := NewHandler(pool, "fixture", []byte("0123456789abcdef01234567"), nil)
	authorized := canonicalApplicationAuthorizedRequest(org, actor)
	requestID := "registry-metadata-update"
	update := func(body string) *httptest.ResponseRecorder {
		r := authorized(http.MethodPut, fmt.Sprintf("/api/v1/model-registry/%d?org_id=%d", model, org), body)
		r = r.WithContext(context.WithValue(r.Context(), middleware.CtxRequestID, requestID))
		r.SetPathValue("id", strconv.FormatInt(model, 10))
		w := httptest.NewRecorder()
		h.UpdateModelRegistry(w, r)
		return w
	}
	snapshot := func() string { return registryMetadataProtectedSnapshot(t, pool, model) }
	protected := snapshot()
	for _, body := range []string{
		`{"lifecycle":"ga","approved_catalog":true}`,
		fmt.Sprintf(`{"model_name":"deepseek-flash","display_name":"Flash","provider_family":"deepseek","lifecycle":"ga","context_length":1000000,"output_limit":384000,"capability_tags":"","data_residency":"","pricing_model_name":"deepseek-flash","approved_catalog":true,"migration_recommendation":"","execution_profile_id":%q,"execution_profile_version":%q}`, d.Profile().ID, d.Profile().Version),
	} {
		w := update(body)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Equal(t, protected, snapshot())
	}
	var lifecycle string
	var approved bool
	var audits int
	require.NoError(t, pool.QueryRow(ctx, `SELECT lifecycle,approved_catalog FROM model_registry WHERE id=$1`, model).Scan(&lifecycle, &approved))
	require.Equal(t, "ga", lifecycle)
	require.True(t, approved)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE resource='model_registry' AND resource_id=$1 AND action='model_registry.update' AND org_id=$2 AND user_id=$3 AND after_state->>'lifecycle'='ga' AND (after_state->>'approved_catalog')::boolean`, strconv.FormatInt(model, 10), org, actor).Scan(&audits))
	require.Equal(t, 2, audits)
	for _, body := range []string{
		`{"lifecycle":"retired","context_length":1000001}`,
		`{"lifecycle":"retired","execution_profile_id":"deepseek.v4-flash.api","execution_profile_version":"1.5.0"}`,
		`{"lifecycle":"retired","execution_profile_id":"unknown.api","execution_profile_version":"99.0.0"}`,
		`{"execution_profile_id":"deepseek.v4.1-flash.api"}`,
	} {
		w := update(body)
		require.Contains(t, []int{http.StatusBadRequest, http.StatusConflict}, w.Code, w.Body.String())
		require.Equal(t, protected, snapshot())
	}
	for _, query := range []string{fmt.Sprintf("public_model_id=%d", model), fmt.Sprintf("public_model_id=%d&org_id=%d", model, org)} {
		w := httptest.NewRecorder()
		h.ListModelExecutionProfiles(w, authorized(http.MethodGet, "/api/v1/model-execution-profiles?"+query, ""))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), d.Digest())
	}
	w := httptest.NewRecorder()
	h.ListModelExecutionProfiles(w, authorized(http.MethodGet, fmt.Sprintf("/api/v1/model-execution-profiles?public_model_id=%d&org_id=%d", model, other), ""))
	require.Equal(t, http.StatusForbidden, w.Code)
	otherRequest := canonicalApplicationAuthorizedRequest(other, actor)
	w = httptest.NewRecorder()
	h.ListModelExecutionProfiles(w, otherRequest(http.MethodGet, fmt.Sprintf("/api/v1/model-execution-profiles?public_model_id=%d", model), ""))
	require.Equal(t, http.StatusNotFound, w.Code)
	// The model lock serializes metadata writers. No speculative definition read
	// may run against a stale row while another writer holds the product lock.
	lock, err := pool.Begin(ctx)
	require.NoError(t, err)
	_, err = lock.Exec(ctx, `SELECT id FROM model_registry WHERE id=$1 FOR UPDATE`, model)
	require.NoError(t, err)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- update(`{"lifecycle":"ga","approved_catalog":true}`) }()
	select {
	case <-done:
		t.Fatal("writer escaped model lock")
	case <-time.After(50 * time.Millisecond):
	}
	require.NoError(t, lock.Commit(ctx))
	select {
	case w = <-done:
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	case <-time.After(5 * time.Second):
		t.Fatal("serialized writer did not finish")
	}
	require.Equal(t, protected, snapshot())
	require.NoError(t, installRejectModelGovernanceAuditTrigger(ctx, pool))
	requestID = "model-governance-reject-published-update"
	w = update(`{"lifecycle":"retired","approved_catalog":false}`)
	assertModelGovernanceAuditFailure(t, w)
	require.NoError(t, pool.QueryRow(ctx, `SELECT lifecycle,approved_catalog FROM model_registry WHERE id=$1`, model).Scan(&lifecycle, &approved))
	require.Equal(t, "ga", lifecycle)
	require.True(t, approved)
	require.Equal(t, protected, snapshot())
}

func registryMetadataProtectedSnapshot(t *testing.T, pool *pgxpool.Pool, id int64) string {
	t.Helper()
	var wire string
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT jsonb_build_object(
 'model',(SELECT to_jsonb(m)-'lifecycle'-'approved_catalog'-'updated_at' FROM model_registry m WHERE id=$1),
 'definitions',(SELECT jsonb_agg(to_jsonb(d) ORDER BY definition_digest) FROM model_profile_definitions d),
 'deployments',(SELECT jsonb_agg(to_jsonb(d) ORDER BY id) FROM model_deployments d WHERE public_model_id=$1),
 'releases',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM runtime_change_releases r WHERE public_model_id=$1),
 'slots',(SELECT jsonb_agg(to_jsonb(s) ORDER BY public_model_id) FROM runtime_change_slots s WHERE public_model_id=$1))::text`, id).Scan(&wire))
	return wire
}

// Run in the existing native data-first Publication→qualification→sealed
// Release→Gateway fixture, whose actual pins and Slot are already nonempty.
func assertPublishedRegistryMetadataWithRelease(t *testing.T, pool *pgxpool.Pool, h *Handler, authorized func(string, string, string) *http.Request, id int64) {
	t.Helper()
	before := registryMetadataProtectedSnapshot(t, pool, id)
	r := authorized(http.MethodPut, fmt.Sprintf("/api/v1/model-registry/%d", id), `{"lifecycle":"ga","approved_catalog":true}`)
	r.SetPathValue("id", strconv.FormatInt(id, 10))
	w := httptest.NewRecorder()
	h.UpdateModelRegistry(w, r)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, before, registryMetadataProtectedSnapshot(t, pool, id))
	var releaseCount, slotCount int
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM runtime_change_releases WHERE public_model_id=$1 AND profile_definition_digest IS NOT NULL`, id).Scan(&releaseCount))
	require.Positive(t, releaseCount)
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM runtime_change_slots WHERE public_model_id=$1 AND stable_release_id IS NOT NULL`, id).Scan(&slotCount))
	require.Positive(t, slotCount)
}
