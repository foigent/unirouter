//go:build integration && pr4browser

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/admin"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/gateway"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/testutil"
	"golang.org/x/crypto/bcrypt"
)

type overviewBrowserRow struct {
	ID        string    `json:"id"`
	OrgID     int64     `json:"org_id"`
	Model     string    `json:"model"`
	Requested string    `json:"requested_model"`
	Cost      string    `json:"cost_micros"`
	CreatedAt time.Time `json:"created_at"`
	Status    int       `json:"status_code"`
	Latency   int       `json:"latency_ms"`
}

type overviewBrowserFixture struct {
	BaseURL        string               `json:"base_url"`
	SelectionURL   string               `json:"selection_url"`
	EmptyURL       string               `json:"empty_url"`
	UnavailableURL string               `json:"unavailable_url"`
	AmbiguousURL   string               `json:"ambiguous_url"`
	Password       string               `json:"password"`
	OrgID          int64                `json:"org_id"`
	OtherOrgID     int64                `json:"other_org_id"`
	OrgName        string               `json:"org_name"`
	Users          map[string]string    `json:"users"`
	Rows           []overviewBrowserRow `json:"rows"`
	Wallet         map[string]string    `json:"wallet"`
	DeploymentID   int64                `json:"deployment_id"`
	OperationID    string               `json:"operation_id"`
	TrapURLs       map[string]string    `json:"trap_urls"`
	ControlURLs    map[string]string    `json:"control_urls"`
}

// Only fixture-local request failures/delays are controlled here. The business
// response always comes from the real handler; no dashboard DTO is fabricated.
type overviewBrowserFaults struct {
	failLogs      atomic.Bool
	failStats     atomic.Bool
	failAttention atomic.Bool
	mu            sync.Mutex
	gate          chan struct{}
	entered       bool
}

// A real canceled SQL read makes the production customer-context resolver
// unavailable. Other queries, including genuine login/session checks, use PG.
type overviewUnavailableCustomerDB struct{ *pgxpool.Pool }

func (q overviewUnavailableCustomerDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, "WITH roots AS") {
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		return q.Pool.QueryRow(canceled, sql, args...)
	}
	return q.Pool.QueryRow(ctx, sql, args...)
}

func (f *overviewBrowserFaults) release() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.gate != nil {
		close(f.gate)
		f.gate = nil
	}
}

