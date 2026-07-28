package main

import (
	"os"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/completedfeed"
	"github.com/uniroute/unirouter/internal/config"
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

func TestSourceComposeBackendsShareCompletedExecutionFeedEnvironment(t *testing.T) {
	for _, name := range []string{
		"../../docker-compose.yml",
		"../../docker-compose.canary.yml",
		"../../deploy/customer/docker-compose.yml",
	} {
		srcBytes, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		src := string(srcBytes)
		for _, mapping := range []string{
			"COMPLETED_EXECUTION_FEED_ENABLED: ${COMPLETED_EXECUTION_FEED_ENABLED:-false}",
			"COMPLETED_EXECUTION_FEED_SOURCE_INSTANCE: ${COMPLETED_EXECUTION_FEED_SOURCE_INSTANCE:-}",
			"COMPLETED_EXECUTION_FEED_REFERENCE_MODE: ${COMPLETED_EXECUTION_FEED_REFERENCE_MODE:-legacy}",
		} {
			if count := strings.Count(src, mapping); count != 1 {
				t.Fatalf("%s must contain exactly one %q mapping, got %d", name, mapping, count)
			}
		}
	}
}

func TestCompletedExecutionFeedReferenceModesStayAligned(t *testing.T) {
	for _, pair := range [][2]string{
		{config.CompletedExecutionFeedReferenceModeLegacy, string(completedfeed.ReferenceModeLegacy)},
		{config.CompletedExecutionFeedReferenceModeShadow, string(completedfeed.ReferenceModeShadow)},
		{config.CompletedExecutionFeedReferenceModeNeutralCanary, string(completedfeed.ReferenceModeNeutralCanary)},
	} {
		if pair[0] != pair[1] {
			t.Fatalf("config mode=%q runtime mode=%q", pair[0], pair[1])
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
