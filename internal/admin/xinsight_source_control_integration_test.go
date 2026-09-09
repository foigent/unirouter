//go:build integration

package admin

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/executioncontext"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/sourcefacts"
	"github.com/uniroute/unirouter/internal/testutil"
)

const syntheticSourceControlSecret = "synthetic-source-control-local-only-secret"
const sourceReadPath = "/api/v1/integrations/xinsight/source-facts"

type sourceControlFixture struct {
	xInsightFeedAuthorizationFixture
	handler *Handler
	source  *sourcefacts.Service
	reader  http.Handler
}

func newSourceControlFixture(t *testing.T) sourceControlFixture {
	t.Helper()
	f := sourceControlFixture{xInsightFeedAuthorizationFixture: newXInsightFeedAuthorizationFixture(t, false)}
	var err error
	f.source, err = sourcefacts.New(f.pool, sourcefacts.Options{Enabled: true, SourceInstance: "synthetic-source-control", CursorSecret: syntheticSourceControlSecret})
	require.NoError(t, err)
	require.NoError(t, f.source.CheckReady(t.Context()))
	f.handler = NewHandler(f.pool, syntheticSourceControlSecret, []byte("0123456789abcdef01234567"), nil)
	f.handler.SetSourceFactsController(f.source)
	resolver, err := middleware.NewClientIPResolver(nil)
	require.NoError(t, err)
	f.reader = f.source.Handler(resolver)
	return f
}

func (f sourceControlFixture) registration(t *testing.T, name string, profiles []string) (sourcefacts.Registration, string) {
	t.Helper()
	old, token := testXInsightFeedRegistration(t, "source-only-"+name)
	return sourcefacts.Registration{SchemaVersion: sourcefacts.RegistrationVersion, TokenFormat: old.TokenFormat, VerifierScheme: old.VerifierScheme, KeyPrefix: old.KeyPrefix, VerifierSHA256: old.VerifierSHA256, OperationID: old.OperationID, SourceInstance: "synthetic-source-control", OrgID: stringInt64(f.orgID), ProjectID: stringInt64(f.projectID), Profiles: profiles}, token
}

func (f sourceControlFixture) request(t *testing.T, action string, body any) *http.Request {
	t.Helper()
	wire, err := json.Marshal(body)
	require.NoError(t, err)
	r := apiKeyIntegrationRequest(t, f.pool, "POST", "/api/v1/data-exchange/xinsight/source-connections/"+stringInt64(f.projectID)+"/"+action, string(wire), f.actorID, f.orgID, auth.RolePlatformAdmin, "source-control-"+action)
	r.SetPathValue("project_id", stringInt64(f.projectID))
	return r
}

