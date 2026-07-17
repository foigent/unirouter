package main

import (
	"os"
	"strings"
	"testing"
)

func TestFrontendNginxAPIProxiesPreserveStreamingAndAppendForwardedPeer(t *testing.T) {
	srcBytes, err := os.ReadFile("../../frontend/nginx.conf")
	if err != nil {
		t.Fatalf("read frontend nginx config: %v", err)
	}
	src := string(srcBytes)
	if !strings.Contains(src, "client_max_body_size 16m;") {
		t.Fatal("frontend nginx must admit the backend's 16 MiB Embeddings request profile")
	}

	for _, path := range []string{"/api/", "/v1/"} {
		block := nginxLocationBlock(t, src, path)
		for _, directive := range []string{
			"proxy_http_version 1.1;",
			"proxy_set_header Connection \"\";",
			"proxy_set_header X-Real-IP $remote_addr;",
			"proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;",
			"proxy_buffering off;",
			"proxy_cache off;",
			"proxy_read_timeout 300s;",
			"proxy_send_timeout 300s;",
		} {
			if !strings.Contains(block, directive) {
				t.Fatalf("frontend nginx location %s missing %q", path, directive)
			}
		}
	}
}

func TestFrontendNginxSPAResponsesCarryBaselineSecurityHeaders(t *testing.T) {
	srcBytes, err := os.ReadFile("../../frontend/nginx.conf")
	if err != nil {
		t.Fatalf("read frontend nginx config: %v", err)
	}
	block := nginxLocationBlock(t, string(srcBytes), "/")
	for _, directive := range []string{
		`add_header X-Content-Type-Options "nosniff" always;`,
		`add_header X-Frame-Options "DENY" always;`,
		`add_header Referrer-Policy "no-referrer" always;`,
		`add_header Permissions-Policy "camera=(), microphone=(), geolocation=()" always;`,
		`add_header Content-Security-Policy "frame-ancestors 'none'; base-uri 'self'; form-action 'self'" always;`,
	} {
		if !strings.Contains(block, directive) {
			t.Fatalf("frontend nginx SPA location missing %q", directive)
		}
	}
}

func nginxLocationBlock(t *testing.T, src, path string) string {
	t.Helper()
	marker := "location " + path + " {"
	start := strings.Index(src, marker)
	if start < 0 {
		t.Fatalf("frontend nginx location %s is missing", path)
	}
	remainder := src[start+len(marker):]
	end := strings.Index(remainder, "\n    }")
	if end < 0 {
		t.Fatalf("frontend nginx location %s is not closed", path)
	}
	return remainder[:end]
}
