package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestCompletedExecutionFeedAuthAcceptsOnlyExactFeedServiceKey(t *testing.T) {
	db := completedFeedAuthMockDB(t, completedFeedAuthFixture{
		projectID:    22,
		keyType:      "service",
		purpose:      APIKeyPurposeXInsightFeedRead,
		allowedIPs:   "127.0.0.1",
		orgStatus:    "active",
		keyStatus:    APIKeyStatusFeedActive,
		allowedModel: "",
	})
	handler := CompletedExecutionFeedAuth(
		db,
		"feed-auth-secret",
		mustClientIPResolver(t),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if GetOrgIDFromContext(r.Context()) != 7 ||
			GetProjectIDFromContext(r.Context()) != 22 ||
			GetKeyIDFromContext(r.Context()) != 42 {
			t.Fatalf("feed authorization did not bind exact project credential context")
		}
		w.WriteHeader(http.StatusOK)
	}))

	request := httptest.NewRequest(http.MethodGet, "/api/v1/integrations/xinsight/completed-executions", nil)
	request.Header.Set("Authorization", "Bearer ur-feed-only")
	request.RemoteAddr = "127.0.0.1:54321"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected feed service key to pass, got %d: %s", response.Code, response.Body.String())
	}
}

func TestCompletedExecutionFeedAuthAdmitsStagedCredentialToPausedGate(t *testing.T) {
	db := completedFeedAuthMockDB(t, completedFeedAuthFixture{
		projectID:    22,
		keyType:      "service",
		purpose:      APIKeyPurposeXInsightFeedRead,
		allowedIPs:   "127.0.0.1",
		orgStatus:    "active",
		keyStatus:    APIKeyStatusFeedStaged,
		allowedModel: "",
	})
	called := false
	handler := CompletedExecutionFeedAuth(
		db,
		"feed-auth-secret",
		mustClientIPResolver(t),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if key := GetAPIKeyFromContext(r.Context()); key == nil || key.Status != APIKeyStatusFeedStaged {
			t.Fatal("staged feed credential status was not preserved for the connection-state gate")
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/integrations/xinsight/completed-executions", nil)
	request.Header.Set("Authorization", "Bearer ur-feed-staged")
	request.RemoteAddr = "127.0.0.1:54321"
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if !called || response.Code != http.StatusServiceUnavailable {
		t.Fatalf("staged credential must reach only the downstream pause gate: called=%t status=%d", called, response.Code)
	}
}

func TestCompletedExecutionFeedAuthRejectsPurposeOrScopeSubstitutes(t *testing.T) {
	tests := []struct {
		name    string
		fixture completedFeedAuthFixture
	}{
		{
			name: "policy label cannot substitute for purpose",
			fixture: completedFeedAuthFixture{
				projectID: 22, keyType: "service", allowedIPs: "127.0.0.1",
			},
		},
		{
			name: "personal key",
			fixture: completedFeedAuthFixture{
				projectID: 22, keyType: "personal", purpose: APIKeyPurposeXInsightFeedRead, allowedIPs: "127.0.0.1",
			},
		},
		{
			name: "org scoped key",
			fixture: completedFeedAuthFixture{
				keyType: "service", purpose: APIKeyPurposeXInsightFeedRead, allowedIPs: "127.0.0.1",
			},
		},
		{
			name: "unbound network",
			fixture: completedFeedAuthFixture{
				projectID: 22, keyType: "service", purpose: APIKeyPurposeXInsightFeedRead,
			},
		},
		{
			name: "model allowance",
			fixture: completedFeedAuthFixture{
				projectID: 22, keyType: "service", purpose: APIKeyPurposeXInsightFeedRead,
				allowedIPs: "127.0.0.1", allowedModel: "integration-model",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.fixture.keyStatus = APIKeyStatusFeedActive
			test.fixture.orgStatus = "active"
			handler := CompletedExecutionFeedAuth(
				completedFeedAuthMockDB(t, test.fixture),
				"feed-auth-secret",
				mustClientIPResolver(t),
			)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			request := httptest.NewRequest(http.MethodGet, "/api/v1/integrations/xinsight/completed-executions", nil)
			request.Header.Set("Authorization", "Bearer ur-not-feed-authorized")
			request.RemoteAddr = "127.0.0.1:54321"
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusForbidden {
				t.Fatalf("expected exact feed scope denial, got %d: %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestFeedPurposeCredentialIsRejectedByEveryModelGatewaySurface(t *testing.T) {
	for _, path := range []string{
		"/v1/chat/completions",
		"/v1/embeddings",
		"/v1/models",
	} {
		t.Run(path, func(t *testing.T) {
			db := completedFeedAuthMockDB(t, completedFeedAuthFixture{
				projectID:    22,
				keyType:      "service",
				purpose:      APIKeyPurposeXInsightFeedRead,
				allowedIPs:   "127.0.0.1",
				orgStatus:    "active",
				keyStatus:    APIKeyStatusFeedActive,
				allowedModel: "",
			})
			handler := GatewayAuthWithAgentCapabilitiesAndClientIP(
				db,
				"feed-auth-secret",
				nil,
				mustClientIPResolver(t),
			)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			request := httptest.NewRequest(http.MethodPost, path, nil)
			request.Header.Set("Authorization", "Bearer ur-feed-only")
			request.RemoteAddr = "127.0.0.1:54321"
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusForbidden {
				t.Fatalf("feed-only key reached %s: status=%d body=%s", path, response.Code, response.Body.String())
			}
		})
	}
}

type completedFeedAuthFixture struct {
	projectID    int64
	keyType      string
	purpose      string
	allowedIPs   string
	allowedModel string
	keyStatus    string
	orgStatus    string
}

func completedFeedAuthMockDB(t *testing.T, fixture completedFeedAuthFixture) *testutil.MockDB {
	t.Helper()
	return &testutil.MockDB{
		QueryRowFn: func(_ context.Context, _ string, _ ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				if len(dest) != 15 {
					t.Fatalf("unexpected API-key scan width: %d", len(dest))
				}
				*dest[0].(*int64) = 42
				*dest[1].(*int64) = 7
				*dest[2].(*int64) = fixture.projectID
				*dest[3].(*string) = fixture.keyStatus
				*dest[4].(*string) = fixture.allowedIPs
				*dest[5].(*string) = fixture.allowedModel
				*dest[6].(*int) = 60
				*dest[7].(*int) = 10
				*dest[8].(**int) = nil
				*dest[9].(*string) = "ur-feed"
				*dest[10].(*string) = "xinsight-feed"
				*dest[11].(*int64) = 0
				*dest[12].(*string) = fixture.keyType
				*dest[13].(*string) = fixture.purpose
				*dest[14].(*string) = fixture.orgStatus
				return nil
			}}
		},
	}
}
