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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/completedfeed"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
	"github.com/uniroute/unirouter/internal/xinsightconnector"
)

type descriptorFixture struct {
	pool              *pgxpool.Pool
	handler           *Handler
	service           *completedfeed.Service
	key, org, project int64
	credential        string
	resume            xinsightconnector.ControlOperationV2
	resumeResult      xinsightconnector.ControlResultV2
}

func newDescriptorFixture(t *testing.T, bridge bool) descriptorFixture {
	t.Helper()
	var f descriptorFixture
	if bridge {
		f.pool = testutil.SetupMigratedThroughTestDB(t, schemaauthority.XInsightPolicyExpandHead)
	} else {
		f.pool = testutil.SetupMigratedTestDB(t)
	}
	ctx := t.Context()
	journal, executor, counterpart, project, _ := setupCP2BActivation(t, f.pool)
	f.handler, f.project = executor.handler, project
	agent, e := xinsightconnector.NewTransactionalAgent(cp2bInstallationID, counterpart, journal, executor)
	require.NoError(t, e)
	prepare := cp2bPrepareOperation()
	counterpart.operation = canonicalCP2BOperation(t, prepare)
	_, e = agent.RunOnce(ctx)
	require.NoError(t, e)
	prepared := decodeCP2BResult(t, counterpart.acks[0])
	sha, e := prepared.CanonicalSHA256()
	require.NoError(t, e)
	f.credential = auth.ReceiverFeedTokenPrefix + strings.Repeat("ab", 32)
	register := cp2bRegisterOperation(prepare, sha, cp2bReceiverRegistration(t, f.credential))
	counterpart.operation = canonicalCP2BOperation(t, register)
	_, e = agent.RunOnce(ctx)
	require.NoError(t, e)
	registered := decodeCP2BResult(t, counterpart.acks[1])
	sha, e = registered.CanonicalSHA256()
	require.NoError(t, e)
	f.resume = cp2bResumeOperation(t, register, registered, sha)
	counterpart.operation = canonicalCP2BOperation(t, f.resume)
	_, e = agent.RunOnce(ctx)
	require.NoError(t, e)
	f.resumeResult = decodeCP2BResult(t, counterpart.acks[2])
	require.NoError(t, f.pool.QueryRow(ctx, `SELECT id,org_id FROM api_keys WHERE project_id=$1 AND purpose='xinsight.feed.read' AND status='feed_active'`, project).Scan(&f.key, &f.org))
	f.service, e = completedfeed.NewService(f.pool, "cp2b-api-key-hmac", cp2bSource, true)
	require.NoError(t, e)
	f.handler.SetCompletedFeedController(f.service)
	return f
}