func (f *overviewBrowserFaults) control(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		f.mu.Lock()
		entered := f.entered
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]bool{"entered": entered})
		return
	}
	var input struct {
		Action string `json:"action"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 256)).Decode(&input) != nil {
		http.Error(w, "invalid fixture control", http.StatusBadRequest)
		return
	}
	switch input.Action {
	case "logs-fail-next":
		f.failLogs.Store(true)
	case "stats-fail-next":
		f.failStats.Store(true)
	case "attention-fail":
		f.failAttention.Store(true)
	case "attention-recover":
		f.failAttention.Store(false)
	case "hold-next":
		f.mu.Lock()
		if f.gate != nil {
			f.mu.Unlock()
			http.Error(w, "fixture already held", http.StatusConflict)
			return
		}
		f.gate, f.entered = make(chan struct{}), false
		f.mu.Unlock()
	case "release":
		f.release()
	default:
		http.Error(w, "unknown fixture control", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (f *overviewBrowserFaults) stats(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		var gate chan struct{}
		if f.gate != nil && !f.entered {
			gate, f.entered = f.gate, true
		}
		f.mu.Unlock()
		if gate != nil {
			select {
			case <-gate:
			case <-r.Context().Done():
				return
			}
		}
		if f.failStats.Swap(false) {
			ctx, cancel := context.WithCancel(r.Context())
			cancel()
			next(w, r.WithContext(ctx))
			return
		}
		next(w, r)
	}
}

func (f *overviewBrowserFaults) attention(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if f.failAttention.Load() {
			ctx, cancel := context.WithCancel(r.Context())
			cancel()
			next(w, r.WithContext(ctx))
			return
		}
		next(w, r)
	}
}

func TestCustomerOverviewBrowserE2E(t *testing.T) {
	if os.Getenv("OVERVIEW_NAVIGATION_ONLY") == "1" {
		require.False(t, strings.EqualFold(os.Getenv("CI"), "true") || os.Getenv("CI") == "1", "navigation-only selection is a local diagnostic, never a CI acceptance substitute")
	}
	require.Equal(t, "1", os.Getenv("REQUIRE_TEST_DATABASE"), "explicit required-mode PG16 is mandatory")
	require.NotEmpty(t, strings.TrimSpace(os.Getenv("TEST_DATABASE_URL")))
	root := pr4BrowserRepositoryRoot(t)
	_ = overviewBrowserDist(t)

	provider := newPR4BrowserTrafficTrap(t, "overview-provider")
	feed := newPR4BrowserTrafficTrap(t, "overview-feed")
	upstream := newPR4BrowserTrafficTrap(t, "overview-upstream")
	controlProvider := newPR4BrowserTrafficTrap(t, "overview-control-provider")
	controlFeed := newPR4BrowserTrafficTrap(t, "overview-control-feed")
	controlUpstream := newPR4BrowserTrafficTrap(t, "overview-control-upstream")
	oldTransport := http.DefaultTransport
	http.DefaultTransport = newPR4BrowserEgressTransport(t, provider, feed, upstream)
	t.Cleanup(func() { http.DefaultTransport = oldTransport })
	provePR4BrowserBackendEgressGuard(t,
		newPR4BrowserEgressTransport(t, controlProvider, controlFeed, controlUpstream),
		controlProvider, controlFeed, controlUpstream)

	pool := testutil.SetupMigratedTestDB(t)
	f, _ := seedOverviewBrowser(t, pool, false)
	state := &overviewBrowserFaults{}
	f.BaseURL = newOverviewBrowserServer(t, pool, state)
	before := overviewBusinessSnapshot(t, pool)

	emptyPool := testutil.SetupMigratedTestDB(t)
	empty, _ := seedOverviewBrowser(t, emptyPool, true)
	emptyState := &overviewBrowserFaults{}
	f.EmptyURL = newOverviewBrowserServer(t, emptyPool, emptyState)
	emptyBefore := overviewBusinessSnapshot(t, emptyPool)
	f.UnavailableURL = newOverviewBrowserServerWithDB(t, pool, overviewUnavailableCustomerDB{pool}, &overviewBrowserFaults{})
	ambiguousPool := testutil.SetupMigratedTestDB(t)
	_, _ = seedOverviewBrowser(t, ambiguousPool, true)
	_, err := ambiguousPool.Exec(context.Background(), `INSERT INTO orgs(name,status,org_type) VALUES ('Second customer fixture','active','customer')`)
	require.NoError(t, err)
	f.AmbiguousURL = newOverviewBrowserServer(t, ambiguousPool, &overviewBrowserFaults{})
	ambiguousBefore := overviewBusinessSnapshot(t, ambiguousPool)
	selectionPool := testutil.SetupMigratedTestDB(t)
	selection, _ := seedOverviewBrowser(t, selectionPool, true)
	for _, row := range []struct {
		id, model string
		age       time.Duration
	}{
		{"selection-seven", "selection-older-model", 48 * time.Hour},
		{"selection-thirty", "selection-oldest-model", 240 * time.Hour},
	} {
		_, err := selectionPool.Exec(context.Background(), `INSERT INTO call_logs(request_id,org_id,model,requested_model,cost_micros,created_at,status_code,latency_ms) VALUES($1,$2,$3,$3,7,$4,200,10)`, row.id, selection.OrgID, row.model, time.Now().Add(-row.age))
		require.NoError(t, err)
	}
	f.SelectionURL = newOverviewBrowserServer(t, selectionPool, &overviewBrowserFaults{})
	f.TrapURLs = map[string]string{"provider": provider.server.URL, "feed": feed.server.URL, "upstream": upstream.server.URL}
	f.ControlURLs = map[string]string{"provider": controlProvider.server.URL, "feed": controlFeed.server.URL, "upstream": controlUpstream.server.URL}
	payload, err := json.Marshal(f)
	require.NoError(t, err)
	emptyPayload, err := json.Marshal(empty)
	require.NoError(t, err)

	// Reserve time for the child to close its own contexts/browser and for Go's
	// httptest/DB cleanup. No global process or existing service is stopped.
	ctx := context.Background()
	var cancel context.CancelFunc
	if deadline, ok := t.Deadline(); ok {
		ctx, cancel = context.WithDeadline(ctx, deadline.Add(-30*time.Second))
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	defer cancel()
	if os.Getenv("OVERVIEW_NAVIGATION_ONLY") != "1" {
		command := exec.CommandContext(ctx, "node", filepath.Join(root, "frontend", "e2e", "dashboard_overview.mjs"))
		command.Dir = filepath.Join(root, "frontend")
		command.Env = append(os.Environ(), "OVERVIEW_BROWSER_FIXTURE="+string(payload), "OVERVIEW_EMPTY_FIXTURE="+string(emptyPayload))
		command.Cancel = func() error { return command.Process.Signal(os.Interrupt) }
		command.WaitDelay = 10 * time.Second
		output, runErr := command.CombinedOutput()
		// Release any held handler before failing, including a failed Node assertion.
		state.release()
		emptyState.release()
		if runErr != nil {
			t.Fatalf("overview browser failed: %v\n%s", runErr, output)
		}
		t.Logf("overview browser output:\n%s", output)
		require.Contains(t, string(output), `"browser_closed":true`)
	}
	require.Equal(t, before, overviewBusinessSnapshot(t, pool), "overview reads changed business data")
	require.Equal(t, emptyBefore, overviewBusinessSnapshot(t, emptyPool), "empty overview reads changed business data")
	require.Equal(t, ambiguousBefore, overviewBusinessSnapshot(t, ambiguousPool), "ambiguous overview changed business data")
	// Navigation/forms use the same production handlers/auth on the private DB,
	// after the original read-only business snapshot is independently checked.
	navigationBefore := overviewBusinessSnapshot(t, pool, true)
	nav := exec.CommandContext(ctx, "node", filepath.Join(root, "frontend", "e2e", "dashboard_navigation.mjs"))
	nav.Dir = filepath.Join(root, "frontend")
	nav.Env = append(os.Environ(), "OVERVIEW_BROWSER_FIXTURE="+string(payload), "DEBUG=pw:browser")
	nav.Cancel = func() error { return nav.Process.Signal(os.Interrupt) }
	nav.WaitDelay = 10 * time.Second
	navOutput, navErr := nav.CombinedOutput()
	t.Logf("overview navigation output:\n%s", navOutput)
	require.NoError(t, navErr, "real navigation browser failed: %s", navOutput)
	require.Contains(t, string(navOutput), `"browser_closed":true`)
	var registeredName string
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT display_name FROM deployment_profiles WHERE profile_ref='dep_ui_nav_profile'`).Scan(&registeredName))
	require.Equal(t, "Local UI registration", registeredName)
	require.Equal(t, navigationBefore, overviewBusinessSnapshot(t, pool, true), "navigation changed execution/economic fixture facts")
	var resolved, events int
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FILTER(WHERE status='resolved'),count(*) FROM notification_events WHERE org_id=$1`, f.OrgID).Scan(&resolved, &events))
	require.Equal(t, 1, resolved)
	require.Equal(t, 3, events)
	var foreignStatus string
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT status FROM notification_events WHERE org_id=$1`, f.OtherOrgID).Scan(&foreignStatus))
	require.Equal(t, "open", foreignStatus)

	for _, trap := range []*pr4BrowserTrafficTrap{provider, feed, upstream} {
		require.Zero(t, trap.count.Load(), "regular overview must not make external traffic: %s", trap.name)
	}
	require.Equal(t, int64(2), controlProvider.count.Load())
	require.Equal(t, int64(3), controlFeed.count.Load())
	require.Equal(t, int64(2), controlUpstream.count.Load())
	invariants := map[string]any{
		"readonly_snapshot_before":                             before,
		"navigation_execution_economic_before":                 navigationBefore,
		"navigation_execution_economic_after":                  overviewBusinessSnapshot(t, pool, true),
		"readonly_snapshot_includes_notification_and_pricing":  true,
		"navigation_notification_mutation_separately_verified": true,
		"resolved_customer_events":                             resolved, "customer_events_preserved": events,
		"foreign_event_status":   foreignStatus,
		"registered_profile_ref": "dep_ui_nav_profile", "registered_profile_name": registeredName,
		"regular_provider_feed_upstream_requests": []int64{provider.count.Load(), feed.count.Load(), upstream.count.Load()},
		"causal_guard_counts":                     []int64{controlProvider.count.Load(), controlFeed.count.Load(), controlUpstream.count.Load()},
	}
	invariantBytes, err := json.MarshalIndent(invariants, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(os.Getenv("OVERVIEW_BROWSER_ARTIFACTS"), "database-invariants.json"), invariantBytes, 0o600))
	t.Log("OVERVIEW_REAL_BACKEND database=postgres16 api=production_handlers browser=real external_traffic=0 execution_economic_snapshot=unchanged navigation_registration=verified")
}