func (f sourceControlFixture) control(t *testing.T, action string, body any, status int) map[string]any {
	t.Helper()
	r := f.request(t, action, body)
	w := httptest.NewRecorder()
	f.handler.SourceControl(action)(w, r)
	require.Equal(t, status, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.NotContains(t, w.Body.String(), "verifier_sha256")
	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	return result
}

func (f sourceControlFixture) read(t *testing.T, token, target, ip string, status int) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("GET", target, nil)
	r.Header.Set("Authorization", "Bearer "+token)
	r.RemoteAddr = ip
	w := httptest.NewRecorder()
	f.reader.ServeHTTP(w, r)
	require.Equal(t, status, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.NotContains(t, w.Body.String(), token)
	return w
}

func (f sourceControlFixture) capture(t *testing.T, request string) {
	t.Helper()
	require.NoError(t, f.source.CaptureAuthenticated(t.Context(), executioncontext.Snapshot{SchemaVersion: executioncontext.Version,
		Core:     executioncontext.Core{RequestID: request, OrgID: stringInt64(f.orgID), ProjectID: stringInt64(f.projectID), APIKeyID: "9007199254740993", KeyType: "personal", AuthKind: "api_key", AuthEpoch: "0", Operation: "chat.completions", Protocol: "openai.v1", CapturedAt: executioncontext.Time(time.Now())},
		Identity: executioncontext.Identity{OwnerUserID: "9007199254740994", ActualHuman: "unknown", AuthorityRead: "live_key"}}))
}

type sourceWirePage struct {
	Events     []json.RawMessage `json:"events"`
	NextCursor string            `json:"next_cursor"`
	HasMore    bool              `json:"has_more"`
}

func sourcePage(t *testing.T, w *httptest.ResponseRecorder) sourceWirePage {
	t.Helper()
	var page sourceWirePage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	return page
}

func TestSourceFactsCredentialLifecycleAndAuthenticatedPagesIntegration(t *testing.T) {
	f := newSourceControlFixture(t)
	ctx := t.Context()
	empty := map[string]any{}
	profiles := []string{sourcefacts.ContextProfile}
	reg, token := f.registration(t, "initial", profiles)
	prepared := map[string]any{"allowed_ip": "127.0.0.1/32", "registration": reg}
	f.capture(t, "before-bootstrap")
	first := f.control(t, "prepare", prepared, 200)
	keyID := first["credential_id"].(string)
	require.Equal(t, true, f.control(t, "prepare", prepared, 200)["replayed"])
	f.control(t, "prepare", map[string]any{"allowed_ip": "127.0.0.2/32", "registration": reg}, 409)
	f.read(t, token, sourceReadPath+"/capabilities", "127.0.0.1:3000", 200)
	f.read(t, token, sourceReadPath+"?profile="+sourcefacts.ContextProfile, "127.0.0.1:3000", 503)
	f.control(t, "activate", map[string]any{"credential_id": "9223372036854775807"}, 409)
	f.control(t, "activate", map[string]any{"credential_id": keyID}, 200)
	f.control(t, "resume", empty, 200)
	target := sourceReadPath + "?profile=" + sourcefacts.ContextProfile
	boot := sourcePage(t, f.read(t, token, target, "127.0.0.1:3000", 200))
	require.Empty(t, boot.Events, "bootstrap is forward only")
	require.NotEmpty(t, boot.NextCursor)
	f.read(t, token, target, "127.0.0.2:3000", 401)
	f.read(t, token, sourceReadPath+"?profile="+sourcefacts.IdentityProfile, "127.0.0.1:3000", 403)
	f.control(t, "rotate", map[string]any{"registration": reg}, 200) // Exact replay changes no lifecycle state.
	f.capture(t, "after-bootstrap-one")
	f.capture(t, "after-bootstrap-two")
	cursorTarget := target + "&limit=1&cursor=" + url.QueryEscape(boot.NextCursor)
	page := sourcePage(t, f.read(t, token, cursorTarget, "127.0.0.1:3000", 200))
	require.Len(t, page.Events, 1)
	require.True(t, page.HasMore)
	replay := sourcePage(t, f.read(t, token, cursorTarget, "127.0.0.1:3000", 200))
	require.Equal(t, page.Events, replay.Events)
	require.NotContains(t, string(page.Events[0]), "owner_user_id")
	next := sourcePage(t, f.read(t, token, target+"&cursor="+url.QueryEscape(page.NextCursor), "127.0.0.1:3000", 200))
	require.Len(t, next.Events, 1)
	var ack int64
	require.NoError(t, f.pool.QueryRow(ctx, `SELECT acknowledged_sequence FROM execution_source_acknowledgements WHERE project_id=$1 AND profile=$2`, f.projectID, sourcefacts.ContextProfile).Scan(&ack))
	require.EqualValues(t, 2, ack, "acknowledges supplied cursor, not newly sent page")
	f.control(t, "pause", empty, 200)
	f.read(t, token, target, "127.0.0.1:3000", 503)
	rot, rotated := f.registration(t, "rotate", profiles)
	second := f.control(t, "rotate", map[string]any{"registration": rot}, 200)
	secondID := second["credential_id"].(string)
	f.control(t, "resume", empty, 409)
	f.read(t, rotated, sourceReadPath+"/capabilities", "127.0.0.1:3000", 200)
	f.control(t, "activate", map[string]any{"credential_id": keyID}, 409)
	f.control(t, "activate", map[string]any{"credential_id": secondID}, 200)
	f.control(t, "resume", empty, 200)
	f.read(t, token, target, "127.0.0.1:3000", 401)
	rotatedPage := sourcePage(t, f.read(t, rotated, cursorTarget, "127.0.0.1:3000", 200))
	require.Equal(t, page.Events, rotatedPage.Events, "rotation preserves existing cursor generation")
	f.control(t, "pause", empty, 200)
	cancelReg, cancelToken := f.registration(t, "cancel", profiles)
	staged := f.control(t, "rotate", map[string]any{"registration": cancelReg}, 200)
	f.control(t, "cancel", map[string]any{"credential_id": staged["credential_id"]}, 200)
	f.read(t, cancelToken, sourceReadPath+"/capabilities", "127.0.0.1:3000", 401)
	f.control(t, "resume", empty, 200)
	f.control(t, "profiles", map[string]any{"profiles": []string{sourcefacts.ContextProfile, sourcefacts.IdentityProfile}}, 409)
	f.control(t, "pause", empty, 200)
	f.control(t, "profiles", map[string]any{"profiles": []string{sourcefacts.ContextProfile, sourcefacts.IdentityProfile}}, 200)
	f.read(t, rotated, target, "127.0.0.1:3000", 401)
	expanded, newToken := f.registration(t, "expanded", []string{sourcefacts.ContextProfile, sourcefacts.IdentityProfile})
	newKey := f.control(t, "prepare", map[string]any{"allowed_ip": "127.0.0.1/32", "registration": expanded}, 200)
	f.control(t, "activate", map[string]any{"credential_id": newKey["credential_id"]}, 200)
	repeatedProfiles := f.control(t, "profiles", map[string]any{"profiles": expanded.Profiles}, 200)
	require.Equal(t, "2", repeatedProfiles["scope_generation"], "setting identical permissions must not invalidate the new credential")
	f.control(t, "resume", empty, 200)
	f.read(t, newToken, cursorTarget, "127.0.0.1:3000", 400)
	identity := sourcePage(t, f.read(t, newToken, sourceReadPath+"?profile="+sourcefacts.IdentityProfile, "127.0.0.1:3000", 200))
	f.capture(t, "expanded-authorized")
	private := sourcePage(t, f.read(t, newToken, sourceReadPath+"?profile="+sourcefacts.IdentityProfile+"&cursor="+url.QueryEscape(identity.NextCursor), "127.0.0.1:3000", 200))
	require.Len(t, private.Events, 1)
	require.Contains(t, string(private.Events[0]), "owner_user_id")
	deleted, err := f.source.DeleteExpired(ctx, time.Now().Add(time.Hour))
	require.NoError(t, err)
	require.Positive(t, deleted)
	f.read(t, newToken, sourceReadPath+"?profile="+sourcefacts.IdentityProfile+"&cursor="+url.QueryEscape(identity.NextCursor), "127.0.0.1:3000", 410)
	f.control(t, "revoke", empty, 200)
	f.read(t, newToken, sourceReadPath+"/capabilities", "127.0.0.1:3000", 401)
	f.control(t, "resume", empty, 409)
	var audit string
	require.NoError(t, f.pool.QueryRow(ctx, `SELECT string_agg(row_to_json(a)::text,' ') FROM audit_logs a WHERE action LIKE 'data_exchange.xinsight.source.%'`).Scan(&audit))
	for _, secret := range []string{token, rotated, cancelToken, newToken, reg.VerifierSHA256, syntheticSourceControlSecret} {
		require.NotContains(t, audit, secret)
	}
	var ordinaryWrites int
	require.NoError(t, f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM completed_execution_feed_connections)+(SELECT count(*) FROM freeze_idempotency)+(SELECT count(*) FROM usage_finalization_journal)`).Scan(&ordinaryWrites))
	require.Zero(t, ordinaryWrites)
}

func TestSourceFactsControlReauthorizesLiveActorIntegration(t *testing.T) {
	for _, kind := range []string{"disabled", "role", "epoch", "session", "project"} {
		t.Run(kind, func(t *testing.T) {
			f := newSourceControlFixture(t)
			reg, _ := f.registration(t, kind, []string{sourcefacts.ContextProfile})
			request := f.request(t, "prepare", map[string]any{"allowed_ip": "127.0.0.1/32", "registration": reg})
			// Request carries a formerly valid session; commit-time checks must see the change.
			var err error
			switch kind {
			case "disabled":
				_, err = f.pool.Exec(t.Context(), `UPDATE users SET status='disabled' WHERE id=$1`, f.actorID)
			case "role":
				_, err = f.pool.Exec(t.Context(), `UPDATE users SET role='org_viewer' WHERE id=$1`, f.actorID)
			case "epoch":
				_, err = f.pool.Exec(t.Context(), `UPDATE users SET auth_epoch=auth_epoch+1 WHERE id=$1`, f.actorID)
			case "session":
				_, err = f.pool.Exec(t.Context(), `UPDATE admin_sessions SET revoked_at=clock_timestamp() WHERE user_id=$1`, f.actorID)
			case "project":
				_, err = f.pool.Exec(t.Context(), `UPDATE projects SET status='disabled' WHERE id=$1`, f.projectID)
			}
			require.NoError(t, err)
			w := httptest.NewRecorder()
			f.handler.SourceControl("prepare")(w, request)
			require.Equal(t, 403, w.Code, w.Body.String())
			var rows int
			require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM execution_source_connections)+(SELECT count(*) FROM execution_source_credentials)+(SELECT count(*) FROM audit_logs WHERE action LIKE 'data_exchange.xinsight.source.%')`).Scan(&rows))
			require.Zero(t, rows)
		})
	}
}

