package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func mustClientIPResolver(t *testing.T, cidrs ...string) ClientIPResolver {
	t.Helper()
	resolver, err := NewClientIPResolver(cidrs)
	if err != nil {
		t.Fatalf("create client IP resolver: %v", err)
	}
	return resolver
}

func TestClientIPResolverEmptyTrustIgnoresSpoofedForwardingHeaders(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "198.51.100.20:4321"
	request.Header.Set("X-Forwarded-For", "10.0.0.1")
	request.Header.Set("X-Real-IP", "10.0.0.2")

	if got := (ClientIPResolver{}).Resolve(request); got != "198.51.100.20" {
		t.Fatalf("untrusted peer must resolve from RemoteAddr, got %q", got)
	}
}

func TestClientIPResolverTrustedFrontendRejectsClientPrependedSpoof(t *testing.T) {
	resolver := mustClientIPResolver(t, "172.30.250.10/32")
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "172.30.250.10:4321"
	// frontend nginx appends the network peer on the right. The left value is a
	// client-supplied spoof and must not win.
	request.Header.Set("X-Forwarded-For", "10.0.0.1, 198.51.100.20")

	if got := resolver.Resolve(request); got != "198.51.100.20" {
		t.Fatalf("expected first untrusted hop from the right, got %q", got)
	}
}

func TestClientIPResolverTrustedFrontendAndTLSEdgeResolveBrowser(t *testing.T) {
	resolver := mustClientIPResolver(t, "172.30.250.10/32", "172.30.250.1/32")
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "172.30.250.10:4321"
	request.Header.Set("X-Forwarded-For", "203.0.113.77, 172.30.250.1")

	if got := resolver.Resolve(request); got != "203.0.113.77" {
		t.Fatalf("expected browser IP through trusted TLS edge and frontend, got %q", got)
	}
}

func TestClientIPResolverStopsAtUntrustedIntermediateProxy(t *testing.T) {
	resolver := mustClientIPResolver(t, "172.30.250.10/32")
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "172.30.250.10:4321"
	request.Header.Set("X-Forwarded-For", "203.0.113.77, 192.0.2.44")

	if got := resolver.Resolve(request); got != "192.0.2.44" {
		t.Fatalf("untrusted intermediate proxy must be the security boundary, got %q", got)
	}
}

func TestClientIPResolverMalformedTrustedChainFallsBackToPeer(t *testing.T) {
	resolver := mustClientIPResolver(t, "172.30.250.10/32", "172.30.250.1/32")
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "172.30.250.10:4321"
	request.Header.Set("X-Forwarded-For", "203.0.113.77, not-an-ip, 172.30.250.1")

	if got := resolver.Resolve(request); got != "172.30.250.10" {
		t.Fatalf("malformed trusted chain must fail safe to direct peer, got %q", got)
	}
}

func TestClientIPResolverXRealIPRequiresTrustedPeer(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "172.30.250.10:4321"
	request.Header.Set("X-Real-IP", "203.0.113.77")

	if got := (ClientIPResolver{}).Resolve(request); got != "172.30.250.10" {
		t.Fatalf("untrusted X-Real-IP must be ignored, got %q", got)
	}
	if got := mustClientIPResolver(t, "172.30.250.10/32").Resolve(request); got != "203.0.113.77" {
		t.Fatalf("trusted X-Real-IP should resolve, got %q", got)
	}
}

func TestNewClientIPResolverRejectsInvalidCIDR(t *testing.T) {
	if _, err := NewClientIPResolver([]string{"not-a-cidr"}); err == nil {
		t.Fatal("expected invalid trusted proxy CIDR to fail")
	}
	if _, err := NewClientIPResolver([]string{"0.0.0.0/0"}); err == nil {
		t.Fatal("expected trust-all proxy CIDR to fail")
	}
}
