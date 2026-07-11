package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- checkIPWhitelist tests ---

func TestCheckIPWhitelist_EmptyWhitelist(t *testing.T) {
	if !checkIPWhitelist("192.168.1.1", "") {
		t.Fatal("empty whitelist should allow all IPs")
	}
}

func TestCheckIPWhitelist_ExactMatch(t *testing.T) {
	if !checkIPWhitelist("192.168.1.1", "192.168.1.1") {
		t.Fatal("exact match should be allowed")
	}
}

func TestCheckIPWhitelist_ExactMismatch(t *testing.T) {
	if checkIPWhitelist("192.168.1.2", "192.168.1.1") {
		t.Fatal("mismatched IP should be rejected")
	}
}

func TestCheckIPWhitelist_MultipleEntries(t *testing.T) {
	whitelist := "10.0.0.1, 192.168.1.1, 172.16.0.1"
	if !checkIPWhitelist("192.168.1.1", whitelist) {
		t.Fatal("IP in comma-separated list should be allowed")
	}
	if checkIPWhitelist("10.0.0.5", whitelist) {
		t.Fatal("IP not in list should be rejected")
	}
}

func TestCheckIPWhitelist_CIDRMatch(t *testing.T) {
	if !checkIPWhitelist("192.168.1.100", "192.168.1.0/24") {
		t.Fatal("IP in CIDR range should be allowed")
	}
	if checkIPWhitelist("192.168.2.1", "192.168.1.0/24") {
		t.Fatal("IP outside CIDR range should be rejected")
	}
}

func TestCheckIPWhitelist_MixedExactAndCIDR(t *testing.T) {
	whitelist := "10.0.0.1, 192.168.0.0/16"
	if !checkIPWhitelist("10.0.0.1", whitelist) {
		t.Fatal("exact match should work in mixed list")
	}
	if !checkIPWhitelist("192.168.99.99", whitelist) {
		t.Fatal("CIDR match should work in mixed list")
	}
	if checkIPWhitelist("172.16.0.1", whitelist) {
		t.Fatal("IP not matching either should be rejected")
	}
}

func TestCheckIPWhitelist_InvalidCIDR(t *testing.T) {
	// Invalid CIDR entries are silently skipped
	if checkIPWhitelist("192.168.1.1", "not-a-cidr/33") {
		t.Fatal("invalid CIDR should be skipped, not match")
	}
}

func TestCheckIPWhitelist_EmptyIP(t *testing.T) {
	// Edge case: empty client IP
	if checkIPWhitelist("", "192.168.1.1") {
		t.Fatal("empty IP should not match")
	}
}

func TestCheckIPWhitelist_TrailingComma(t *testing.T) {
	whitelist := "192.168.1.1,"
	if !checkIPWhitelist("192.168.1.1", whitelist) {
		t.Fatal("trailing comma should not break matching")
	}
}

func TestCheckIPWhitelist_SpacesInList(t *testing.T) {
	whitelist := "  192.168.1.1 , 10.0.0.1  "
	if !checkIPWhitelist("192.168.1.1", whitelist) {
		t.Fatal("spaces should be trimmed")
	}
	if !checkIPWhitelist("10.0.0.1", whitelist) {
		t.Fatal("spaces should be trimmed")
	}
}

// --- getClientIP tests ---

func TestGetClientIP_XForwardedFor_Single(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.50")
	ip := getClientIP(req)
	if ip != "203.0.113.50" {
		t.Fatalf("expected 203.0.113.50, got %s", ip)
	}
}

func TestGetClientIP_XForwardedFor_Multiple(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.50, 70.41.3.18, 150.172.238.178")
	ip := getClientIP(req)
	if ip != "203.0.113.50" {
		t.Fatalf("expected first IP 203.0.113.50, got %s", ip)
	}
}

func TestGetClientIP_XRealIP(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Real-IP", "10.0.0.1")
	ip := getClientIP(req)
	if ip != "10.0.0.1" {
		t.Fatalf("expected 10.0.0.1, got %s", ip)
	}
}

func TestGetClientIP_XForwardedFor_PriorityOver_XRealIP(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.50")
	req.Header.Set("X-Real-IP", "10.0.0.1")
	ip := getClientIP(req)
	if ip != "203.0.113.50" {
		t.Fatalf("X-Forwarded-For should take priority, got %s", ip)
	}
}

