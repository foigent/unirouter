package oidc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/uniroute/unirouter/internal/auth"
)

func TestServiceKeepsOrganizationViewerDefaultRoleDistinct(t *testing.T) {
	svc, err := NewService(Config{DefaultRole: auth.RoleOrgViewer})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if got := svc.Config().DefaultRole; got != auth.RoleOrgViewer {
		t.Fatalf("expected org_viewer default role to remain distinct, got %q", got)
	}
}

func TestClassifyOIDCRoleSeparatesOrganizationAndProjectAxes(t *testing.T) {
	tests := []struct {
		input             string
		organizationRole  string
		compatibilityRole string
		projectRole       string
		valid             bool
	}{
		{input: auth.RoleOrgAdmin, organizationRole: auth.RoleOrgAdmin, compatibilityRole: auth.RoleOrgAdmin, valid: true},
		{input: auth.RoleOrgViewer, organizationRole: auth.RoleOrgViewer, compatibilityRole: auth.RoleOrgViewer, valid: true},
		{input: auth.ProjectRoleAdmin, organizationRole: auth.RoleMember, compatibilityRole: auth.RoleProjectAdmin, projectRole: auth.ProjectRoleAdmin, valid: true},
		{input: auth.ProjectRoleUser, organizationRole: auth.RoleMember, compatibilityRole: auth.RoleDeveloper, projectRole: auth.ProjectRoleUser, valid: true},
		{input: auth.ProjectRoleViewer, organizationRole: auth.RoleMember, compatibilityRole: auth.RoleViewer, projectRole: auth.ProjectRoleViewer, valid: true},
		{input: auth.LegacyProjectRoleDeveloper, organizationRole: auth.RoleMember, compatibilityRole: auth.RoleDeveloper, projectRole: auth.ProjectRoleUser, valid: true},
		{input: auth.RoleDeveloper, organizationRole: auth.RoleMember, compatibilityRole: auth.RoleDeveloper, projectRole: auth.ProjectRoleUser, valid: true},
		{input: auth.RoleViewer, organizationRole: auth.RoleMember, compatibilityRole: auth.RoleViewer, projectRole: auth.ProjectRoleViewer, valid: true},
		{input: "project_owner", valid: false},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			organizationRole, compatibilityRole, projectRole, valid := classifyOIDCRole(test.input)
			if valid != test.valid || organizationRole != test.organizationRole || compatibilityRole != test.compatibilityRole || projectRole != test.projectRole {
				t.Fatalf("classifyOIDCRole(%q) = (%q,%q,%q,%v)", test.input, organizationRole, compatibilityRole, projectRole, valid)
			}
		})
	}
}