func (f descriptorFixture) call(t *testing.T, path, body, ip string, resolver XInsightReceiverBindingResolver) *httptest.ResponseRecorder {
	t.Helper()
	if ip == "" {
		ip = "198.51.100.8"
	}
	r := httptest.NewRequest(http.MethodGet, path, strings.NewReader(body))
	r.RemoteAddr = ip + ":12345"
	r.Header.Set("Authorization", "Bearer "+f.credential)
	ipResolver, e := middleware.NewClientIPResolver(nil)
	require.NoError(t, e)
	gate := middleware.CompletedExecutionFeedAuth(f.pool, "cp2b-api-key-hmac", ipResolver)
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/integrations/xinsight/connector-installations/{installation_id}/source-binding", gate(f.handler.XInsightSourceDescriptorHandler(resolver)))
	mux.Handle("GET /api/v1/integrations/xinsight/completed-executions", gate(f.service.Handler()))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func descriptorDatabaseSnapshot(t *testing.T, pool *pgxpool.Pool) map[string]string {
	t.Helper()
	ctx := context.Background()
	rows, e := pool.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename`)
	require.NoError(t, e)
	var names []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		names = append(names, name)
	}
	require.NoError(t, rows.Err())
	rows.Close()
	result := map[string]string{}
	for _, name := range names {
		var digest string
		// Only table digests leave PostgreSQL; credentials/ciphertexts are never
		// included in an assertion message, test log or tool output.
		query := fmt.Sprintf(`SELECT md5(COALESCE(jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text),'[]'::jsonb)::text) FROM %s r`, pgx.Identifier{"public", name}.Sanitize())
		require.NoError(t, pool.QueryRow(ctx, query).Scan(&digest))
		result[name] = digest
	}
	return result
}

const descriptorPath = "/api/v1/integrations/xinsight/connector-installations/" + cp2bInstallationID + "/source-binding"

func TestSourceDescriptorOriginalV2ReadOnlyIntegration(t *testing.T) {
	for _, bridge := range []bool{false, true} {
		t.Run(fmt.Sprintf("M123_bridge_%v", bridge), func(t *testing.T) {
			f := newDescriptorFixture(t, bridge)
			feed := f.call(t, "/api/v1/integrations/xinsight/completed-executions", "", "", cp2bBindingResolver{})
			require.Equal(t, 200, feed.Code, feed.Body.String())
			var page completedfeed.FeedPage
			require.NoError(t, json.Unmarshal(feed.Body.Bytes(), &page))
			require.Empty(t, page.Items)
			before := descriptorDatabaseSnapshot(t, f.pool)
			first := f.call(t, descriptorPath, "", "", cp2bBindingResolver{})
			require.Equal(t, 200, first.Code, first.Body.String())
			d, e := xinsightconnector.DecodeSourceDescriptor(first.Body.Bytes())
			require.NoError(t, e)
			require.Equal(t, cp2bInstallationID, d.SourceBinding.InstallationID)
			require.Equal(t, f.org, d.SourceBinding.OrgID)
			require.Equal(t, f.project, d.SourceBinding.ProjectID)
			require.Equal(t, cp2bAllowedIP, d.ReceiverNetwork.AllowedIP)
			require.Equal(t, stringInt64(f.key), d.ReceiverNetwork.FeedKeyID)
			require.Equal(t, xinsightconnector.PublicCanonicalSHA256(f.resumeResult), d.ResumeResultSHA256)
			require.Equal(t, "no-store", first.Header().Get("Cache-Control"))
			require.NotContains(t, first.Body.String(), f.credential)
			require.NotContains(t, first.Body.String(), "verifier_sha256")
			require.NotContains(t, first.Body.String(), "cursor")
			repeated := f.call(t, descriptorPath, "", "", cp2bBindingResolver{})
			require.Equal(t, first.Body.Bytes(), repeated.Body.Bytes())
			require.Equal(t, before, descriptorDatabaseSnapshot(t, f.pool), "GET must preserve every public table, including auth timestamps, counters, journal and audits")
		})
	}
}

func TestSourceDescriptorOriginalV2NegativeReadOnlyIntegration(t *testing.T) {
	cases := []struct {
		name, sql, path, body, ip string
		status                    int
		code                      string
	}{
		{"invalid path identity", "", strings.Replace(descriptorPath, cp2bInstallationID, "xci_bad", 1), "", "", 400, "invalid_request"},
		{"query", "", descriptorPath + "?org_id=1", "", "", 400, "invalid_request"},
		{"body", "", descriptorPath, "{}", "", 400, "invalid_request"},
		{"foreign installation", "", strings.Replace(descriptorPath, cp2bInstallationID, "xci_BBBBBBBBBBBBBBBBBBBBBB", 1), "", "", 404, "not_found"},
		{"paused", `UPDATE completed_execution_feed_connections SET status='paused'`, descriptorPath, "", "", 409, "not_ready"},
		{"staged", `UPDATE api_keys SET status='feed_staged' WHERE purpose='xinsight.feed.read'`, descriptorPath, "", "", 409, "not_ready"},
		{"revoked", `UPDATE api_keys SET status='feed_revoked' WHERE purpose='xinsight.feed.read'`, descriptorPath, "", "", 401, ""},
		{"purpose", `UPDATE api_keys SET purpose='xinsight.source.read',source_lifecycle_scope='legacy' WHERE purpose='xinsight.feed.read'`, descriptorPath, "", "", 403, ""},
		{"network caller", "", descriptorPath, "", "198.51.100.9", 403, ""},
		{"network binding drift", `UPDATE api_keys SET allowed_ips='198.51.100.9/32' WHERE purpose='xinsight.feed.read'`, descriptorPath, "", "198.51.100.9", 409, "identity_conflict"},
		{"original registration drift", `UPDATE api_keys SET registration_operation_id=repeat('f',32) WHERE purpose='xinsight.feed.read'`, descriptorPath, "", "", 409, "identity_conflict"},
		{"stale fence", `UPDATE xinsight_connector_installations SET control_fence=control_fence+1`, descriptorPath, "", "", 409, "identity_conflict"},
		{"disconnected", `UPDATE xinsight_connector_installations SET observed_state='disconnected'`, descriptorPath, "", "", 409, "not_ready"},
		{"corrupt register hash", `UPDATE xinsight_connector_phase_operations SET request_sha256='sha256:'||repeat('0',64) WHERE operation_id='xco_CCCCCCCCCCCCCCCCCCCCCC'`, descriptorPath, "", "", 503, "unavailable"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newDescriptorFixture(t, false)
			if c.sql != "" {
				_, e := f.pool.Exec(t.Context(), c.sql)
				require.NoError(t, e)
			}
			before := descriptorDatabaseSnapshot(t, f.pool)
			w := f.call(t, c.path, c.body, c.ip, cp2bBindingResolver{})
			require.Equal(t, c.status, w.Code, w.Body.String())
			if c.code != "" {
				require.JSONEq(t, `{"error":"connector_source_descriptor.`+c.code+`"}`, w.Body.String())
			}
			require.NotContains(t, w.Body.String(), f.credential)
			require.Equal(t, before, descriptorDatabaseSnapshot(t, f.pool))
		})
	}
	t.Run("missing trusted mapping", func(t *testing.T) {
		f := newDescriptorFixture(t, false)
		before := descriptorDatabaseSnapshot(t, f.pool)
		w := f.call(t, descriptorPath, "", "", &adoptionResolver{})
		require.Equal(t, 409, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), "not_ready")
		require.Equal(t, before, descriptorDatabaseSnapshot(t, f.pool))
	})
	for _, foreign := range []bool{true, false} {
		t.Run(fmt.Sprintf("replacement_key_foreign_scope_%v", foreign), func(t *testing.T) {
			f := newDescriptorFixture(t, false)
			org, project := f.org, f.project
			if foreign {
				require.NoError(t, f.pool.QueryRow(t.Context(), `INSERT INTO orgs(name,status,org_type) VALUES('descriptor-foreign','active','standard') RETURNING id`).Scan(&org))
				require.NoError(t, f.pool.QueryRow(t.Context(), `INSERT INTO projects(org_id,name,slug,status,budget_period) VALUES($1,'Descriptor foreign','descriptor-foreign','active','none') RETURNING id`, org).Scan(&project))
			} else {
				_, e := f.pool.Exec(t.Context(), `UPDATE api_keys SET status='feed_revoked' WHERE id=$1`, f.key)
				require.NoError(t, e)
			}
			f.credential = auth.ReceiverFeedTokenPrefix + strings.Repeat("cd", 32)
			verifierHex, e := auth.ReceiverFeedVerifierHex(f.credential)
			require.NoError(t, e)
			verifier, e := auth.DecodeReceiverFeedVerifier(verifierHex)
			require.NoError(t, e)
			_, e = f.pool.Exec(t.Context(), `INSERT INTO api_keys(org_id,project_id,key_type,name,key_hash,key_prefix,status,purpose,allowed_ips,allowed_models,verifier_scheme,registration_operation_id)
				VALUES($1,$2,'service','descriptor-other', $3,$4,'feed_active','xinsight.feed.read',$5,'','receiver_sha256_v1',repeat('e',32))`, org, project, auth.HashReceiverFeedVerifier(verifier, "cp2b-api-key-hmac"), f.credential[:auth.ReceiverFeedCredentialPrefixLength], cp2bAllowedIP)
			require.NoError(t, e)
			before := descriptorDatabaseSnapshot(t, f.pool)
			w := f.call(t, descriptorPath, "", "", cp2bBindingResolver{})
			if foreign {
				require.Equal(t, 404, w.Code, w.Body.String())
				require.Contains(t, w.Body.String(), "not_found")
			} else {
				require.Equal(t, 409, w.Code, w.Body.String())
				require.Contains(t, w.Body.String(), "identity_conflict")
			}
			require.Equal(t, before, descriptorDatabaseSnapshot(t, f.pool))
		})
	}
	t.Run("wrong accepted register predecessor", func(t *testing.T) {
		f := newDescriptorFixture(t, false)
		changed := f.resume
		changed.PredecessorResultSHA256 = "sha256:" + strings.Repeat("0", 64)
		wire, e := changed.CanonicalJSON()
		require.NoError(t, e)
		_, e = f.pool.Exec(t.Context(), `UPDATE xinsight_connector_phase_operations SET request_canonical=$2,request_sha256=$3 WHERE operation_id=$1`, changed.OperationID, wire, xinsightconnector.PublicCanonicalSHA256(changed))
		require.NoError(t, e)
		before := descriptorDatabaseSnapshot(t, f.pool)
		w := f.call(t, descriptorPath, "", "", cp2bBindingResolver{})
		require.Equal(t, 409, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), "identity_conflict")
		require.Equal(t, before, descriptorDatabaseSnapshot(t, f.pool))
	})
	t.Run("adoption is not new install", func(t *testing.T) {
		f := newAdoptionFixture(t)
		f.handler.SetCompletedFeedController(f.service)
		fixture := descriptorFixture{pool: f.pool, handler: f.handler, service: f.service, credential: f.rawFeed}
		// This fixture uses its own key-hash secret; directly preserve the real
		// auth context already bound by the tested original feed middleware.
		resolver, e := middleware.NewClientIPResolver(nil)
		require.NoError(t, e)
		gate := middleware.CompletedExecutionFeedAuth(f.pool, syntheticSourceControlSecret, resolver)
		r := httptest.NewRequest("GET", descriptorPath, nil)
		r.RemoteAddr = "127.0.0.1:12345"
		r.Header.Set("Authorization", "Bearer "+fixture.credential)
		r.SetPathValue("installation_id", cp2bInstallationID)
		before := descriptorDatabaseSnapshot(t, f.pool)
		w := httptest.NewRecorder()
		gate(f.handler.XInsightSourceDescriptorHandler(cp2bBindingResolver{})).ServeHTTP(w, r)
		require.Equal(t, 409, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), "not_ready")
		require.Equal(t, before, descriptorDatabaseSnapshot(t, f.pool))
	})
}
