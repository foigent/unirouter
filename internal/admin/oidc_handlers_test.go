package admin

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/licensing"
	"github.com/uniroute/unirouter/internal/oidc"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestOIDCConfigDisabled(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/config", nil)
	rec := httptest.NewRecorder()

	h.OIDCConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"enabled":false`) {
		t.Fatalf("expected disabled config, got %s", rec.Body.String())
	}
}

func TestOIDCConfigProjectsLicensedAvailability(t *testing.T) {
	h := newOIDCTestHandler(t, &testutil.MockDB{}, "https://issuer.example")

	t.Run("configured service is visible without a runtime wrapper", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/config", nil)
		rec := httptest.NewRecorder()

		h.OIDCConfig(rec, req)

		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"enabled":true`) {
			t.Fatalf("expected enabled config, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("unlicensed service is projected as unavailable", func(t *testing.T) {
		h.SetLicenseRuntime(licensing.NewRuntime(nil))
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/config", nil)
		rec := httptest.NewRecorder()

		h.OIDCConfig(rec, req)

		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"enabled":false`) {
			t.Fatalf("expected unavailable config, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestOIDCStartRedirectsWithSignedStateCookie(t *testing.T) {
	provider := newAdminOIDCTestProvider(t, "nonce-unused")
	defer provider.Close()
	h := newOIDCTestHandler(t, &testutil.MockDB{}, provider.URL)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/start?redirect=/developer", nil)
	rec := httptest.NewRecorder()

	h.OIDCStart(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d: %s", rec.Code, rec.Body.String())
	}
	location := rec.Header().Get("Location")
	if !strings.Contains(location, provider.URL+"/authorize") || !strings.Contains(location, "client_id=uniroute") {
		t.Fatalf("unexpected redirect location: %s", location)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != oidcStateCookieName || !cookies[0].HttpOnly {
		t.Fatalf("expected http-only oidc state cookie, got %#v", cookies)
	}
}

func TestOIDCCallbackAutoProvisionsUserAndReturnsJWT(t *testing.T) {
	provider := newAdminOIDCTestProvider(t, "nonce-ok")
	defer provider.Close()

	var auditArgs []interface{}
	var sessionArgs []interface{}
	var membershipInserted bool
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			switch {
			case strings.Contains(sql, "FROM users") && strings.Contains(sql, "oidc_issuer"):
				return &testutil.MockRow{ScanFn: func(dest ...any) error { return pgx.ErrNoRows }}
			case strings.Contains(sql, "FROM orgs") && strings.Contains(sql, "org_type = 'customer'"):
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 1
					*dest[1].(*int64) = 7
					*dest[2].(*string) = "Customer"
					*dest[3].(*string) = "active"
					return nil
				}}
			case strings.Contains(sql, "INSERT INTO users"):
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 42
					return nil
				}}
			default:
				return &testutil.MockRow{ScanFn: func(dest ...any) error { return pgx.ErrNoRows }}
			}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(sql, "INSERT INTO audit_logs") {
				auditArgs = args
			}
			if strings.Contains(sql, "INSERT INTO admin_sessions") {
				sessionArgs = args
			}
			return pgconn.NewCommandTag("INSERT 1"), nil
		},
	}
	db.BeginFn = func(context.Context) (pgx.Tx, error) {
		return &testutil.MockTx{
			QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				if !strings.Contains(sql, "INSERT INTO users") {
					t.Fatalf("unexpected transaction query: %s", sql)
				}
				if !strings.Contains(sql, "ON CONFLICT DO NOTHING") || strings.Contains(sql, "ON CONFLICT (org_id, username)") {
					t.Fatalf("OIDC provisioning must handle both display-name and issuer/subject races: %s", sql)
				}
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 42
					return nil
				}}
			},
			ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				if strings.Contains(sql, "INSERT INTO project_members") {
					membershipInserted = true
					return pgconn.NewCommandTag("INSERT 1"), nil
				}
				return pgconn.NewCommandTag("SELECT 1"), nil
			},
		}, nil
	}
	h := newOIDCTestHandler(t, db, provider.URL)
	stateValue, err := h.encodeOIDCState(oidcStateCookie{
		State:     "state-ok",
		Nonce:     "nonce-ok",
		Redirect:  "/developer",
		ExpiresAt: time.Now().Add(time.Minute).Unix(),
	})
	if err != nil {
		t.Fatalf("encode state: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?state=state-ok&code=code-ok&format=json", nil)
	req.AddCookie(&http.Cookie{Name: oidcStateCookieName, Value: stateValue})
	rec := httptest.NewRecorder()

	h.OIDCCallback(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Pragma") != "no-cache" ||
		rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("OIDC token response must disable caching and referrers: %#v", rec.Header())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	token, _ := resp["token"].(string)
	claims, err := auth.ParseJWT(token, "test-secret")
	if err != nil {
		t.Fatalf("parse jwt: %v", err)
	}
	if claims.UserID != 42 || claims.OrgID != 7 || claims.Role != auth.RoleDeveloper {
		t.Fatalf("unexpected jwt claims: %#v", claims)
	}
	if len(sessionArgs) != 4 || sessionArgs[0] != int64(42) {
		t.Fatalf("expected persisted OIDC admin session, got %#v", sessionArgs)
	}
	storedHash, ok := sessionArgs[1].([]byte)
	if !ok || len(storedHash) != 32 || !bytes.Equal(storedHash, auth.HashAdminSessionID(claims.SessionID)) {
		t.Fatalf("OIDC session must persist only the matching SHA-256 sid hash: %#v", sessionArgs[1])
	}
	if !membershipInserted {
		t.Fatal("expected default project membership for developer role")
	}
	if len(auditArgs) == 0 || auditArgs[2] != "login.oidc" {
		t.Fatalf("expected oidc login audit args, got %#v", auditArgs)
	}
	detail, _ := auditArgs[5].(string)
	for _, sensitive := range []string{"id_token", "access_token", provider.URL, "sub-42", "dev@example.com"} {
		if strings.Contains(detail, sensitive) {
			t.Fatalf("audit detail leaked %q: %s", sensitive, detail)
		}
	}
	for _, reference := range []string{"oidc_identity_ref", "username_ref", "email_ref", "sha256:"} {
		if !strings.Contains(detail, reference) {
			t.Fatalf("audit detail missing %q: %s", reference, detail)
		}
	}
	if gotSource, _ := auditArgs[9].(string); gotSource != "system" {
		t.Fatalf("expected system audit source, got %#v", auditArgs[9])
	}
}

func TestWriteOIDCCallbackHTMLDisablesTokenCaching(t *testing.T) {
	rec := httptest.NewRecorder()

	writeOIDCCallbackHTML(rec, "signed-token", "/developer")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Pragma") != "no-cache" ||
		rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("OIDC HTML token response must disable caching and referrers: %#v", rec.Header())
	}
}

func TestOIDCUsernameCollisionCandidatePreservesSuffixAtMaximumLength(t *testing.T) {
	base := strings.Repeat("a", 180)
	suffix := "0123456789abcdef"

	candidate := oidcUsernameWithSuffix(base, suffix)

	if candidate == base {
		t.Fatal("collision candidate must differ from the original username")
	}
	if len(candidate) > 180 {
		t.Fatalf("collision candidate exceeds username limit: %d", len(candidate))
	}
	if !strings.HasSuffix(candidate, "-"+suffix) {
		t.Fatalf("collision candidate lost deterministic suffix: %q", candidate)
	}
}

func TestEnsureDefaultProjectMembershipSkipsOrganizationViewer(t *testing.T) {
	for _, tc := range []struct {
		name        string
		role        string
		wantInserts int
	}{
		{name: "project viewer", role: auth.ProjectRoleViewer, wantInserts: 1},
		{name: "organization-only identity", role: "", wantInserts: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			membershipInserts := 0
			tx := &testutil.MockTx{
				ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					if strings.Contains(sql, "INSERT INTO project_members") {
						membershipInserts++
						if len(args) != 4 || args[2] != projectMemberViewer || args[3] != "default" {
							t.Fatalf("unexpected default membership args: %#v", args)
						}
					}
					return pgconn.NewCommandTag("INSERT 1"), nil
				},
			}

			if err := ensureDefaultProjectMembershipTx(context.Background(), tx, 7, 42, tc.role, "default"); err != nil {
				t.Fatalf("ensure membership: %v", err)
			}

			if membershipInserts != tc.wantInserts {
				t.Fatalf("role %s: expected %d membership inserts, got %d", tc.role, tc.wantInserts, membershipInserts)
			}
		})
	}
}

func TestOIDCAutoProvisionRejectsPlatformAdministratorClaimBeforeMutation(t *testing.T) {
	beginCalled := false
	db := &testutil.MockDB{
		QueryRowFn: func(context.Context, string, ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(...any) error { return pgx.ErrNoRows }}
		},
		BeginFn: func(context.Context) (pgx.Tx, error) {
			beginCalled = true
			return &testutil.MockTx{}, nil
		},
	}
	h := newOIDCProvisioningUnitHandler(t, db)

	_, err := h.upsertOIDCUser(context.Background(), &oidc.Identity{
		Issuer:           "https://issuer.example.test",
		Subject:          "new-platform-admin",
		Username:         "new-platform-admin",
		OrganizationRole: auth.RolePlatformAdmin,
		Role:             auth.RoleLegacyAdmin,
		OrgID:            7,
	})

	if !errors.Is(err, errOIDCPlatformProvisioningForbidden) {
		t.Fatalf("expected platform provisioning rejection, got %v", err)
	}
	if beginCalled {
		t.Fatal("platform administrator claim reached the user mutation transaction")
	}
}

func TestEnsureOIDCOrgRequiresUniqueActiveRootCustomer(t *testing.T) {
	tests := []struct {
		name     string
		count    int64
		orgID    int64
		status   string
		targetID int64
		wantErr  bool
	}{
		{name: "not initialized or partner only", count: 0, targetID: 1, wantErr: true},
		{name: "ambiguous customer roots", count: 2, orgID: 8, status: "active", targetID: 8, wantErr: true},
		{name: "inactive customer root", count: 1, orgID: 7, status: "disabled", targetID: 7, wantErr: true},
		{name: "claim points outside canonical customer", count: 1, orgID: 7, status: "active", targetID: 1, wantErr: true},
		{name: "unique active root customer", count: 1, orgID: 7, status: "active", targetID: 7},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := &testutil.MockDB{QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				if !strings.Contains(sql, "parent_id IS NULL") || !strings.Contains(sql, "org_type = 'customer'") {
					t.Fatalf("OIDC organization did not use canonical customer resolver: %s", sql)
				}
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = test.count
					*dest[1].(*int64) = test.orgID
					*dest[2].(*string) = "Customer"
					*dest[3].(*string) = test.status
					return nil
				}}
			}}
			h := newTestHandler(db)

			err := h.ensureOIDCOrg(context.Background(), test.targetID)
			if (err != nil) != test.wantErr {
				t.Fatalf("ensureOIDCOrg() err=%v, wantErr=%v", err, test.wantErr)
			}
		})
	}
}

func TestOIDCExistingLegacyPlatformAdministratorLoginRemainsAvailable(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			if !strings.Contains(sql, "oidc_issuer") {
				t.Fatalf("existing login unexpectedly queried provisioning scope: %s", sql)
			}
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int64) = 42
				*dest[1].(*int64) = 1
				*dest[2].(*string) = "legacy-platform-oidc"
				*dest[3].(*string) = auth.RolePlatformAdmin
				*dest[4].(*string) = auth.RoleLegacyAdmin
				*dest[5].(*string) = "active"
				*dest[6].(*int64) = 3
				return nil
			}}
		},
		BeginFn: func(context.Context) (pgx.Tx, error) {
			return &testutil.MockTx{
				QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					if !strings.Contains(sql, "FROM users WHERE id") {
						t.Fatalf("unexpected existing-user query: %s", sql)
					}
					return &testutil.MockRow{ScanFn: func(dest ...any) error {
						*dest[0].(*int64) = 42
						*dest[1].(*int64) = 1
						*dest[2].(*string) = "legacy-platform-oidc"
						*dest[3].(*string) = auth.RolePlatformAdmin
						*dest[4].(*string) = auth.RoleLegacyAdmin
						*dest[5].(*string) = "active"
						*dest[6].(*int64) = 3
						*dest[7].(*string) = "operator@example.test"
						return nil
					}}
				},
				ExecFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
					return pgconn.NewCommandTag("SELECT 1"), nil
				},
			}, nil
		},
	}
	h := newOIDCProvisioningUnitHandler(t, db)
	identity := &oidc.Identity{
		Issuer:           "https://issuer.example.test",
		Subject:          "legacy-platform",
		Email:            "operator@example.test",
		OrganizationRole: auth.RolePlatformAdmin,
		Role:             auth.RoleLegacyAdmin,
		OrgID:            1,
	}

	user, err := h.upsertOIDCUser(context.Background(), identity)
	if err != nil {
		t.Fatalf("existing legacy platform OIDC login was rejected: %v", err)
	}
	if user.ID != 42 || user.OrganizationRole != auth.RolePlatformAdmin || user.OrgID != 1 {
		t.Fatalf("unexpected existing platform identity: %#v", user)
	}
}

func newOIDCProvisioningUnitHandler(t *testing.T, db *testutil.MockDB) *Handler {
	t.Helper()
	svc, err := oidc.NewService(oidc.Config{
		AutoProvision: true,
		DefaultOrgID:  7,
		DefaultRole:   auth.RoleViewer,
	})
	if err != nil {
		t.Fatalf("new oidc service: %v", err)
	}
	h := newTestHandler(db)
	h.SetOIDCService(svc)
	return h
}

func newOIDCTestHandler(t *testing.T, db *testutil.MockDB, issuer string) *Handler {
	t.Helper()
	svc, err := oidc.NewService(oidc.Config{
		Enabled:            true,
		IssuerURL:          issuer,
		ClientID:           "uniroute",
		ClientSecret:       "secret",
		RedirectURL:        "http://console.example/api/v1/auth/oidc/callback",
		AutoProvision:      true,
		DefaultOrgID:       7,
		DefaultRole:        auth.RoleViewer,
		DefaultProjectSlug: "default",
	})
	if err != nil {
		t.Fatalf("new oidc service: %v", err)
	}
	h := newTestHandler(db)
	h.SetOIDCService(svc)
	return h
}

func newAdminOIDCTestProvider(t *testing.T, nonce string) *httptest.Server {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	mux := http.NewServeMux()
	var server *httptest.Server
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		writeAdminOIDCTestJSON(w, map[string]interface{}{
			"issuer":                 server.URL,
			"authorization_endpoint": server.URL + "/authorize",
			"token_endpoint":         server.URL + "/token",
			"jwks_uri":               server.URL + "/jwks",
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		token := signedAdminOIDCTestToken(t, key, server.URL, "uniroute", nonce)
		writeAdminOIDCTestJSON(w, map[string]string{
			"id_token":     token,
			"access_token": "access",
			"token_type":   "Bearer",
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		pub := key.Public().(*rsa.PublicKey)
		writeAdminOIDCTestJSON(w, map[string]interface{}{
			"keys": []map[string]string{{
				"kty": "RSA",
				"kid": "kid-1",
				"alg": "RS256",
				"use": "sig",
				"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
			}},
		})
	})
	server = httptest.NewServer(mux)
	return server
}

func signedAdminOIDCTestToken(t *testing.T, key *rsa.PrivateKey, issuer, audience, nonce string) string {
	t.Helper()
	claims := jwt.MapClaims{
		"iss":                issuer,
		"sub":                "sub-42",
		"aud":                audience,
		"exp":                time.Now().Add(5 * time.Minute).Unix(),
		"iat":                time.Now().Unix(),
		"nonce":              nonce,
		"email":              "dev@example.com",
		"preferred_username": "dev@example.com",
		"uniroute_role":      "developer",
		"uniroute_org_id":    7,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = "kid-1"
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

func writeAdminOIDCTestJSON(w http.ResponseWriter, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}
