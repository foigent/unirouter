//go:build integration

package sourcereceiver

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/admin"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/sourcefacts"
	"github.com/uniroute/unirouter/internal/testutil"
)

// All receiver credentials below originate through the production admin routes
// and live synthetic administrator sessions. The optional evidence directory
// receives only HTTP source pages and synthetic I-01 snapshots, never tokens.
func Observe(t *testing.T, pool *pgxpool.Pool, source *sourcefacts.Service, secret, jwtSecret string, org, project int64, register func(*http.ServeMux, *admin.Handler, func(http.HandlerFunc) http.Handler)) func() {
	t.Helper()
	var actor int64
	require.NoError(t, pool.QueryRow(t.Context(), `INSERT INTO users(org_id,username,password_hash,role,status) VALUES($1,'source-synthetic-admin','synthetic-hash','platform_admin','active') RETURNING id`, org).Scan(&actor))
	jwt := testutil.MustAdminSessionJWT(t, t.Context(), pool, actor, org, auth.RolePlatformAdmin, jwtSecret)
	h := admin.NewHandlerWithAPIKeyHashSecret(pool, jwtSecret, secret, []byte("0123456789abcdef01234567"), nil)
	h.SetSourceFactsController(source)
	guard := func(next http.HandlerFunc) http.Handler {
		return middleware.AdminAuth(pool, jwtSecret)(middleware.RequireRoles(auth.RolePlatformAdmin)(next))
	}
	mux := http.NewServeMux()
	if register != nil {
		register(mux, h, guard)
	} else {
		for _, action := range []string{"prepare", "activate", "resume"} {
			mux.Handle("POST /api/v1/data-exchange/xinsight/source-connections/{project_id}/"+action, guard(h.SourceControl(action)))
		}
	}
	resolver, err := middleware.NewClientIPResolver(nil)
	require.NoError(t, err)
	reader := source.Handler(resolver)
	random := make([]byte, 32)
	_, err = rand.Read(random)
	require.NoError(t, err)
	token := "ur-xf1-" + hex.EncodeToString(random)
	verifier, err := auth.ReceiverFeedVerifierHex(token)
	require.NoError(t, err)
	operation := sha256.Sum256([]byte("source registration:" + token))
	registration := sourcefacts.Registration{SchemaVersion: sourcefacts.RegistrationVersion, TokenFormat: auth.ReceiverFeedTokenFormat, VerifierScheme: auth.ReceiverFeedVerifierScheme, KeyPrefix: token[:auth.ReceiverFeedCredentialPrefixLength], VerifierSHA256: verifier, OperationID: hex.EncodeToString(operation[:16]), SourceInstance: source.SourceInstance(), OrgID: jsonNumber(org), ProjectID: jsonNumber(project), Profiles: []string{sourcefacts.CostProfile, sourcefacts.ContextProfile, sourcefacts.IdentityProfile}}
	control := func(action string, body any) map[string]any {
		wire, e := json.Marshal(body)
		require.NoError(t, e)
		r := httptest.NewRequest("POST", "/api/v1/data-exchange/xinsight/source-connections/"+jsonNumber(project)+"/"+action, strings.NewReader(string(wire)))
		r.Header.Set("Authorization", "Bearer "+jwt)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		require.Equal(t, 200, w.Code, w.Body.String())
		var result map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		return result
	}
	key := control("prepare", map[string]any{"allowed_ip": "127.0.0.1/32", "registration": registration})
	control("activate", map[string]any{"credential_id": key["credential_id"]})
	control("resume", map[string]any{})
	read := func(profile, cursor string) map[string]any {
		path := "/api/v1/integrations/xinsight/source-facts?profile=" + profile + "&limit=2"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		r := httptest.NewRequest("GET", path, nil)
		r.RemoteAddr = "127.0.0.1:2000"
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		reader.ServeHTTP(w, r)
		require.Equal(t, 200, w.Code, w.Body.String())
		require.NotContains(t, w.Body.String(), token)
		var page map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
		return page
	}
	cursors := map[string]string{}
	for _, profile := range registration.Profiles {
		page := read(profile, "")
		require.Empty(t, page["events"])
		cursors[profile] = page["next_cursor"].(string)
	}
	return func() {
		if t.Failed() {
			return
		}
		started := time.Now()
		vectors := []map[string]any{}
		seen := map[string]bool{}
		for _, profile := range registration.Profiles {
			cursor := cursors[profile]
			for {
				page := read(profile, cursor)
				for _, raw := range page["events"].([]any) {
					event := raw.(map[string]any)
					id := event["event_id"].(string)
					require.False(t, seen[id])
					seen[id] = true
					payload, canonicalErr := eventconnector.CanonicalJSON(event["payload"])
					require.Nil(t, canonicalErr)
					var stored []byte
					var hash string
					require.NoError(t, pool.QueryRow(t.Context(), `SELECT document,source_hash FROM execution_source_facts WHERE event_id=$1 AND org_id=$2 AND project_id=$3 AND profile=$4`, id, org, project, profile).Scan(&stored, &hash))
					require.Equal(t, stored, payload)
					require.Equal(t, hash, event["integrity"].(map[string]any)["source_hash"])
				}
				vectors = append(vectors, map[string]any{"profile": profile, "org_id": jsonNumber(org), "project_id": jsonNumber(project), "previous_cursor": cursor, "page": page})
				// Retrying after a lost HTTP response returns identical events and cursor.
				replay := read(profile, cursor)
				require.Equal(t, page["events"], replay["events"])
				require.Equal(t, page["next_cursor"], replay["next_cursor"])
				next := page["next_cursor"].(string)
				if !page["has_more"].(bool) {
					read(profile, next)
					break
				}
				require.NotEqual(t, cursor, next)
				cursor = next
			}
		}
		var total int
		require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM execution_source_facts WHERE org_id=$1 AND project_id=$2`, org, project).Scan(&total))
		require.Equal(t, total, len(seen), "authenticated pages must deliver every published fact after the initial forward-only bootstrap")
		pullMS := float64(time.Since(started).Microseconds()) / 1000
		var publicationLag, journalLag *float64
		require.NoError(t, pool.QueryRow(t.Context(), `SELECT EXTRACT(EPOCH FROM (clock_timestamp()-max(published_at)))::float8*1000,
		 (SELECT EXTRACT(EPOCH FROM (clock_timestamp()-max(created_at)))::float8*1000 FROM usage_finalization_journal WHERE org_id=$1 AND project_id=$2)
		 FROM execution_source_facts WHERE org_id=$1 AND project_id=$2`, org, project).Scan(&publicationLag, &journalLag))
		lagMeasured := time.Now()
		measurements := map[string]any{"pull_with_replay_ack_ms": pullMS, "latest_publication_to_read_ms": publicationLag, "latest_journal_timestamp_to_read_ms": journalLag}
		t.Logf("authenticated source pull: %d facts, %d pages, %.3f ms including replay and acknowledgement", total, len(vectors), pullMS)
		if root := os.Getenv("SOURCE_FACTS_PAGE_EVIDENCE_DIR"); root != "" {
			require.True(t, filepath.IsAbs(root))
			records := []map[string]any{}
			rows, e := pool.Query(t.Context(), `SELECT snapshot,snapshot_sha256,revision FROM completed_execution_feed_items WHERE org_id=$1 AND project_id=$2 ORDER BY id`, org, project)
			require.NoError(t, e)
			for rows.Next() {
				var wire []byte
				var hash string
				var revision string
				require.NoError(t, rows.Scan(&wire, &hash, &revision))
				var snapshot map[string]any
				require.NoError(t, json.Unmarshal(wire, &snapshot))
				records = append(records, map[string]any{"source_instance": source.SourceInstance(), "snapshot_revision": revision, "snapshot_sha256": hash, "snapshot_wire": string(wire), "snapshot": snapshot})
			}
			require.NoError(t, rows.Err())
			rows.Close()
			fixture := map[string]any{"fixture_only": true, "test": t.Name(), "pages": vectors, "i01": records, "measurements": measurements}
			output, e := json.MarshalIndent(fixture, "", "  ")
			require.NoError(t, e)
			require.NoError(t, os.MkdirAll(root, 0700))
			name := strings.NewReplacer("/", "_", "\\", "_").Replace(t.Name()) + ".json"
			path := filepath.Join(root, name)
			require.NoError(t, os.WriteFile(path, output, 0600))
			if os.Getenv("SOURCE_FACTS_VERIFY_CONSUMER") == "1" {
				consumerStarted := time.Now()
				command := exec.CommandContext(t.Context(), "python3", "../../contracts/source-facts/v1/check_http_pages.py", path, "--expected-cases", "1")
				result, err := command.CombinedOutput()
				require.NoError(t, err, "independent consumer: %s", result)
				measurements["independent_consumer_verify_ms"] = float64(time.Since(consumerStarted).Microseconds()) / 1000
				elapsed := float64(time.Since(lagMeasured).Microseconds()) / 1000
				if publicationLag != nil {
					measurements["latest_publication_to_verified_consumer_ms"] = *publicationLag + elapsed
				}
				if journalLag != nil {
					measurements["latest_journal_timestamp_to_verified_consumer_ms"] = *journalLag + elapsed
				}
				t.Logf("independent consumer: %s", result)
				output, e = json.MarshalIndent(fixture, "", "  ")
				require.NoError(t, e)
				require.NoError(t, os.WriteFile(path, output, 0600))
			}
		}
	}
}

func jsonNumber(value int64) string { return strconv.FormatInt(value, 10) }