func TestSourceFactsRegistrationRejectsMalformedAndScopeIntegration(t *testing.T) {
	f := newSourceControlFixture(t)
	reg, _ := f.registration(t, "invalid", []string{sourcefacts.ContextProfile})
	for _, body := range []string{`null`, `{}`, `{"registration":null}`, `{"allowed_ip":"a","allowed_ip":"b"}`, `{"bearer":"synthetic-never-accepted"}`, `{} {}`} {
		r := f.request(t, "prepare", map[string]any{})
		r.Body = http.NoBody
		r.Body = io.NopCloser(strings.NewReader(body))
		w := httptest.NewRecorder()
		f.handler.SourceControl("prepare")(w, r)
		require.Equal(t, 400, w.Code, w.Body.String())
	}
	f.control(t, "prepare", map[string]any{"allowed_ip": "127.0.0.0/24", "registration": reg}, 400)
	wire, err := json.Marshal(map[string]any{"allowed_ip": "127.0.0.1/32", "registration": reg})
	require.NoError(t, err)
	for _, field := range []string{"registration", "project_id"} {
		r := f.request(t, "prepare", map[string]any{})
		r.Body = io.NopCloser(strings.NewReader(strings.Replace(string(wire), `"`+field+`"`, `"`+strings.ToUpper(field)+`"`, 1)))
		w := httptest.NewRecorder()
		f.handler.SourceControl("prepare")(w, r)
		require.Equal(t, 400, w.Code, w.Body.String())
	}
	reg.ProjectID = "9223372036854775807"
	f.control(t, "prepare", map[string]any{"allowed_ip": "127.0.0.1/32", "registration": reg}, 400)
}

