package modelcapabilitypublication_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/strictjson"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestPublicationStartupReadsOneCommittedProjectionGeneration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	p, err := publication.ReadStaticProjectionV2(ctx, pool)
	require.NoError(t, err)
	digest, err := p.Digest()
	require.NoError(t, err)
	// Structural reader fixtures do not invoke the future live publication
	// writer or claim Provider qualification. Retain honest exact plan bytes.
	target := map[string]string{"environment": "integration", "database_name": "fixture", "system_identifier": "fixture"}
	publisher := map[string]string{"approved_identity": "isolated-reader-fixture"}
	appendEvent := func(id, predecessor, kind, before, after, materializer string) {
		t.Helper()
		plan := map[string]any{"schema_version": publication.PlanSchemaV2, "projection_schema_version": publication.ProjectionSchemaV2,
			"publication_id": id, "expected_predecessor": predecessor, "kind": kind, "expected_projection_digest": before,
			"result_projection_digest": after, "materializer_contract_version": materializer,
			"bundle_digest": "", "legacy_through": "", "legacy_projection_digest": "", "target_identity": target, "publisher_identity": publisher}
		var legacyHead, legacyDigest, bundleDigest, bundle any
		if kind == "legacy_import" {
			legacyHead = schemaauthority.LegacyCapabilityHead
			legacyDigest = digest
			plan["legacy_through"] = legacyHead
			plan["legacy_projection_digest"] = legacyDigest
		} else {
			wire := []byte(`{"fixture":"non-activating delta"}`)
			bundle = wire
			bundleDigest = fmt.Sprintf("%x", sha256.Sum256(wire))
			plan["bundle_digest"] = bundleDigest
		}
		wire, err := json.Marshal(plan)
		require.NoError(t, err)
		wire, err = strictjson.Canonicalize(wire)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `INSERT INTO capability_publications(publication_id,predecessor_id,kind,legacy_through,legacy_projection_digest,
expected_projection_digest,result_projection_digest,plan_digest,plan_bytes,bundle_digest,bundle_bytes,materializer_contract,target_identity,publisher_identity)
VALUES($1,NULLIF($2,''),$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, id, predecessor, kind, legacyHead, legacyDigest, before, after, fmt.Sprintf("%x", sha256.Sum256(wire)), wire, bundleDigest, bundle, materializer, target, publisher)
		require.NoError(t, err)
	}
	appendEvent("import", "", "legacy_import", digest, digest, publication.MaterializerContractV2)
	require.NoError(t, publication.VerifyPublicationProjectionV2(ctx, pool))
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	head, err := publication.ReadPublicationHeadV2(ctx, tx)
	require.NoError(t, err)
	require.Equal(t, "import", head.PublicationID)
	// Writer implementation identity need not equal every runtime's complete
	// registry. Runtime must still understand the retained projection format.
	appendEvent("no-op", "import", "delta", digest, digest, "unirouter.capability-materializer.future")
	require.NoError(t, publication.VerifyPublicationProjectionV2(ctx, pool))
	appendEvent("corrupt", "no-op", "delta", digest, strings.Repeat("a", 64), publication.MaterializerContractV2)
	require.ErrorContains(t, publication.VerifyPublicationProjectionV2(ctx, pool), "real capability projection differs")
	require.NoError(t, publication.VerifyPublicationProjectionV2(ctx, tx), "concurrent commits cannot splice startup's retained snapshot")
	require.NoError(t, tx.Commit(ctx))
}
