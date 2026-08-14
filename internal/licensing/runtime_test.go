package licensing

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func commercialRuntimeFixture(t *testing.T, features []string) (*Runtime, ed25519.PrivateKey) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	verifier := NewCommercialVerifier(map[string]ed25519.PublicKey{"license-2026-a": publicKey}, nil, 30, "prod-cn-1").WithNow(func() time.Time {
		return time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	})
	runtime := NewRuntime(verifier)
	payload := commercialPayload()
	payload.Features = features
	doc, err := SignCommercialPayload(payload, "license-2026-a", privateKey)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	runtime.Replace(raw)
	return runtime, privateKey
}

func TestRuntimeAuthorizesFeaturesAndLimits(t *testing.T) {
	runtime, _ := commercialRuntimeFixture(t, []string{FeatureGateway})
	if decision := runtime.Authorize(FeatureGateway); !decision.Allowed {
		t.Fatalf("gateway should be allowed: %#v", decision)
	}
	if decision := runtime.Authorize(FeatureAgentCapabilities); decision.Allowed || decision.Code != "license_error.feature_not_licensed" {
		t.Fatalf("agent feature should be denied: %#v", decision)
	}
	for _, feature := range []string{FeatureGuardrails, FeatureNotifications, FeatureModelGovernance} {
		if decision := runtime.Authorize(feature); !decision.Allowed {
			t.Fatalf("core management feature %s should follow the enforceable License: %#v", feature, decision)
		}
	}
	if limit, ok := runtime.Limit("active_projects"); !ok || limit != 20 {
		t.Fatalf("expected signed project limit, got %d %v", limit, ok)
	}
}

func TestRuntimeCapabilitiesExposeOnlyConcreteAuthorizationResults(t *testing.T) {
	t.Run("partial", func(t *testing.T) {
		runtime, _ := commercialRuntimeFixture(t, []string{FeatureGateway})
		capabilities := runtime.Capabilities()
		if capabilities.Status != StatusActive || !capabilities.Enforceable {
			t.Fatalf("unexpected partial license status: %#v", capabilities)
		}
		if !capabilities.Features[FeatureGateway] || !capabilities.Features[FeatureNotifications] ||
			!capabilities.Features[FeatureModelGovernance] || !capabilities.Features[FeatureGuardrails] {
			t.Fatalf("licensed features missing: %#v", capabilities.Features)
		}
		if capabilities.Features[FeatureAgentCapabilities] || capabilities.Features[FeatureOIDC] || capabilities.Features[FeatureDataExchange] {
			t.Fatalf("independently licensed features must remain denied: %#v", capabilities.Features)
		}
		if _, ok := capabilities.Features[FeatureAll]; ok {
			t.Fatalf("wildcard must be expanded instead of exposed: %#v", capabilities.Features)
		}
		if len(capabilities.Features) != len(concreteFeatures) {
			t.Fatalf("expected every concrete feature, got %#v", capabilities.Features)
		}
	})

	t.Run("wildcard", func(t *testing.T) {
		runtime, _ := commercialRuntimeFixture(t, []string{FeatureAll})
		capabilities := runtime.Capabilities()
		for _, feature := range concreteFeatures {
			if !capabilities.Features[feature] {
				t.Fatalf("wildcard should authorize %s: %#v", feature, capabilities.Features)
			}
		}
	})

	t.Run("missing", func(t *testing.T) {
		runtime := NewRuntime(NewCommercialVerifier(nil, nil, 30, "prod-cn-1"))
		capabilities := runtime.Capabilities()
		if capabilities.Status != StatusMissing || capabilities.Enforceable {
			t.Fatalf("unexpected missing license status: %#v", capabilities)
		}
		for _, allowed := range capabilities.Features {
			if allowed {
				t.Fatalf("missing license must deny every feature: %#v", capabilities.Features)
			}
		}
	})

	t.Run("expired", func(t *testing.T) {
		publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		now := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
		verifier := NewCommercialVerifier(map[string]ed25519.PublicKey{"license-2026-a": publicKey}, nil, 30, "prod-cn-1").WithNow(func() time.Time {
			return now
		})
		runtime := NewRuntime(verifier)
		payload := commercialPayload()
		payload.Features = []string{FeatureAll}
		payload.ExpiresAt = now.Add(-time.Hour).Format(time.RFC3339)
		doc, err := SignCommercialPayload(payload, "license-2026-a", privateKey)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		runtime.Replace(raw)
		capabilities := runtime.Capabilities()
		if capabilities.Status != StatusExpired || capabilities.Enforceable {
			t.Fatalf("unexpected expired license status: %#v", capabilities)
		}
		for _, allowed := range capabilities.Features {
			if allowed {
				t.Fatalf("expired wildcard license must deny every feature: %#v", capabilities.Features)
			}
		}
	})
}

func TestRuntimeGateDeniesBeforeNextHandler(t *testing.T) {
	runtime := NewRuntime(NewCommercialVerifier(nil, nil, 30, "prod-cn-1"))
	called := false
	handler := runtime.Gate(FeatureGateway)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
	if called {
		t.Fatal("unlicensed request reached the downstream handler")
	}
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "license_error.missing") {
		t.Fatalf("unexpected denial: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRuntimeExpiryTakesEffectWithoutLicenseReload(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	verifier := NewCommercialVerifier(map[string]ed25519.PublicKey{"license-2026-a": publicKey}, nil, 30, "prod-cn-1").WithNow(func() time.Time {
		return now
	})
	runtime := NewRuntime(verifier)
	payload := commercialPayload()
	payload.Features = []string{FeatureGateway}
	payload.ExpiresAt = now.Add(time.Hour).Format(time.RFC3339)
	doc, err := SignCommercialPayload(payload, "license-2026-a", privateKey)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if status := runtime.Replace(raw); !status.Enforceable {
		t.Fatalf("new license should be enforceable: %#v", status)
	}
	if decision := runtime.Authorize(FeatureGateway); !decision.Allowed {
		t.Fatalf("gateway should initially be allowed: %#v", decision)
	}

	now = now.Add(2 * time.Hour)
	if decision := runtime.Authorize(FeatureGateway); decision.Allowed || decision.Code != "license_error.expired" {
		t.Fatalf("wall-clock expiry must deny without a reload: %#v", decision)
	}
}
