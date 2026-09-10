//go:build integration && pr4browser

package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/admin"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/licensing"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/sourcefacts"
	"github.com/uniroute/unirouter/internal/testutil"
	"golang.org/x/crypto/bcrypt"
)

// Uses the built product UI, original sessions and production route guards.
// All identities, signing material and receiver credentials are local fixtures.
func TestSourceBudgetManagementBrowserE2E(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	var org, project int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs(name,status,org_type) VALUES('Source Browser Customer','active','customer') RETURNING id`).Scan(&org))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO projects(org_id,name,slug,status,budget_period) VALUES($1,'Source Browser Project','source-browser','active','monthly') RETURNING id`, org).Scan(&project))
	password := "source-browser-synthetic-password"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	require.NoError(t, err)
	for _, role := range []string{auth.RolePlatformAdmin, auth.RoleBillingAdmin, auth.RoleOrgAdmin} {
		_, err = pool.Exec(ctx, `INSERT INTO users(org_id,username,password_hash,organization_role,role,status,auth_provider) VALUES($1,$2,$3,$4,$4,'active','local')`, org, "source-browser-"+role, string(hash), role)
		require.NoError(t, err)
	}
	events, err := outbox.New(pool, outbox.DefaultOptions())
	require.NoError(t, err)
	financial := billing.NewBilling(pool, nil)
	financial.SetOutbox(events)
	source, err := sourcefacts.New(pool, sourcefacts.Options{Enabled: true, ObservationsEnabled: true, BudgetSnapshotsEnabled: true, FinancialJournalReady: financial.FinancialJournalReady, SourceInstance: "synthetic-source-browser", CursorSecret: pr4BrowserAPIKeySecret})
	require.NoError(t, err)
	require.NoError(t, source.CheckReady(ctx))
	handler := admin.NewHandlerWithAPIKeyHashSecret(pool, pr4BrowserJWTSecret, pr4BrowserAPIKeySecret, []byte(pr4BrowserEncryptionKey), nil)
	handler.SetOutbox(events)
	handler.SetSourceFactsController(source)
	handler.SetBillingReadinessProvider(financial)
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	licenseRuntime := licensing.NewRuntime(licensing.NewCommercialVerifier(map[string]ed25519.PublicKey{"source-browser": publicKey}, nil, 30, "source-browser"))
	now := time.Now().UTC()
	document, err := licensing.SignCommercialPayload(licensing.Payload{LicenseID: "source-browser", Customer: "Synthetic", DeploymentID: "source-browser", Tier: "enterprise", IssuedAt: now.Add(-time.Hour).Format(time.RFC3339), NotBefore: now.Add(-time.Hour).Format(time.RFC3339), ExpiresAt: now.Add(time.Hour).Format(time.RFC3339), Features: []string{licensing.FeatureGateway, licensing.FeatureDataExchange}, Limits: map[string]int64{licensing.LimitActiveProjects: 10, licensing.LimitActiveUsers: 10, licensing.LimitActiveAPIKeys: 10}}, "source-browser", privateKey)
	require.NoError(t, err)
	wire, err := json.Marshal(document)
	require.NoError(t, err)
	status := licenseRuntime.Replace(wire)
	require.True(t, status.Valid && status.Enforceable)
	handler.SetLicenseRuntime(licenseRuntime)
	authenticated := func(next http.HandlerFunc) http.Handler {
		return middleware.AdminAuth(pool, pr4BrowserJWTSecret)(middleware.CustomerContext(pool)(next))
	}
	roles := func(allowed ...string) func(http.HandlerFunc) http.Handler {
		return func(next http.HandlerFunc) http.Handler {
			return authenticated(func(w http.ResponseWriter, r *http.Request) {
				middleware.RequireOrganizationRoles(allowed...)(next).ServeHTTP(w, r)
			})
		}
	}
	platform := roles(auth.RolePlatformAdmin)
	expansion := func(next http.HandlerFunc) http.Handler {
		return platform(func(w http.ResponseWriter, r *http.Request) {
			licenseRuntime.Gate(licensing.FeatureDataExchange)(next).ServeHTTP(w, r)
		})
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/login", handler.Login)
	mux.HandleFunc("POST /api/v1/auth/logout", handler.Logout)
	mux.HandleFunc("GET /api/v1/auth/oidc/config", handler.OIDCConfig)
	mux.Handle("GET /api/v1/me", authenticated(handler.GetMe))
	mux.Handle("GET /api/v1/projects", authenticated(handler.ListProjects))
	mux.Handle("GET /api/v1/organizations", authenticated(handler.ListOrganizations))
	mux.Handle("GET /api/v1/dashboard/stats", authenticated(handler.DashboardStats))
	registerXInsightFeedControlRoutes(mux, handler, xInsightFeedControlRouteGuards{safety: platform, expansion: expansion})
	registerXInsightSourceControlRoutes(mux, handler, xInsightFeedControlRouteGuards{safety: platform, expansion: expansion})
	registerEnterpriseFinanceRoutes(mux, handler, enterpriseFinanceRouteGuards{read: roles(auth.RolePlatformAdmin, auth.RoleBillingAdmin, auth.RoleOrgAdmin), write: roles(auth.RolePlatformAdmin, auth.RoleBillingAdmin)})
	resolver, err := middleware.NewClientIPResolver(nil)
	require.NoError(t, err)
	mux.Handle("GET /api/v1/integrations/xinsight/source/v2/budgets", source.BudgetHandler(resolver))
	server := httptest.NewServer(pr4BrowserSPAHandler(filepath.Join(pr4BrowserRepositoryRoot(t), "frontend", "dist"), middleware.SecurityHeaders(middleware.OperationIdentityWithSecret(pr4BrowserAPIKeySecret)(mux))))
	t.Cleanup(server.Close)
	random := make([]byte, 32)
	_, err = rand.Read(random)
	require.NoError(t, err)
	token := "ur-xf1-" + hex.EncodeToString(random)
	verifier, err := auth.ReceiverFeedVerifierHex(token)
	require.NoError(t, err)
	operation := sha256.Sum256([]byte("browser registration:" + token))
	profiles := append([]string{sourcefacts.ContextProfile}, sourcefacts.BudgetProfiles()...)
	slices.Sort(profiles)
	registration := sourcefacts.Registration{SchemaVersion: sourcefacts.RegistrationVersion, TokenFormat: auth.ReceiverFeedTokenFormat, VerifierScheme: auth.ReceiverFeedVerifierScheme, KeyPrefix: token[:auth.ReceiverFeedCredentialPrefixLength], VerifierSHA256: verifier, OperationID: hex.EncodeToString(operation[:16]), SourceInstance: source.SourceInstance(), OrgID: strconv.FormatInt(org, 10), ProjectID: strconv.FormatInt(project, 10), Profiles: profiles}
	require.NoError(t, registration.Validate())
	fixture, err := json.Marshal(map[string]any{"base_url": server.URL, "password": password, "org_id": org, "project_id": project, "registration": registration, "receiver_token": token})
	require.NoError(t, err)
	command := exec.Command("node", filepath.Join(pr4BrowserRepositoryRoot(t), "frontend", "e2e", "source_budget_management.mjs"))
	command.Dir = filepath.Join(pr4BrowserRepositoryRoot(t), "frontend")
	command.Env = append(os.Environ(), "SOURCE_BUDGET_BROWSER_FIXTURE="+string(fixture))
	output, err := command.CombinedOutput()
	require.NoError(t, err, "source budget browser: %s", output)
	t.Logf("%s", output)
	var revision int64
	var bindingStatus string
	require.NoError(t, pool.QueryRow(ctx, `SELECT revision,status FROM execution_source_budget_bindings WHERE org_id=$1 AND enterprise_project_scope_ref='browser-synthetic-scope'`, org).Scan(&revision, &bindingStatus))
	require.EqualValues(t, 3, revision)
	require.Equal(t, "active", bindingStatus)
}