func TestSourceFactsSafetyControlsRemainAvailableWithoutCaptureIntegration(t *testing.T) {
	f := newSourceControlFixture(t)
	reg, _ := f.registration(t, "disable-capture", []string{sourcefacts.ContextProfile})
	key := f.control(t, "prepare", map[string]any{"allowed_ip": "127.0.0.1/32", "registration": reg}, 200)
	disabled, err := sourcefacts.New(f.pool, sourcefacts.Options{SourceInstance: f.source.SourceInstance()})
	require.NoError(t, err)
	f.handler.SetSourceFactsController(disabled)
	_, err = f.pool.Exec(t.Context(), `UPDATE projects SET status='disabled' WHERE id=$1`, f.projectID)
	require.NoError(t, err)
	r := f.request(t, "", map[string]any{})
	w := httptest.NewRecorder()
	f.handler.GetXInsightSourceConnection(w, r)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"capture_enabled":false`)
	f.control(t, "pause", map[string]any{}, 200)
	f.control(t, "cancel", map[string]any{"credential_id": key["credential_id"]}, 200)
	f.control(t, "resume", map[string]any{}, 503)
	f.control(t, "revoke", map[string]any{}, 200)
}

func TestSourceFactsCredentialPurposeIsolationIntegration(t *testing.T) {
	f := newSourceControlFixture(t)
	f.handler.SetCompletedFeedController(completedFeedControllerFixture{})
	reg, token := f.registration(t, "purpose", []string{sourcefacts.ContextProfile})
	key := f.control(t, "prepare", map[string]any{"allowed_ip": "127.0.0.1/32", "registration": reg}, 200)
	f.control(t, "activate", map[string]any{"credential_id": key["credential_id"]}, 200)
	f.control(t, "resume", map[string]any{}, 200)
	resolver, err := middleware.NewClientIPResolver(nil)
	require.NoError(t, err)
	for name, wrap := range map[string]func(http.Handler) http.Handler{
		"old feed": middleware.CompletedExecutionFeedAuth(f.pool, syntheticSourceControlSecret, resolver),
		"model":    middleware.GatewayAuthWithAPIKeyHashSecret(f.pool, syntheticSourceControlSecret),
		"admin":    middleware.AdminAuth(f.pool, syntheticSourceControlSecret),
	} {
		t.Run(name, func(t *testing.T) {
			called := false
			h := wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(204) }))
			r := httptest.NewRequest("GET", "/", nil)
			r.Header.Set("Authorization", "Bearer "+token)
			r.RemoteAddr = "127.0.0.1:3000"
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			require.Contains(t, []int{401, 403}, w.Code, w.Body.String())
			require.False(t, called)
		})
	}
	for _, action := range []string{"update", "disable"} {
		r := apiKeyIntegrationRequest(t, f.pool, "POST", "/api/v1/keys/"+key["credential_id"].(string), `{"status":"active"}`, f.actorID, f.orgID, auth.RolePlatformAdmin, "source-purpose-"+action)
		r.SetPathValue("id", key["credential_id"].(string))
		w := httptest.NewRecorder()
		if action == "update" {
			f.handler.UpdateAPIKey(w, r)
		} else {
			f.handler.DisableAPIKey(w, r)
		}
		require.Equal(t, 409, w.Code, w.Body.String())
	}
	oldReg, oldToken := testXInsightFeedRegistration(t, "old-purpose-only")
	body, _ := json.Marshal(map[string]any{"allowed_ip": "127.0.0.1/32", "registration": oldReg})
	r := apiKeyIntegrationRequest(t, f.pool, "POST", "/", string(body), f.actorID, f.orgID, auth.RolePlatformAdmin, "old-purpose-register")
	r.SetPathValue("project_id", stringInt64(f.projectID))
	w := httptest.NewRecorder()
	f.handler.CreateXInsightFeedCredential(w, r)
	require.Equal(t, 201, w.Code, w.Body.String())
	f.read(t, oldToken, sourceReadPath+"/capabilities", "127.0.0.1:3000", 401)
	// The database's global verifier hash uniqueness prevents cross-purpose reuse.
	reused := reg
	reused.KeyPrefix = oldReg.KeyPrefix
	reused.VerifierSHA256 = oldReg.VerifierSHA256
	reused.OperationID = oldReg.OperationID
	f.control(t, "pause", map[string]any{}, 200)
	f.control(t, "rotate", map[string]any{"registration": reused}, 409)
	var actualHash, actualPurpose string
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT key_hash,purpose FROM api_keys WHERE id=$1`, key["credential_id"]).Scan(&actualHash, &actualPurpose))
	verifier, err := auth.DecodeReceiverFeedVerifier(reg.VerifierSHA256)
	require.NoError(t, err)
	require.Equal(t, auth.HashReceiverFeedVerifier(verifier, syntheticSourceControlSecret), actualHash)
	require.Equal(t, sourcefacts.Purpose, actualPurpose)
}