func seedOverviewBrowser(t *testing.T, pool *pgxpool.Pool, empty bool) (overviewBrowserFixture, int64) {
	t.Helper()
	ctx := context.Background()
	f := overviewBrowserFixture{Password: pr4BrowserPassword, OrgName: "Overview customer 客户", Users: map[string]string{}, Rows: []overviewBrowserRow{}, Wallet: map[string]string{}}
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs(name,status,org_type) VALUES ($1,'active','customer') RETURNING id`, f.OrgName).Scan(&f.OrgID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs(name,status,org_type) VALUES ('FOREIGN-OVERVIEW-DO-NOT-DISPLAY','active','partner') RETURNING id`).Scan(&f.OtherOrgID))
	hash, err := bcrypt.GenerateFromPassword([]byte(f.Password), bcrypt.MinCost)
	require.NoError(t, err)
	var platformID int64
	for _, role := range []string{auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleBillingAdmin, auth.RoleOrgViewer, auth.RoleMember, "foreign"} {
		orgID, organizationRole, compatibilityRole := f.OrgID, role, role
		if role == "foreign" {
			orgID, organizationRole, compatibilityRole = f.OtherOrgID, auth.RoleOrgAdmin, auth.RoleOrgAdmin
		}
		if role == auth.RoleMember {
			compatibilityRole = auth.RoleDeveloper
		}
		username := "overview-" + role
		f.Users[role] = username
		var id int64
		require.NoError(t, pool.QueryRow(ctx, `INSERT INTO users(org_id,username,password_hash,organization_role,role,status,auth_provider) VALUES ($1,$2,$3,$4,$5,'active','local') RETURNING id`, orgID, username, string(hash), organizationRole, compatibilityRole).Scan(&id))
		if role == auth.RolePlatformAdmin {
			platformID = id
		}
	}
	if empty {
		return f, platformID
	}
	// Enough synthetic people for a real second page; no extra login identities
	// are exported to the browser or used outside this disposable database.
	for i := 0; i < 25; i++ {
		_, err := pool.Exec(ctx, `INSERT INTO users(org_id,username,password_hash,organization_role,role,status,auth_provider) VALUES($1,$2,$3,'org_viewer','org_viewer','active','local')`, f.OrgID, fmt.Sprintf("ui-nav-person-%02d", i), string(hash))
		require.NoError(t, err)
	}

	_, err = pool.Exec(ctx, `INSERT INTO wallets(org_id,balance_micros,granted_balance_micros,frozen_micros,total_spent_micros,low_balance_threshold_cents) VALUES ($1,1000000,500001,250000,0,10)`, f.OrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO model_prices(model_name,input_price_cents,output_price_cents) VALUES('overview-model-00',1,2)`)
	require.NoError(t, err)
	var balance, granted, frozen, threshold string
	require.NoError(t, pool.QueryRow(ctx, `SELECT balance_micros::text,granted_balance_micros::text,frozen_micros::text,low_balance_threshold_cents::text FROM wallets WHERE org_id=$1`, f.OrgID).Scan(&balance, &granted, &frozen, &threshold))
	f.Wallet = map[string]string{"balance": balance, "granted": granted, "frozen": frozen, "threshold_cents": threshold}
	// Keep equal-window samples away from a bucket boundary while retaining
	// distinct ordering in the independently queried recent-request list.
	insertCanonical := seedOverviewCanonicalDeployment(t, pool, platformID, &f)
	now := time.Now().UTC().Truncate(time.Hour).Add(30 * time.Minute)
	seeded := []overviewBrowserRow{}
	add := func(id string, org int64, model, requested, cost string, age time.Duration, status, latency int) {
		row := overviewBrowserRow{id, org, model, requested, cost, now.Add(-age), status, latency}
		if id == "overview-current-00" || id == "overview-exact" {
			insertCanonical(row)
		} else {
			_, err := pool.Exec(ctx, `INSERT INTO call_logs(request_id,org_id,model,requested_model,cost_micros,created_at,status_code,latency_ms) VALUES ($1,$2,$3,$4,$5::bigint,$6,$7,$8)`, row.ID, row.OrgID, row.Model, row.Requested, row.Cost, row.CreatedAt, row.Status, row.Latency)
			require.NoError(t, err)
		}
		seeded = append(seeded, row)
	}
	for i := 0; i < 61; i++ {
		model := fmt.Sprintf("overview-model-%02d", i)
		if i == 60 {
			model += "-long-model-name-中文-identifier-preserved"
		}
		status := 200
		if i == 2 {
			status = 500
		}
		add(fmt.Sprintf("overview-current-%02d", i), f.OrgID, model, "original-alias", "1", time.Hour+time.Duration(i)*time.Millisecond, status, 10+i)
	}
	add("overview-exact", f.OrgID, "overview-model-00", "alias", "9007199254740993", 2*time.Hour, 200, 20)
	add("overview-fallback", f.OrgID, " ", " original-name ", "7", 3*time.Hour, 200, 30)
	add("overview-seven-day", f.OrgID, "overview-model-00", "alias", "10000", 48*time.Hour, 200, 40)
	add("overview-thirty-day", f.OrgID, "overview-model-00", "alias", "20000", 240*time.Hour, 200, 50)
	add("overview-before-window", f.OrgID, "overview-model-00", "alias", "777", 960*time.Hour, 200, 60)
	add("overview-future-recent", f.OrgID, "overview-model-00", "alias", "999", -time.Hour, 200, 70)
	add("overview-foreign", f.OtherOrgID, "FOREIGN-OVERVIEW-DO-NOT-DISPLAY", "foreign", "888888888", time.Hour, 200, 80)
	// Read raw SQL rows back and compare with the independent insertion manifest.
	rows, err := pool.Query(ctx, `SELECT request_id,org_id,model,requested_model,cost_micros::text,created_at,status_code,latency_ms FROM call_logs ORDER BY request_id`)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var row overviewBrowserRow
		require.NoError(t, rows.Scan(&row.ID, &row.OrgID, &row.Model, &row.Requested, &row.Cost, &row.CreatedAt, &row.Status, &row.Latency))
		f.Rows = append(f.Rows, row)
	}
	require.NoError(t, rows.Err())
	sort.Slice(seeded, func(i, j int) bool { return seeded[i].ID < seeded[j].ID })
	require.Len(t, f.Rows, len(seeded))
	for i := range seeded {
		require.Equal(t, seeded[i].ID, f.Rows[i].ID)
		require.Equal(t, seeded[i].OrgID, f.Rows[i].OrgID)
		require.Equal(t, seeded[i].Model, f.Rows[i].Model)
		require.Equal(t, seeded[i].Requested, f.Rows[i].Requested)
		require.Equal(t, seeded[i].Cost, f.Rows[i].Cost)
		require.True(t, seeded[i].CreatedAt.Equal(f.Rows[i].CreatedAt))
		require.Equal(t, seeded[i].Status, f.Rows[i].Status)
		require.Equal(t, seeded[i].Latency, f.Rows[i].Latency)
	}
	for _, event := range []struct {
		org                   int64
		severity, status, key string
	}{{f.OrgID, "critical", "open", "critical"}, {f.OrgID, "warning", "open", "warning"}, {f.OrgID, "critical", "acknowledged", "ack"}, {f.OtherOrgID, "critical", "open", "foreign"}} {
		_, err := pool.Exec(ctx, `INSERT INTO notification_events(org_id,event_type,severity,status,title,dedupe_key) VALUES ($1,'overview_fixture',$2,$3,'Synthetic overview event',$4)`, event.org, event.severity, event.status, event.key)
		require.NoError(t, err)
	}
	var critical, warning int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE severity='critical'),count(*) FILTER(WHERE severity='warning') FROM notification_events WHERE org_id=$1 AND status='open'`, f.OrgID).Scan(&critical, &warning))
	require.Equal(t, 1, critical)
	require.Equal(t, 1, warning)
	return f, platformID
}