func TestServiceAuthCodeURL(t *testing.T) {
	provider := newOIDCTestProvider(t, "nonce-1")
	defer provider.Close()

	svc, err := NewService(Config{
		Enabled:      true,
		IssuerURL:    provider.URL,
		ClientID:     "uniroute",
		ClientSecret: "secret",
		RedirectURL:  "http://console.example/api/v1/auth/oidc/callback",
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	authURL, err := svc.AuthCodeURL(context.Background(), "state-1", "nonce-1")
	if err != nil {
		t.Fatalf("auth code url: %v", err)
	}
	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse auth url: %v", err)
	}
	if parsed.Path != "/authorize" {
		t.Fatalf("expected /authorize path, got %s", parsed.Path)
	}
	query := parsed.Query()
	if query.Get("client_id") != "uniroute" || query.Get("state") != "state-1" || query.Get("nonce") != "nonce-1" {
		t.Fatalf("unexpected auth query: %s", parsed.RawQuery)
	}
	if query.Get("scope") != "openid email profile" {
		t.Fatalf("expected normalized scopes, got %q", query.Get("scope"))
	}
}

func TestServiceExchangeVerifiesIDTokenAndMapsClaims(t *testing.T) {
	provider := newOIDCTestProvider(t, "nonce-ok")
	defer provider.Close()

	svc, err := NewService(Config{
		Enabled:            true,
		IssuerURL:          provider.URL,
		ClientID:           "uniroute",
		ClientSecret:       "secret",
		RedirectURL:        "http://console.example/api/v1/auth/oidc/callback",
		AllowedEmailDomain: "example.com",
		AutoProvision:      true,
		DefaultOrgID:       1,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	identity, err := svc.Exchange(context.Background(), "code-ok", "nonce-ok")
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if identity.Subject != "user-123" || identity.Email != "dev@example.com" || identity.Username != "dev@example.com" {
		t.Fatalf("unexpected identity: %#v", identity)
	}
	if identity.Role != "developer" || identity.OrgID != 7 {
		t.Fatalf("expected role/org from claims, got role=%s org=%d", identity.Role, identity.OrgID)
	}
}

func TestServiceRejectsMissingEmailWhenDomainAllowlistConfigured(t *testing.T) {
	provider := newOIDCTestProvider(t, "nonce-email-required", func(claims jwt.MapClaims) {
		delete(claims, "email")
		claims["preferred_username"] = "provider-user"
	})
	defer provider.Close()

	svc, err := NewService(Config{
		Enabled:            true,
		IssuerURL:          provider.URL,
		ClientID:           "uniroute",
		ClientSecret:       "secret",
		RedirectURL:        "http://console.example/api/v1/auth/oidc/callback",
		AllowedEmailDomain: "example.com",
		AutoProvision:      true,
		DefaultOrgID:       1,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	if _, err := svc.Exchange(context.Background(), "code-ok", "nonce-email-required"); err == nil {
		t.Fatal("expected configured email-domain policy to reject an identity without an email claim")
	}
}

func TestServiceRejectsEmailOutsideConfiguredDomain(t *testing.T) {
	provider := newOIDCTestProvider(t, "nonce-email-domain", func(claims jwt.MapClaims) {
		claims["email"] = "provider-user@example.net"
	})
	defer provider.Close()

	svc, err := NewService(Config{
		Enabled:            true,
		IssuerURL:          provider.URL,
		ClientID:           "uniroute",
		ClientSecret:       "secret",
		RedirectURL:        "http://console.example/api/v1/auth/oidc/callback",
		AllowedEmailDomain: "example.com",
		DefaultOrgID:       1,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if _, err := svc.Exchange(context.Background(), "code-ok", "nonce-email-domain"); err == nil {
		t.Fatal("expected configured email-domain policy to reject an out-of-domain email")
	}
}

func TestServiceAllowsMissingEmailWithoutDomainAllowlist(t *testing.T) {
	provider := newOIDCTestProvider(t, "nonce-email-optional", func(claims jwt.MapClaims) {
		delete(claims, "email")
		claims["preferred_username"] = "provider-user"
	})
	defer provider.Close()

	svc, err := NewService(Config{
		Enabled:      true,
		IssuerURL:    provider.URL,
		ClientID:     "uniroute",
		ClientSecret: "secret",
		RedirectURL:  "http://console.example/api/v1/auth/oidc/callback",
		DefaultOrgID: 1,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	identity, err := svc.Exchange(context.Background(), "code-ok", "nonce-email-optional")
	if err != nil {
		t.Fatalf("missing email must remain supported when no domain policy is configured: %v", err)
	}
	if identity.Email != "" || identity.Username != "provider-user" {
		t.Fatalf("unexpected email-optional identity: %#v", identity)
	}
}

func TestServiceCustomEmailClaimFallsBackToStandardEmail(t *testing.T) {
	provider := newOIDCTestProvider(t, "nonce-email-fallback")
	defer provider.Close()

	svc, err := NewService(Config{
		Enabled:            true,
		IssuerURL:          provider.URL,
		ClientID:           "uniroute",
		ClientSecret:       "secret",
		RedirectURL:        "http://console.example/api/v1/auth/oidc/callback",
		AllowedEmailDomain: "example.com",
		EmailClaim:         "mail",
		DefaultOrgID:       1,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	identity, err := svc.Exchange(context.Background(), "code-ok", "nonce-email-fallback")
	if err != nil {
		t.Fatalf("standard email fallback should satisfy the configured domain: %v", err)
	}
	if identity.Email != "dev@example.com" {
		t.Fatalf("expected standard email fallback, got %#v", identity)
	}
}

func TestServiceRejectsNonceMismatch(t *testing.T) {
	provider := newOIDCTestProvider(t, "nonce-a")
	defer provider.Close()

	svc, err := NewService(Config{
		Enabled:      true,
		IssuerURL:    provider.URL,
		ClientID:     "uniroute",
		ClientSecret: "secret",
		RedirectURL:  "http://console.example/api/v1/auth/oidc/callback",
		DefaultOrgID: 1,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if _, err := svc.Exchange(context.Background(), "code-ok", "nonce-b"); err == nil {
		t.Fatal("expected nonce mismatch error")
	}
}

func newOIDCTestProvider(t *testing.T, nonce string, mutateClaims ...func(jwt.MapClaims)) *httptest.Server {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	mux := http.NewServeMux()
	var server *httptest.Server
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		writeTestJSON(w, map[string]interface{}{
			"issuer":                 server.URL,
			"authorization_endpoint": server.URL + "/authorize",
			"token_endpoint":         server.URL + "/token",
			"jwks_uri":               server.URL + "/jwks",
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") != "code-ok" {
			w.WriteHeader(http.StatusBadRequest)
			writeTestJSON(w, map[string]string{"error": "invalid_grant"})
			return
		}
		token := signedOIDCTestToken(t, key, server.URL, "uniroute", nonce, mutateClaims...)
		writeTestJSON(w, map[string]string{
			"id_token":     token,
			"access_token": "access",
			"token_type":   "Bearer",
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		pub := key.Public().(*rsa.PublicKey)
		writeTestJSON(w, map[string]interface{}{
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

func signedOIDCTestToken(t *testing.T, key *rsa.PrivateKey, issuer, audience, nonce string, mutateClaims ...func(jwt.MapClaims)) string {
	t.Helper()
	claims := jwt.MapClaims{
		"iss":                issuer,
		"sub":                "user-123",
		"aud":                audience,
		"exp":                time.Now().Add(5 * time.Minute).Unix(),
		"iat":                time.Now().Unix(),
		"nonce":              nonce,
		"email":              "dev@example.com",
		"preferred_username": "dev@example.com",
		"name":               "Dev User",
		"uniroute_role":      "developer",
		"uniroute_org_id":    7,
	}
	for _, mutate := range mutateClaims {
		mutate(claims)
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = "kid-1"
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

func writeTestJSON(w http.ResponseWriter, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}