func TestSourceFactsCredentialAndSignedCursorPGRestoreIntegration(t *testing.T) {
	bin := os.Getenv("SOURCE_FACTS_PG_BIN")
	if bin == "" {
		t.Skip("SOURCE_FACTS_PG_BIN must select existing matching pg_dump/pg_restore tools")
	}
	f := newSourceControlFixture(t)
	reg, token := f.registration(t, "restore", []string{sourcefacts.ContextProfile})
	key := f.control(t, "prepare", map[string]any{"allowed_ip": "127.0.0.1/32", "registration": reg}, 200)
	f.control(t, "activate", map[string]any{"credential_id": key["credential_id"]}, 200)
	f.control(t, "resume", map[string]any{}, 200)
	target := sourceReadPath + "?profile=" + sourcefacts.ContextProfile
	boot := sourcePage(t, f.read(t, token, target, "127.0.0.1:2000", 200))
	f.capture(t, "backup-source-request")
	before := sourcePage(t, f.read(t, token, target+"&cursor="+url.QueryEscape(boot.NextCursor), "127.0.0.1:2000", 200))
	require.Len(t, before.Events, 1)
	archive := filepath.Join(t.TempDir(), "synthetic-source-credentials.pgdump")
	command := exec.CommandContext(t.Context(), filepath.Join(bin, "pg_dump"), "--format=custom", "--no-owner", "--no-acl", "--file", archive, "--dbname", f.pool.Config().ConnString())
	output, err := command.CombinedOutput()
	require.NoError(t, err, "pg_dump: %s", output)
	f.capture(t, "after-backup-source-request")
	ahead := sourcePage(t, f.read(t, token, target+"&cursor="+url.QueryEscape(before.NextCursor), "127.0.0.1:2000", 200))
	require.Len(t, ahead.Events, 1)
	restored := testutil.SetupIsolatedTestDB(t)
	command = exec.CommandContext(t.Context(), filepath.Join(bin, "pg_restore"), "--exit-on-error", "--single-transaction", "--no-owner", "--no-acl", "--dbname", restored.Config().ConnString(), archive)
	output, err = command.CombinedOutput()
	require.NoError(t, err, "pg_restore: %s", output)
	resumed, err := sourcefacts.New(restored, sourcefacts.Options{Enabled: true, SourceInstance: f.source.SourceInstance(), CursorSecret: syntheticSourceControlSecret})
	require.NoError(t, err)
	require.NoError(t, resumed.CheckReady(t.Context()))
	resolver, err := middleware.NewClientIPResolver(nil)
	require.NoError(t, err)
	rf := f
	rf.pool = restored
	rf.source = resumed
	rf.reader = resumed.Handler(resolver)
	same := sourcePage(t, rf.read(t, token, target+"&cursor="+url.QueryEscape(boot.NextCursor), "127.0.0.1:2000", 200))
	require.Equal(t, before.Events, same.Events)
	require.Equal(t, before.NextCursor, same.NextCursor)
	w := rf.read(t, token, target+"&cursor="+url.QueryEscape(ahead.NextCursor), "127.0.0.1:2000", 409)
	require.Contains(t, w.Body.String(), "cursor_regressed")
	rf.read(t, token, target, "127.0.0.2:2000", 401)
	rf.read(t, token, sourceReadPath+"?profile="+sourcefacts.IdentityProfile, "127.0.0.1:2000", 403)
	var count int
	require.NoError(t, restored.QueryRow(t.Context(), `SELECT count(*) FROM execution_source_credentials WHERE project_id=$1`, f.projectID).Scan(&count))
	require.Equal(t, 1, count)
	t.Log("real PG16 archive restored source credential binding and signed retained cursor; newer cursor rejected with 409, no silent bootstrap")
}