func TestGetClientIP_RemoteAddr(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	// httptest sets RemoteAddr to "192.0.2.1:1234"
	ip := getClientIP(req)
	if ip != "192.0.2.1" {
		t.Fatalf("expected 192.0.2.1, got %s", ip)
	}
}

func TestGetClientIP_EmptyXForwardedFor(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-For", "")
	req.Header.Set("X-Real-IP", "10.0.0.1")
	ip := getClientIP(req)
	if ip != "10.0.0.1" {
		t.Fatalf("empty X-Forwarded-For should fall through to X-Real-IP, got %s", ip)
	}
}

func TestGetClientIP_NoHeaders(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	ip := getClientIP(req)
	if ip == "" {
		t.Fatal("should return RemoteAddr when no headers present")
	}
}

// --- hashKey tests ---

func TestHashKey_Consistency(t *testing.T) {
	h1 := hashKey("test-key", "test-secret")
	h2 := hashKey("test-key", "test-secret")
	if h1 != h2 {
		t.Fatal("hashKey should be deterministic")
	}
}

func TestHashKey_DifferentInputs(t *testing.T) {
	h1 := hashKey("key1", "secret")
	h2 := hashKey("key2", "secret")
	if h1 == h2 {
		t.Fatal("different keys should produce different hashes")
	}
}

func TestHashKey_DifferentSecrets(t *testing.T) {
	h1 := hashKey("key", "secret1")
	h2 := hashKey("key", "secret2")
	if h1 == h2 {
		t.Fatal("different secrets should produce different hashes")
	}
}

func TestHashKey_HexLength(t *testing.T) {
	h := hashKey("any-key", "any-secret")
	if len(h) != 64 {
		t.Fatalf("expected 64-char hex string, got %d", len(h))
	}
}

// --- CORS middleware tests ---

func TestCORS_Headers(t *testing.T) {
	handler := CORSWithOptions(CORSOptions{AllowedOrigins: []string{"https://console.example.com"}})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "https://console.example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "https://console.example.com" {
		t.Fatal("expected configured CORS origin")
	}
	if rec.Header().Get("Access-Control-Max-Age") != "86400" {
		t.Fatal("expected 86400 max age")
	}
	allowedHeaders := splitComma(rec.Header().Get("Access-Control-Allow-Headers"))
	for _, expected := range []string{"X-Request-ID", "X-UniRouter-Client-Request-ID", "Idempotency-Key"} {
		found := false
		for _, allowed := range allowedHeaders {
			if allowed == expected {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected %s in browser-allowed headers: %#v", expected, allowedHeaders)
		}
	}
	for _, forbidden := range []string{"X-UniRouter-Evidence-Auth", "X-UniRouter-Agent-Id"} {
		for _, allowed := range allowedHeaders {
			if allowed == forbidden {
				t.Fatalf("trusted server evidence header %s must not be browser-enabled", forbidden)
			}
		}
	}
	exposedHeaders := splitComma(rec.Header().Get("Access-Control-Expose-Headers"))
	if len(exposedHeaders) != 3 {
		t.Fatalf("expected the three public identity response headers, got %#v", exposedHeaders)
	}
}

func TestCORS_Preflight(t *testing.T) {
	handler := CORSWithOptions(CORSOptions{AllowedOrigins: []string{"https://console.example.com"}})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called for OPTIONS")
	}))
	req := httptest.NewRequest("OPTIONS", "/test", nil)
	req.Header.Set("Origin", "https://console.example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
}

func TestCORS_AllowedMethods(t *testing.T) {
	handler := CORSWithOptions(CORSOptions{AllowedOrigins: []string{"https://console.example.com"}})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("POST", "/test", nil)
	req.Header.Set("Origin", "https://console.example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	methods := rec.Header().Get("Access-Control-Allow-Methods")
	for _, m := range []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"} {
		found := false
		for _, allowed := range splitComma(methods) {
			if allowed == m {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected method %s in allowed methods: %s", m, methods)
		}
	}
}

func splitComma(s string) []string {
	var result []string
	current := ""
	for _, c := range s {
		if c == ',' || c == ' ' {
			if current != "" {
				result = append(result, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}
