package admin

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCanonicalAdminCurrentReadsHaveNoLegacyRouteAuthority(t *testing.T) {
	_, sourcePath, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	dir := filepath.Dir(sourcePath)
	files := []string{
		"model_endpoint_projection.go",
		"model_readiness_handlers.go",
		"dashboard_handlers.go",
	}
	for _, name := range files {
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		source := string(body)
		for _, forbidden := range []string{
			"FROM channels", "JOIN channels", "channel_keys",
			"runtime_channel_assignments_v1", ".Preview(",
		} {
			if strings.Contains(source, forbidden) {
				t.Fatalf("%s retains legacy current-read authority %q", name, forbidden)
			}
		}
	}
}

func TestCanonicalAdminContractsNameExactDeploymentOperationIdentity(t *testing.T) {
	_, sourcePath, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	dir := filepath.Dir(sourcePath)
	assertions := map[string][]string{
		"model_readiness_handlers.go": {
			"deployment_id", "ModelOperationID", "modelrouting.ModeCanonical",
			"/model-endpoints?model=",
		},
		"dashboard_handlers.go": {
			"route_deployment_id,route_model_operation_id",
			"historical_identity_authority",
		},
		"cost_optimization_handlers.go": {
			"canonical_bypass",
		},
	}
	for name, required := range assertions {
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, token := range required {
			if !strings.Contains(string(body), token) {
				t.Fatalf("%s missing canonical contract token %q", name, token)
			}
		}
	}
}