func TestSourceFactsReadCannotOvertakeCommittedRevocationIntegration(t *testing.T) {
	f := newSourceControlFixture(t)
	reg, token := f.registration(t, "revoke-read-race", []string{sourcefacts.ContextProfile})
	key := f.control(t, "prepare", map[string]any{"allowed_ip": "127.0.0.1/32", "registration": reg}, 200)
	f.control(t, "activate", map[string]any{"credential_id": key["credential_id"]}, 200)
	f.control(t, "resume", map[string]any{}, 200)
	tx, err := f.pool.Begin(t.Context())
	require.NoError(t, err)
	defer tx.Rollback(t.Context())
	_, err = tx.Exec(t.Context(), `SELECT 1 FROM execution_source_connections WHERE org_id=$1 AND project_id=$2 FOR UPDATE`, f.orgID, f.projectID)
	require.NoError(t, err)
	r := httptest.NewRequest("GET", sourceReadPath+"?profile="+sourcefacts.ContextProfile, nil)
	r.RemoteAddr = "127.0.0.1:2000"
	r.Header.Set("Authorization", "Bearer "+token)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { w := httptest.NewRecorder(); f.reader.ServeHTTP(w, r); done <- w }()
	require.Eventually(t, func() bool {
		var blocked bool
		err := f.pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'SELECT ak.id,ak.org_id%')`).Scan(&blocked)
		return err == nil && blocked
	}, 3*time.Second, 5*time.Millisecond)
	_, err = tx.Exec(t.Context(), `UPDATE execution_source_connections SET status='revoked',scope_generation=scope_generation+1 WHERE org_id=$1 AND project_id=$2`, f.orgID, f.projectID)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))
	select {
	case w := <-done:
		require.Equal(t, 503, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), "source_unavailable")
	case <-time.After(6 * time.Second):
		t.Fatal("reader did not finish after revocation commit")
	}
	f.read(t, token, sourceReadPath+"?profile="+sourcefacts.ContextProfile, "127.0.0.1:2000", 401)
	var count int
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT count(*) FROM execution_source_acknowledgements WHERE project_id=$1`, f.projectID).Scan(&count))
	require.Zero(t, count)
}
