package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestRuntimeSchemaAuthorityRequiresExactArtifactLedgerBeforeStartup(t *testing.T) {
	expected := []string{
		"000001_init_schema",
		"000078_atomic_model_release_authoring",
		schemaauthority.LatestSupportedMigration,
	}
	tests := []struct {
		name        string
		actual      []string
		encoded     string
		scanErr     error
		wantErr     string
		wantDetails string
	}{
		{name: "exact", actual: expected},
		{
			name:        "missing migration",
			actual:      []string{expected[0], expected[2]},
			wantErr:     "does not match migration artifact",
			wantDetails: expected[1],
		},
		{
			name:        "extra migration",
			actual:      []string{expected[0], "000077_unexpected_contract", expected[1], expected[2]},
			wantErr:     "does not match migration artifact",
			wantDetails: "000077_unexpected_contract",
		},
		{
			name:        "future migration",
			actual:      append(append([]string(nil), expected...), "000102_future_contract"),
			wantErr:     "does not match migration artifact",
			wantDetails: "000102_future_contract",
		},
		{
			name:        "duplicate migration",
			actual:      []string{expected[0], expected[1], expected[1], expected[2]},
			wantErr:     "duplicate migration",
			wantDetails: expected[1],
		},
		{name: "empty database ledger", actual: []string{}, wantErr: "database schema ledger is empty"},
		{name: "malformed database ledger", encoded: `{`, wantErr: "decode schema migration authority"},
		{name: "query failure", scanErr: errors.New("ledger unavailable"), wantErr: "read schema migration authority"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded := test.encoded
			if encoded == "" {
				payload, err := json.Marshal(test.actual)
				if err != nil {
					t.Fatalf("encode test ledger: %v", err)
				}
				encoded = string(payload)
			}
			reader := &testutil.MockDB{QueryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
				if !strings.Contains(sql, "jsonb_agg(version ORDER BY version)") || strings.Contains(sql, "MAX(version)") {
					t.Fatalf("schema gate query = %q", sql)
				}
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					if test.scanErr != nil {
						return test.scanErr
					}
					*dest[0].(*string) = encoded
					return nil
				}}
			}}
			err := requireExactRuntimeSchema(t.Context(), reader, expected)
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("exact schema rejected: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("schema error = %v, want %q", err, test.wantErr)
			}
			if test.wantDetails != "" && !strings.Contains(err.Error(), test.wantDetails) {
				t.Fatalf("schema error = %v, want details %q", err, test.wantDetails)
			}
		})
	}
}

func TestRuntimeSchemaAuthorityRejectsNilReader(t *testing.T) {
	if err := requireExactRuntimeSchema(t.Context(), nil, []string{schemaauthority.LatestSupportedMigration}); err == nil || !strings.Contains(err.Error(), "reader is required") {
		t.Fatalf("nil reader error = %v", err)
	}
}