func overviewRoutes(pool *pgxpool.Pool, q db.DBQuerier, h *admin.Handler, state *overviewBrowserFaults) http.Handler {
	adminAuth := middleware.AdminAuth(pool, pr4BrowserJWTSecret)
	customer := middleware.CustomerContext(q)
	authenticated := func(next http.HandlerFunc) http.Handler { return adminAuth(customer(next)) }
	roles := func(next http.HandlerFunc, allowed ...string) http.Handler {
		return adminAuth(customer(middleware.RequireOrganizationRoles(allowed...)(next)))
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/login", h.Login)
	mux.HandleFunc("POST /api/v1/auth/logout", h.Logout)
	mux.HandleFunc("GET /api/v1/auth/oidc/config", h.OIDCConfig)
	mux.Handle("GET /api/v1/me", authenticated(h.GetMe))
	mux.Handle("GET /api/v1/organizations", authenticated(h.ListOrganizations))
	mux.Handle("GET /api/v1/projects", authenticated(h.ListProjects))
	mux.Handle("GET /api/v1/dashboard/stats", roles(state.stats(h.DashboardStats), auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleBillingAdmin, auth.RoleOrgViewer))
	mux.Handle("GET /api/v1/internal-status", roles(state.attention(h.InternalStatus), auth.RolePlatformAdmin, auth.RoleOrgAdmin))
	// Fixture controls are test-only and require a genuine platform session.
	mux.Handle("GET /__overview_fixture/control", roles(state.control, auth.RolePlatformAdmin))
	mux.Handle("POST /__overview_fixture/control", roles(state.control, auth.RolePlatformAdmin))
	registerOverviewNavigationRoutes(mux, h, state, authenticated, roles)
	return middleware.SecurityHeaders(middleware.OperationIdentityWithSecret(pr4BrowserAPIKeySecret)(mux))
}

func newOverviewBrowserServer(t *testing.T, pool *pgxpool.Pool, state *overviewBrowserFaults) string {
	return newOverviewBrowserServerWithDB(t, pool, pool, state)
}

func newOverviewBrowserServerWithDB(t *testing.T, pool *pgxpool.Pool, q db.DBQuerier, state *overviewBrowserFaults) string {
	t.Helper()
	h := admin.NewHandlerWithAPIKeyHashSecret(q, pr4BrowserJWTSecret, pr4BrowserAPIKeySecret, []byte(pr4BrowserEncryptionKey), nil)
	router := gateway.NewRouter(q)
	manager, err := runtimechange.NewManager(pool, router, routingpolicy.Default())
	require.NoError(t, err)
	router.SetRuntimeResolver(manager)
	h.SetRoutingController(router)
	h.SetRuntimeChangeManager(manager)
	h.SetRuntimeStatusOptions(admin.RuntimeStatusOptions{Environment: "development", DeploymentMode: "local", DeploymentRole: "stable", Version: "1.14.2+local-ui"})
	api := overviewRoutes(pool, q, h, state)
	spa := pr4BrowserSPAHandler(overviewBrowserDist(t), api)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/__overview_fixture/") {
			api.ServeHTTP(w, r)
			return
		}
		spa.ServeHTTP(w, r)
	}))
	t.Cleanup(func() {
		server.Close()
		t.Log("OVERVIEW_FIXTURE_HTTP_SERVER_CLOSED")
	})
	t.Cleanup(state.release) // LIFO: release waiting handlers before server.Close.
	return server.URL
}

