package modelprofile_test

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestDefinitionDatabasePinsCannotFallbackOrRewriteHistory(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	p, ok := modelprofile.Lookup("glm.5.3.api", "1.9.0")
	require.True(t, ok)
	p.Version = "99.0.0"
	d, err := modelprofile.NewDefinition(p, "glm/glm-5.3", "")
	require.NoError(t, err)
	insert := `INSERT INTO model_profile_definitions(definition_digest,profile_id,profile_version,product_lineage_id,
schema_version,canonical_bytes,publication_id) VALUES($1,$2,$3,$4,$5,$6,'fixture-import')`
	_, err = pool.Exec(ctx, insert, d.Digest(), p.ID, p.Version, d.ProductLineageID(), modelprofile.DefinitionSchemaV1, d.CanonicalBytes())
	require.ErrorContains(t, err, "same transaction publication owner")
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	plan := []byte(`{"fixture":"isolated database structural authority test"}`)
	// This is explicit fixture setup, not a fabricated live Publication receipt.
	_, err = tx.Exec(ctx, `INSERT INTO capability_publications(publication_id,kind,legacy_through,legacy_projection_digest,
expected_projection_digest,result_projection_digest,plan_digest,plan_bytes,materializer_contract,target_identity,publisher_identity)
VALUES('fixture-import','legacy_import','000113_glm_stream_function_tools_qualification',$1,$1,$1,$2,$3,'fixture','{}','{}')`, strings.Repeat("0", 64), fmt.Sprintf("%x", sha256.Sum256(plan)), plan)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `SELECT set_config('unirouter.capability_publication_id','fixture-import',true)`)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, insert, d.Digest(), p.ID, p.Version, d.ProductLineageID(), modelprofile.DefinitionSchemaV1, d.CanonicalBytes())
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	read, err := modelprofile.ReadDefinition(ctx, pool, d.Digest())
	require.NoError(t, err)
	require.Equal(t, d.CanonicalBytes(), read.CanonicalBytes())
	resolved, err := modelprofile.ResolvePinned(ctx, pool, p.ID, p.Version, d.Digest())
	require.NoError(t, err)
	require.Equal(t, d.Profile(), resolved)
	_, ok = modelprofile.Lookup(p.ID, p.Version)
	require.False(t, ok)
	_, err = modelprofile.ResolvePinned(ctx, pool, p.ID, "1.9.0", d.Digest())
	require.Error(t, err)
	_, err = modelprofile.ResolvePinned(ctx, pool, p.ID, "1.9.0", strings.Repeat("a", 64))
	require.Error(t, err, "no compiled fallback for missing content")
	_, err = modelprofile.ResolvePinned(ctx, pool, p.ID, "1.9.0", "")
	require.NoError(t, err, "legacy absence stays explicit")
	_, err = pool.Exec(ctx, `UPDATE model_profile_definitions SET product_lineage_id='changed' WHERE definition_digest=$1`, d.Digest())
	require.Error(t, err)
	_, err = pool.Exec(ctx, `DELETE FROM model_profile_definitions WHERE definition_digest=$1`, d.Digest())
	require.Error(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO model_profile_definitions SELECT definition_digest,profile_id,'99.0.1',product_lineage_id,predecessor_digest,schema_version,canonical_bytes,publication_id,created_at FROM model_profile_definitions`)
	require.Error(t, err, "publication identity from an old transaction cannot authorize another write")
}
