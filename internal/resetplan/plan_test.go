package resetplan

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestBuildIsReadOnlyFailClosedAndDeterministic(t *testing.T) {
	now := time.Date(2026, 8, 2, 3, 4, 5, 123456789, time.FixedZone("test", 8*60*60))
	inventory := []TableInventory{
		{Name: "channels", RowCount: 3},
		{Name: "schema_migrations", RowCount: 55},
		{Name: "deployment_profiles", RowCount: 1},
	}
	first, err := Build(inventory, "ecs.first-integration", "ecs.second-integration", now)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	second, err := Build(inventory, "ecs.first-integration", "ecs.second-integration", now)
	if err != nil {
		t.Fatalf("Build second: %v", err)
	}
	if first.PlanSHA256 != second.PlanSHA256 || first.GeneratedAt != "2026-08-01T19:04:05.123456Z" {
		t.Fatalf("preview is not deterministic: first=%+v second=%+v", first, second)
	}
	if first.ActionAuthorized || first.ApplySupported || first.MutationPerformed || !first.ScopeComplete {
		t.Fatalf("dry-run boundary widened: %+v", first)
	}
	if first.Summary.ResetCandidateCount != 1 || first.Summary.ProtectedCount != 1 || first.Summary.SchemaLedgerCount != 1 {
		t.Fatalf("unexpected classification: %+v", first.Summary)
	}
	if len(first.Tables) != 3 || first.Tables[0].Name != "channels" || !strings.HasPrefix(first.PlanSHA256, "sha256:") {
		t.Fatalf("unexpected plan ordering or digest: %+v", first)
	}
}

func TestBuildRequiresNewExplicitSourceInstanceAndRejectsUnknownTables(t *testing.T) {
	now := time.Unix(0, 0)
	if _, err := Build(nil, "source-a", "source-a", now); err == nil {
		t.Fatal("reusing source_instance must fail")
	}
	plan, err := Build([]TableInventory{{Name: "future_unknown", RowCount: 0}}, "source-a", "source-b", now)
	if err != nil {
		t.Fatalf("Build unknown inventory: %v", err)
	}
	if plan.ScopeComplete || plan.Summary.ReviewRequiredCount != 1 || plan.Tables[0].Scope != ScopeReviewRequired {
		t.Fatalf("unknown table did not fail closed: %+v", plan)
	}
}

func TestEveryCurrentMigrationTableHasAnExplicitResetClassification(t *testing.T) {
	matches := regexp.MustCompile(`(?im)CREATE\s+TABLE(?:\s+IF\s+NOT\s+EXISTS)?\s+([a-z][a-z0-9_]*)`)
	paths, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.up.sql"))
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	seen := map[string]bool{"schema_migrations": true}
	for _, path := range paths {
		contents, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("read %s: %v", path, readErr)
		}
		for _, match := range matches.FindAllStringSubmatch(string(contents), -1) {
			seen[match[1]] = true
		}
	}
	var unknown []string
	for name := range seen {
		if scope, _ := ClassifyTable(name); scope == ScopeReviewRequired {
			unknown = append(unknown, name)
		}
	}
	sort.Strings(unknown)
	if len(unknown) != 0 {
		t.Fatalf("migration tables missing explicit reset classification: %v", unknown)
	}
}
