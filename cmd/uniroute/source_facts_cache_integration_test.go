//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/admin"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/completedfeed"
	"github.com/uniroute/unirouter/internal/gateway"
	"github.com/uniroute/unirouter/internal/invocationopt"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/responsecache"
	"github.com/uniroute/unirouter/internal/sourcefacts"
)

// This branch uses the existing optimization lifecycle, actual Shadow requests,
// and the supported legacy Chat route. Only the server-generated operation IDs
// are deterministic test inputs, to select two members of the real 10% Canary
// assignment without probabilistic retries. Client headers cannot choose them.
func sourceHTTPCacheCase(t *testing.T, pool *pgxpool.Pool, relay *gateway.Relay, handler http.Handler, journal *outbox.Service, org, owner, project, key int64, raw string, calls *atomic.Int64) {
	t.Helper()
	operationPrefix, err := middleware.NewOperationID()
	require.NoError(t, err)
	manager, err := invocationopt.NewManager(pool)
	require.NoError(t, err)
	ring, err := responsecache.NewStaticKeyring("synthetic", map[string][]byte{"synthetic": bytes.Repeat([]byte{0x51}, 32)})
	require.NoError(t, err)
	cache, err := responsecache.NewService(responsecache.NewMemoryBackend(time.Now), bytes.Repeat([]byte{0x61}, 32), ring, responsecache.DefaultConfig())
	require.NoError(t, err)
	relay.SetInvocationOptimization(manager, cache, "1.3.0", "synthetic-cache-test", "test")
	a := admin.NewHandler(pool, prd2E2EJWTSecret, prd2E2EEncryptionKey, nil)
	a.SetInvocationOptimization(manager, true)
	mux := http.NewServeMux()
	guard := func(next http.HandlerFunc) http.Handler {
		return middleware.AdminAuth(pool, prd2E2EJWTSecret)(middleware.RequireRoles(auth.RoleOrgAdmin)(next))
	}
	mux.Handle("POST /policies", guard(a.CreateInvocationOptimizationPolicy))
	mux.Handle("POST /policies/{ref}/validate", guard(a.ValidateInvocationOptimizationPolicy))
	mux.Handle("POST /policies/{ref}/shadow", guard(a.ShadowInvocationOptimizationPolicy))
	mux.Handle("POST /policies/{ref}/canary", guard(a.CanaryInvocationOptimizationPolicy))
	token := mustJWT(t, pool, owner, org, auth.RoleOrgAdmin)
	control := func(path, body string, expected int) {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		require.Equal(t, expected, w.Code, w.Body.String())
	}
	control("/policies", fmt.Sprintf(`{"org_id":%d,"project_id":%d,"policy_ref":"opt_source_cache","display_name":"Synthetic source cache","response_cache_target":"canary","response_cache_canary_basis_points":1000,"response_cache_ttl_seconds":300,"response_cache_persistence_allowed":true,"response_cache_require_request_opt_in":true,"minimum_shadow_samples":10}`, org, project), http.StatusCreated)
	control("/policies/opt_source_cache/validate", `{"expected_revision":1,"reason":"synthetic integration"}`, http.StatusOK)
	control("/policies/opt_source_cache/shadow", `{"expected_revision":2,"reason":"synthetic integration"}`, http.StatusOK)
	control("/policies/opt_source_cache/canary", `{"expected_revision":3,"reason":"requires actual Shadow evidence"}`, http.StatusConflict)
	invoke := func(operation string, optIn bool) *httptest.ResponseRecorder {
		// This is a cache semantic test, not a load test. Stay below the
		// unchanged built-in 10 RPS admission limit for the synthetic key.
		time.Sleep(200 * time.Millisecond)
		body := `{"model":"company-chat","messages":[{"role":"user","content":"source-private-prompt"}],"stream":false,"temperature":0}`
		r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
		r = r.WithContext(context.WithValue(r.Context(), middleware.CtxRequestID, operation))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+raw)
		if optIn {
			r.Header.Set("X-UniRouter-Response-Cache", "allow")
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		_, err := journal.DrainOnce(t.Context())
		require.NoError(t, err)
		return w
	}
	for n := range 10 {
		w := invoke(fmt.Sprintf("%s_shadow_%02d", operationPrefix, n), true)
		require.NotEqual(t, "HIT", w.Header().Get("X-UniRouter-Response-Cache"))
	}
	require.EqualValues(t, 10, calls.Load())
	control("/policies/opt_source_cache/canary", `{"expected_revision":3,"reason":"ten actual successful Shadow requests"}`, http.StatusOK)
	policy, err := manager.Get(t.Context(), org, "opt_source_cache")
	require.NoError(t, err)
	selected := []string{}
	for n := 0; n < 10000 && len(selected) < 3; n++ {
		id := fmt.Sprintf("%s_canary_%04d", operationPrefix, n)
		if invocationopt.Assigned(policy, invocationopt.DriverResponseCache, key, id) {
			selected = append(selected, id)
		}
	}
	require.Len(t, selected, 3)
	miss := invoke(selected[0], true)
	require.NotEqual(t, "HIT", miss.Header().Get("X-UniRouter-Response-Cache"))
	require.EqualValues(t, 11, calls.Load())
	var original []byte
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT snapshot FROM completed_execution_feed_items WHERE request_id=$1`, selected[0]).Scan(&original))
	hit := invoke(selected[1], true)
	require.Equal(t, "HIT", hit.Header().Get("X-UniRouter-Response-Cache"), hit.Body.String())
	require.Contains(t, hit.Body.String(), `"response_cache_hit":true`)
	require.Contains(t, hit.Body.String(), `"attempt_count":0`)
	require.EqualValues(t, 11, calls.Load())
	var freezes, attempts, terminals, facts int
	var snapshot []byte
	var hash string
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM freeze_idempotency WHERE request_id=$1),(SELECT count(*) FROM billing_provider_attempts WHERE request_id=$1),(SELECT count(*) FROM completed_execution_feed_items WHERE request_id=$1),(SELECT count(*) FROM execution_source_facts WHERE request_id=$1)`, selected[1]).Scan(&freezes, &attempts, &terminals, &facts))
	require.Equal(t, 1, freezes, "the existing cache path freezes before cache lookup")
	require.Zero(t, attempts)
	require.Equal(t, 1, terminals, "the existing cache delivery has its own zero terminal")
	require.Equal(t, 7, facts)
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT snapshot,snapshot_sha256 FROM completed_execution_feed_items WHERE request_id=$1`, selected[1]).Scan(&snapshot, &hash))
	var economic completedfeed.Snapshot
	require.NoError(t, json.Unmarshal(snapshot, &economic))
	require.Zero(t, economic.Usage.PromptTokens)
	require.Zero(t, economic.Usage.CompletionTokens)
	require.Equal(t, "0", economic.Money.CalculatedCostMicros)
	require.Equal(t, "0", economic.Money.ChargedCostMicros)
	var costWire []byte
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT document FROM execution_source_facts WHERE request_id=$1 AND profile=$2`, selected[1], sourcefacts.CostProfile).Scan(&costWire))
	cost, err := sourcefacts.Decode(costWire)
	require.NoError(t, err)
	require.Equal(t, hash, cost.Cost.Reference.SnapshotSHA256)
	require.NotContains(t, string(costWire), "source-private-prompt")
	var reread []byte
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT snapshot FROM completed_execution_feed_items WHERE request_id=$1`, selected[0]).Scan(&reread))
	require.Equal(t, original, reread, "cache delivery cannot alter the originating economic snapshot")
	noOptIn := invoke(selected[2], false)
	require.NotEqual(t, "HIT", noOptIn.Header().Get("X-UniRouter-Response-Cache"))
	require.EqualValues(t, 12, calls.Load())
}
