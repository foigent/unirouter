package modelgov

import (
	"context"
	"errors"
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

func TestNormalizeCSV(t *testing.T) {
	if got := NormalizeCSV(" chat ， vision,chat ,,tools "); got != "chat,vision,tools" {
		t.Fatalf("unexpected normalized csv: %q", got)
	}
}
