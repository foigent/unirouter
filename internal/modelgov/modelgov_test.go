package modelgov

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestCheckAccess_DeniesRetiredModel(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				if strings.Contains(sql, "FROM model_aliases") {
					return pgx.ErrNoRows
				}
				if strings.Contains(sql, "FROM model_registry") {
					*dest[0].(*string) = LifecycleRetired
					return nil
				}
				t.Fatalf("unexpected query: %s", sql)
				return nil
			}}
		},
	}

	result, err := CheckAccess(context.Background(), db, 1, 0, "gpt-old")
	if !errors.Is(err, ErrModelRetired) {
		t.Fatalf("expected ErrModelRetired, got %v", err)
	}
	if result.Allowed || result.Reason != "model_retired" {
		t.Fatalf("expected retired denial, got %+v", result)
	}
}

func TestCheckAccess_ProjectSetDeniesUnassignedModel(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				switch {
				case strings.Contains(sql, "FROM model_aliases"):
					return pgx.ErrNoRows
				case strings.Contains(sql, "FROM model_registry"):
					*dest[0].(*string) = LifecycleGA
				case strings.Contains(sql, "model_set_items"):
					*dest[0].(*int) = 0
				case strings.Contains(sql, "FROM project_model_sets"):
					*dest[0].(*int) = 1
				default:
					t.Fatalf("unexpected query: %s", sql)
				}
				return nil
			}}
		},
	}

	result, err := CheckAccess(context.Background(), db, 1, 42, "gpt-4o")
	if !errors.Is(err, ErrModelDenied) {
		t.Fatalf("expected ErrModelDenied, got %v", err)
	}
	if result.Allowed || result.Reason != "project_model_not_allowed" {
		t.Fatalf("expected project model denial, got %+v", result)
	}
}

func TestCheckAccess_ResolvesAliasAllowedByProjectSet(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				switch {
				case strings.Contains(sql, "FROM model_aliases"):
					*dest[0].(*string) = "gpt-4o"
				case strings.Contains(sql, "FROM model_registry"):
					*dest[0].(*string) = LifecycleGA
				case strings.Contains(sql, "model_set_items"):
					*dest[0].(*int) = 1
				case strings.Contains(sql, "FROM project_model_sets"):
					*dest[0].(*int) = 1
				default:
					t.Fatalf("unexpected query: %s", sql)
				}
				return nil
			}}
		},
	}

	result, err := CheckAccess(context.Background(), db, 1, 42, "company-chat-fast")
	if err != nil {
		t.Fatalf("expected allowed alias, got %v", err)
	}
	if !result.Allowed || result.ResolvedModel != "gpt-4o" || result.Alias != "company-chat-fast" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestCheckAccess_ProjectModelSetsBindProjectAndSetOrganization(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				switch {
				case strings.Contains(sql, "FROM model_aliases"):
					return pgx.ErrNoRows
				case strings.Contains(sql, "FROM model_registry"):
					return pgx.ErrNoRows
				case strings.Contains(sql, "FROM project_model_sets"):
					if !strings.Contains(sql, "JOIN projects") || !strings.Contains(sql, "ms.org_id = p.org_id") || !strings.Contains(sql, "p.org_id = $2") {
						t.Fatalf("project model-set query is not organization bound: %s", sql)
					}
					*dest[0].(*int) = 0
				default:
					t.Fatalf("unexpected query: %s", sql)
				}
				return nil
			}}
		},
	}

	result, err := CheckAccess(context.Background(), db, 7, 42, "gpt-4o")
	if err != nil {
		t.Fatalf("expected unconfigured project to preserve default allow, got %v", err)
	}
	if !result.Allowed {
		t.Fatalf("expected allowed result, got %+v", result)
	}
}

func TestFilterCallableModels_UsesOneQueryForManyCandidates(t *testing.T) {
	candidates := make([]string, 0, 130)
	for i := 0; i < 128; i++ {
		candidates = append(candidates, fmt.Sprintf("model-%03d", i))
	}
	candidates = append(candidates, " model-000 ", "")

	queryCount := 0
	database := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			t.Fatal("batch filtering must not issue per-model QueryRow calls")
			return nil
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			queryCount++
			if !strings.Contains(sql, "WITH candidates AS") ||
				!strings.Contains(sql, "FROM project_model_sets") ||
				!strings.Contains(sql, "JOIN model_aliases") ||
				!strings.Contains(sql, "LEFT JOIN model_registry") {
				t.Fatalf("batch query is missing governance predicates: %s", sql)
			}
			if args[0] != int64(9) || args[1] != int64(42) {
				t.Fatalf("unexpected scope arguments: %#v", args)
			}
			normalized, ok := args[2].([]string)
			if !ok || len(normalized) != 128 || normalized[0] != "model-000" || normalized[127] != "model-127" {
				t.Fatalf("unexpected normalized candidates: %#v", args[2])
			}
			return &testutil.MockRows{Rows: []func(dest ...any) error{
				func(dest ...any) error { *dest[0].(*string) = "model-000"; return nil },
				func(dest ...any) error { *dest[0].(*string) = "model-127"; return nil },
			}}, nil
		},
	}

	allowed, err := FilterCallableModels(context.Background(), database, 9, 42, candidates)
	if err != nil {
		t.Fatalf("FilterCallableModels returned error: %v", err)
	}
	if queryCount != 1 {
		t.Fatalf("expected one fixed-cost query, got %d", queryCount)
	}
	if len(allowed) != 2 || allowed[0] != "model-000" || allowed[1] != "model-127" {
		t.Fatalf("unexpected allowed models: %#v", allowed)
	}
}

func TestFilterCallableModels_QueryError(t *testing.T) {
	database := &testutil.MockDB{QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
		return nil, errors.New("database unavailable")
	}}

	_, err := FilterCallableModels(context.Background(), database, 1, 0, []string{"model"})
	if err == nil || !strings.Contains(err.Error(), "filter callable models") {
		t.Fatalf("expected wrapped batch query error, got %v", err)
	}
}

func TestNormalizeCSV(t *testing.T) {
	if got := NormalizeCSV(" chat ， vision,chat ,,tools "); got != "chat,vision,tools" {
		t.Fatalf("unexpected normalized csv: %q", got)
	}
}
