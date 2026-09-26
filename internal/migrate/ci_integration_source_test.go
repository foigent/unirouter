package migrate

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Follow the production workflow's runner entry, then inspect only the active
// selector declarations and consolidated invocation. Comments and other lanes
// cannot satisfy these migration/Coordinator coverage guards.
func consolidatedIntegrationSource(t *testing.T) string {
	t.Helper()
	workflowBytes, err := os.ReadFile("../../.github/workflows/ci.yml")
	if err != nil {
		t.Fatalf("read CI workflow: %v", err)
	}
	workflow := string(workflowBytes)
	const name = "- name: Run consolidated backend integration and feature gates"
	start := strings.Index(workflow, name)
	if start < 0 {
		t.Fatal("CI consolidated integration gate is missing")
	}
	lane := workflow[start+len(name):]
	if next := strings.Index(lane, "\n      - name:"); next >= 0 {
		lane = lane[:next]
	}
	if !regexp.MustCompile(`(?m)^        run: bash scripts/test-backend-integration\.sh\s*$`).MatchString(lane) {
		t.Fatal("CI consolidated integration gate must invoke the shared integration runner")
	}
	sourceBytes, err := os.ReadFile("../../scripts/test-backend-integration.sh")
	if err != nil {
		t.Fatalf("read integration runner: %v", err)
	}
	source := string(sourceBytes)
	declarations := regexp.MustCompile(`(?m)^test_pattern\+?='([^']*)'$`).FindAllStringSubmatch(source, -1)
	if len(declarations) == 0 || !strings.Contains(source, "\nreadonly test_pattern\n") {
		t.Fatal("shared integration selector declarations are missing")
	}
	var pattern strings.Builder
	for _, declaration := range declarations {
		pattern.WriteString(declaration[1])
	}
	if _, err := regexp.Compile(pattern.String()); err != nil {
		t.Fatalf("invalid integration selector: %v", err)
	}
	const command = "run_group consolidated go test -tags=integration -race"
	start = strings.Index(source, "\n"+command)
	if start < 0 {
		t.Fatal("shared consolidated runner must execute tagged race tests")
	}
	invocation := source[start+1:]
	if end := strings.Index(invocation, "\n\n"); end >= 0 {
		invocation = invocation[:end]
	}
	if !strings.Contains(invocation, `-run "$test_pattern"`) {
		t.Fatal("consolidated command must consume the guarded selector")
	}
	return pattern.String() + "\n" + invocation
}