func overviewBrowserDist(t *testing.T) string {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv("OVERVIEW_BROWSER_DIST"))
	require.NotEmpty(t, raw, "explicit task-owned frontend dist is mandatory")
	require.True(t, filepath.IsAbs(raw), "frontend dist must be an absolute path")
	dist, err := filepath.EvalSymlinks(filepath.Clean(raw))
	require.NoError(t, err)
	require.Equal(t, filepath.Clean(raw), dist, "frontend dist must not redirect through symlinks")
	info, err := os.Lstat(filepath.Join(dist, "index.html"))
	require.NoError(t, err, "exact task frontend index must exist")
	require.True(t, info.Mode().IsRegular(), "frontend index must be a regular file")
	return dist
}

func seedOverviewCanonicalDeployment(t *testing.T, pool *pgxpool.Pool, userID int64, f *overviewBrowserFixture) func(overviewBrowserRow) {
	t.Helper()
	h := admin.NewHandlerWithAPIKeyHashSecret(pool, pr4BrowserJWTSecret, pr4BrowserAPIKeySecret, []byte(pr4BrowserEncryptionKey), nil)
	guard := middleware.AdminAuth(pool, pr4BrowserJWTSecret)(middleware.CustomerContext(pool)(middleware.RequireOrganizationRoles(auth.RolePlatformAdmin, auth.RoleOrgAdmin)(http.HandlerFunc(h.CreateCanonicalModelApplication))))
	guard = middleware.OperationIdentityWithSecret(pr4BrowserAPIKeySecret)(guard)
	body := map[string]any{
		"connection":   map[string]any{"name": "Overview canonical connection", "provider": "openai", "base_url": pr4BrowserProviderTarget + "/v1", "adapter_type": "openai_compatible", "adapter_config": map[string]any{}},
		"public_model": map[string]any{"model_name": "overview-model-00", "display_name": "Overview canonical model", "provider_family": "openai", "lifecycle": "ga", "approved_catalog": true},
		"deployment":   map[string]any{"upstream_model": "overview-upstream"},
		"credential":   map[string]any{"api_key": "synthetic-overview-never-used"},
		"operation":    map[string]any{"operation_id": string(modeloperation.IDChatCompletions)},
	}
	data, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/model-deployments", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "overview-fixture-create-canonical")
	req.Header.Set("Authorization", "Bearer "+testutil.MustAdminSessionJWT(t, context.Background(), pool, userID, f.OrgID, auth.RolePlatformAdmin, pr4BrowserJWTSecret))
	rec := httptest.NewRecorder()
	guard.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, "canonical fixture creation failed: %s", rec.Body.String())
	var created struct {
		Application struct {
			Deployment struct {
				ID              int64 `json:"id"`
				ConfigRevision  int64 `json:"config_revision"`
				CircuitRevision int64 `json:"circuit_revision"`
			} `json:"deployment"`
			Connection struct {
				ID             int64  `json:"id"`
				ConfigRevision int64  `json:"config_revision"`
				Provider       string `json:"provider"`
				Name           string `json:"name"`
			} `json:"connection"`
			PublicModel struct {
				ID int64 `json:"id"`
			} `json:"public_model"`
			Credential struct {
				Bindings []struct {
					ID            int64 `json:"id"`
					StateRevision int64 `json:"state_revision"`
				} `json:"bindings"`
			} `json:"credential"`
		} `json:"application"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	f.DeploymentID = created.Application.Deployment.ID
	require.Positive(t, f.DeploymentID)
	f.OperationID = string(modeloperation.IDChatCompletions)
	var orgID int64
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT customer_org_id FROM model_deployments WHERE id=$1`, f.DeploymentID).Scan(&orgID))
	require.Equal(t, f.OrgID, orgID)
	// Match the supported historical canonical projection fixture, without
	// installing a current runtime slot or loosening any schema constraint.
	require.Len(t, created.Application.Credential.Bindings, 1)
	application := created.Application
	credentialBindingID := application.Credential.Bindings[0].ID
	var credentialLinkedAt time.Time
	require.NoError(t, pool.QueryRow(context.Background(), `
SELECT created_at
  FROM credential_binding_deployments
 WHERE binding_id=$1 AND deployment_id=$2 AND connection_id=$3`,
		credentialBindingID, application.Deployment.ID, application.Connection.ID,
	).Scan(&credentialLinkedAt))

	var probeID int64
	var probeTestedAt time.Time
	require.NoError(t, pool.QueryRow(context.Background(), `
INSERT INTO model_deployment_probes (
    deployment_id,connection_id,operation_id,probe_profile_id,probe_profile_version,
    upstream_model,reported_model,connection_revision,deployment_revision,
    credential_binding_id,credential_state_revision,success,status_code,source_kind,source_ref,
    source_fingerprint,tested_by,tested_at)
VALUES ($1,$2,$3,'openai.chat_completions','1.0.0',
        'overview-upstream','overview-upstream',$4,$5,$6,$7,TRUE,200,
        'integration','overview-browser-history',$8,$9,NOW())
RETURNING probe_id,tested_at`,
		application.Deployment.ID, application.Connection.ID, string(modeloperation.IDChatCompletions),
		application.Connection.ConfigRevision, application.Deployment.ConfigRevision,
		credentialBindingID, application.Credential.Bindings[0].StateRevision,
		"sha256:"+strings.Repeat("d", 64), userID,
	).Scan(&probeID, &probeTestedAt))
	_, err = pool.Exec(context.Background(), `
UPDATE model_deployment_operations
   SET evidence_state='live_probed',evidence_ref='probe:' || $3::bigint::text,
       verified_at=$4,revision=revision+1,updated_at=NOW()
 WHERE deployment_id=$1 AND operation_id=$2`,
		application.Deployment.ID, string(modeloperation.IDChatCompletions), probeID, probeTestedAt)
	require.NoError(t, err)

	var operationRevision int64
	var operationEvidenceState, operationEvidenceRef string
	var operationVerifiedAt time.Time
	require.NoError(t, pool.QueryRow(context.Background(), `
SELECT revision,evidence_state,evidence_ref,verified_at
  FROM model_deployment_operations
 WHERE deployment_id=$1 AND operation_id=$2`,
		application.Deployment.ID, string(modeloperation.IDChatCompletions),
	).Scan(&operationRevision, &operationEvidenceState, &operationEvidenceRef, &operationVerifiedAt))

	var releaseID int64
	require.NoError(t, pool.QueryRow(context.Background(), `INSERT INTO runtime_change_releases (
 release_ref,model_name,candidate_version,candidate_hash,baseline_hash,candidate,
 provider_pack_pins,simulation_report,overall_risk,stage,version,canary_basis_points,
 reason,created_by,validated_by,activated_by,validated_at,activated_at)
 VALUES ('rcr_overview_browser_history','overview-model-00','unirouter.runtime-change-candidate.v1',$1,$2,
 '{}','[]','{}','unchanged','active',1,0,'synthetic overview history',$3,$3,$3,NOW(),NOW()) RETURNING id`,
		"sha256:"+strings.Repeat("a", 64), "sha256:"+strings.Repeat("b", 64), userID).Scan(&releaseID))
	// Create the matching immutable provider attempt before its projection.
	// These are synthetic historical fixtures; no provider request is executed.
	return func(row overviewBrowserRow) {
		canonicalRequestID := row.ID
		_, err := pool.Exec(context.Background(), `
INSERT INTO freeze_idempotency (request_id,org_id,status,pricing_version)
VALUES ($1,$2,'settled','overview-canonical-test')`, canonicalRequestID, orgID)
		require.NoError(t, err)
		_, err = pool.Exec(context.Background(), `
INSERT INTO billing_provider_attempts (
    org_id,request_id,attempt_id,api_key_id,channel_id,channel_key_id,deployment_id,
    route_sequence,route_total,endpoint,provider,channel_name,
    route_evidence_version,route_authority_mode,route_execution_contract_source,route_evidence_hash,
    route_public_model_id,route_connection_id,route_credential_binding_id,
    route_connection_revision,route_deployment_revision,route_circuit_revision,
    route_credential_state_revision,route_credential_linked_at,
    route_model_operation_id,route_model_operation_revision,
    route_protocol_profile_id,route_protocol_profile_version,
    route_operation_evidence_state,route_operation_evidence_ref,route_operation_verified_at,
    route_capability_evidence,route_capability_evidence_hash,
    route_credential_probe_id,route_credential_probe_tested_at,
    route_runtime_release_id,route_runtime_release_ref,route_runtime_stage,route_slot_version,
    route_snapshot_hash,route_requires_probe)
VALUES ($1,$2,'attempt-canonical',101,NULL,NULL,$3,1,1,'/v1/chat/completions',$4,$5,
        'unirouter.execution-route.v1','canonical','canonical_profile',$6,
        $7,$8,$9,$10,$11,$12,$13,$14,$15,$16,'openai.chat_completions','1.0.0',
        $17,$18,$19,'[]',$20,$21,$22,$23,'rcr_overview_browser_history','active',1,$24,FALSE)`,
			orgID, canonicalRequestID, application.Deployment.ID,
			application.Connection.Provider, application.Connection.Name,
			"sha256:"+strings.Repeat("1", 64), application.PublicModel.ID, application.Connection.ID,
			credentialBindingID, application.Connection.ConfigRevision, application.Deployment.ConfigRevision,
			application.Deployment.CircuitRevision, application.Credential.Bindings[0].StateRevision, credentialLinkedAt,
			string(modeloperation.IDChatCompletions), operationRevision, operationEvidenceState, operationEvidenceRef,
			operationVerifiedAt, "sha256:"+strings.Repeat("f", 64), probeID, probeTestedAt, releaseID,
			"mrs1_"+strings.Repeat("e", 64),
		)
		require.NoError(t, err)

		_, err = pool.Exec(context.Background(), `
INSERT INTO call_logs (
    request_id,org_id,attempt_id,source_event_hash,model,requested_model,provider,channel,
    cost_micros,status_code,latency_ms,created_at,
    route_evidence_source,route_evidence_version,route_authority_mode,route_evidence_hash,
    route_public_model_id,route_deployment_id,route_connection_id,route_credential_binding_id,
    route_model_operation_id,route_runtime_release_id,route_snapshot_hash)
VALUES ($1,$2,'attempt-canonical',$3,$4,$19,$5,$6,$15::bigint,$16,$17,$18,
        'provider_attempt','unirouter.execution-route.v1','canonical',$7,$8,$9,$10,$11,$12,$13,$14)`,
			canonicalRequestID, orgID, strings.Repeat("9", 64), row.Model,
			application.Connection.Provider, application.Connection.Name,
			"sha256:"+strings.Repeat("1", 64), application.PublicModel.ID, application.Deployment.ID,
			application.Connection.ID, credentialBindingID, string(modeloperation.IDChatCompletions), releaseID,
			"mrs1_"+strings.Repeat("e", 64), row.Cost, row.Status, row.Latency, row.CreatedAt, row.Requested,
		)
		require.NoError(t, err)

		var routeID int64
		var operationID string
		require.NoError(t, pool.QueryRow(context.Background(), `SELECT route_deployment_id,route_model_operation_id FROM call_logs WHERE request_id=$1`, row.ID).Scan(&routeID, &operationID))
		require.Equal(t, f.DeploymentID, routeID)
		require.Equal(t, f.OperationID, operationID)
	}
}

func overviewBusinessSnapshot(t *testing.T, pool *pgxpool.Pool, navigationWrites ...bool) string {
	t.Helper()
	// Explicit business tables only. Login's legitimate session/last-login and
	// auth audit writes are not mislabeled as dashboard business mutations.
	hash := sha256.New()
	for _, table := range []string{"orgs", "call_logs", "wallets", "model_prices", "notification_events", "model_deployments", "model_deployment_operations", "runtime_change_releases", "model_deployment_probes", "freeze_idempotency", "billing_provider_attempts"} {
		if len(navigationWrites) > 0 && navigationWrites[0] && table == "notification_events" {
			continue
		}
		var raw string
		require.NoError(t, pool.QueryRow(context.Background(), `SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text),'[]'::jsonb)::text FROM `+table+` x`).Scan(&raw))
		_, _ = fmt.Fprintf(hash, "%s\n%s\n", table, raw)
	}
	return hex.EncodeToString(hash.Sum(nil))
}
