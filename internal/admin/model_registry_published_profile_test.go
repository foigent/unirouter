package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/testutil"
)

func registryPublishedDefinition(t *testing.T) modelprofile.Definition {
	t.Helper()
	d := registryProfileDefinition(t, "deepseek-v41-flash-provider-completion.json")
	require.Equal(t, "deepseek.v4.1-flash.api", d.Profile().ID)
	require.Equal(t, "1.1.0", d.Profile().Version)
	return d
}

func registryProfileDefinition(t *testing.T, file string) modelprofile.Definition {
	t.Helper()
	wire, err := os.ReadFile("../../capability-bundles/" + file)
	require.NoError(t, err)
	bundle, err := publication.DecodeBundleV2(wire)
	require.NoError(t, err)
	d, err := modelprofile.DecodeDefinition(bundle.Definitions[0])
	require.NoError(t, err)
	return d
}

func registryProfileRow(t *testing.T, values ...any) pgx.Row {
	t.Helper()
	return &testutil.MockRow{ScanFn: func(dest ...any) error {
		require.Len(t, dest, len(values))
		for i, value := range values {
			reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(value))
		}
		return nil
	}}
}

func TestUpdateModelRegistryRetainsPublishedProfile(t *testing.T) {
	for _, full := range []bool{false, true} {
		t.Run(map[bool]string{false: "two_fields", true: "full_snapshot"}[full], func(t *testing.T) {
			d := registryPublishedDefinition(t)
			p := d.Profile()
			writes, commits := 0, 0
			tx := &testutil.MockTx{
				QueryRowFn: func(_ context.Context, sql string, args ...any) pgx.Row {
					switch {
					case strings.Contains(sql, "FROM model_profile_definitions WHERE profile_id="):
						require.Equal(t, []any{p.ID, p.Version}, args)
						return registryProfileRow(t, d.Digest())
					case strings.Contains(sql, "FROM model_profile_definitions WHERE definition_digest="):
						require.Equal(t, []any{d.Digest()}, args)
						return registryProfileRow(t, p.ID, p.Version, d.ProductLineageID(), modelprofile.DefinitionSchemaV1, d.CanonicalBytes(), d.PredecessorDigest())
					case strings.Contains(sql, "product_lineage_id"):
						require.Equal(t, []any{int64(10), int64(2)}, args)
						return registryProfileRow(t, d.ProductLineageID())
					default:
						lifecycle, approved := "staging", false
						if !strings.Contains(sql, "FOR UPDATE") {
							lifecycle, approved = "ga", true
						}
						return registryProfileRow(t, int64(2), "deepseek-flash", "Flash", "deepseek", lifecycle,
							1_000_000, 384_000, "", "", "deepseek-flash", approved, "", p.ID, p.Version)
					}
				},
				ExecFn: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
					require.True(t, strings.HasPrefix(sql, "UPDATE model_registry SET") || strings.Contains(sql, "INSERT INTO audit_logs"), sql)
					writes++
					return pgconn.NewCommandTag("UPDATE 1"), nil
				},
				CommitFn: func(context.Context) error { commits++; return nil },
			}
			body := map[string]any{"lifecycle": "ga", "approved_catalog": true}
			if full {
				for k, v := range map[string]any{"model_name": "deepseek-flash", "display_name": "Flash", "provider_family": "deepseek",
					"context_length": 1_000_000, "output_limit": 384_000, "capability_tags": "", "data_residency": "", "pricing_model_name": "deepseek-flash",
					"migration_recommendation": "", "execution_profile_id": p.ID, "execution_profile_version": p.Version} {
					body[k] = v
				}
			}
			wire, err := json.Marshal(body)
			require.NoError(t, err)
			r := platformModelGovernanceRequest(httptest.NewRequest(http.MethodPut, "/model-registry/10?org_id=2", strings.NewReader(string(wire))))
			r.SetPathValue("id", "10")
			w := httptest.NewRecorder()
			h := newTestHandler(&testutil.MockDB{BeginFn: func(context.Context) (pgx.Tx, error) { return tx, nil }})
			h.UpdateModelRegistry(w, r)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Equal(t, 2, writes)
			require.Equal(t, 1, commits)
		})
	}
}

func TestUpdateModelRegistryRejectsInvalidRetainedDefinition(t *testing.T) {
	for _, kind := range []string{"missing_lineage", "wrong_lineage", "missing_address", "wrong_digest", "wrong_indexed_identity"} {
		t.Run(kind, func(t *testing.T) {
			d := registryPublishedDefinition(t)
			p := d.Profile()
			tx := &testutil.MockTx{
				QueryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
					switch {
					case strings.Contains(sql, "FOR UPDATE"):
						return registryProfileRow(t, int64(2), "deepseek-flash", "Flash", "deepseek", "staging", 1_000_000, 384_000, "", "", "deepseek-flash", false, "", p.ID, p.Version)
					case strings.Contains(sql, "product_lineage_id") && strings.Contains(sql, "FROM model_registry"):
						lineage := d.ProductLineageID()
						if kind == "missing_lineage" {
							lineage = ""
						}
						if kind == "wrong_lineage" {
							lineage = "other/product"
						}
						return registryProfileRow(t, lineage)
					case strings.Contains(sql, "WHERE profile_id="):
						if kind == "missing_address" {
							return &testutil.MockRow{ScanFn: func(...any) error { return pgx.ErrNoRows }}
						}
						digest := d.Digest()
						if kind == "wrong_digest" {
							digest = strings.Repeat("f", 64)
						}
						return registryProfileRow(t, digest)
					case strings.Contains(sql, "WHERE definition_digest="):
						id := p.ID
						if kind == "wrong_indexed_identity" {
							id = "other.product.api"
						}
						return registryProfileRow(t, id, p.Version, d.ProductLineageID(), modelprofile.DefinitionSchemaV1, d.CanonicalBytes(), d.PredecessorDigest())
					default:
						t.Fatalf("unexpected query: %s", sql)
						return nil
					}
				},
				ExecFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
					t.Fatal("invalid exact binding must not write")
					return pgconn.CommandTag{}, nil
				},
				CommitFn: func(context.Context) error { t.Fatal("invalid exact binding must not commit"); return nil },
			}
			r := platformModelGovernanceRequest(httptest.NewRequest(http.MethodPut, "/model-registry/10?org_id=2", strings.NewReader(`{"lifecycle":"ga","approved_catalog":true}`)))
			r.SetPathValue("id", "10")
			w := httptest.NewRecorder()
			h := newTestHandler(&testutil.MockDB{BeginFn: func(context.Context) (pgx.Tx, error) { return tx, nil }})
			h.UpdateModelRegistry(w, r)
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), "invalid_model_execution_profile")
		})
	}
}
