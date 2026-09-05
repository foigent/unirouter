package runtimegeneration_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestIngressMembershipRequiresLiveExactSchemaAndImplementationSupport(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	m := runtimegeneration.CurrentManifest([]string{schemaauthority.LegacyCapabilityHead, schemaauthority.CapabilityExpandHead})
	wire, err := m.CanonicalBytes()
	require.NoError(t, err)
	digest, err := m.Digest()
	require.NoError(t, err)
	artifact := "sha256:" + strings.Repeat("a", 64)
	_, err = pool.Exec(ctx, `INSERT INTO runtime_support_manifests(artifact_digest,manifest_digest,canonical_bytes) VALUES($1,$2,$3)`, artifact, digest, wire)
	require.NoError(t, err)
	insert := `INSERT INTO runtime_generations(instance_id,artifact_digest,compatibility_epoch,schema_head,state,ingress_state,lease_expires_at)
VALUES($1,$2,$3,$4,'serving','pending_join',clock_timestamp()+$5::interval)`
	id := "uri_" + strings.Repeat("a", 32)
	_, err = pool.Exec(ctx, insert, id, artifact, runtimegeneration.CompatibilityEpoch, schemaauthority.LegacyCapabilityHead, "30 seconds")
	require.ErrorContains(t, err, "current schema head")
	_, err = pool.Exec(ctx, insert, id, artifact, "invented-epoch", schemaauthority.CapabilityExpandHead, "30 seconds")
	require.ErrorContains(t, err, "current schema head")
	_, err = pool.Exec(ctx, insert, id, artifact, runtimegeneration.CompatibilityEpoch, schemaauthority.CapabilityExpandHead, "-1 seconds")
	require.ErrorContains(t, err, "live serving")
	_, err = pool.Exec(ctx, insert, id, artifact, runtimegeneration.CompatibilityEpoch, schemaauthority.CapabilityExpandHead, "30 seconds")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE runtime_generations SET ingress_state='receiving' WHERE instance_id=$1`, id)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE runtime_generations SET instance_id=$2 WHERE instance_id=$1`, id, "uri_"+strings.Repeat("b", 32))
	require.ErrorContains(t, err, "artifact identity is immutable")
	_, err = pool.Exec(ctx, `UPDATE runtime_support_manifests SET canonical_bytes=canonical_bytes WHERE artifact_digest=$1`, artifact)
	require.Error(t, err)
	// Bridge implementation cannot activate a new Release interpreter merely
	// because it recognizes the same data Profile. Mandatory requirements hold.
	complete := m
	m.Implementations = nil
	for _, ref := range complete.Implementations {
		if ref.Kind != "runtime_release" {
			m.Implementations = append(m.Implementations, ref)
		}
	}
	wire, err = m.CanonicalBytes()
	require.NoError(t, err)
	snapshot, _ := json.Marshal(map[string]any{"schema_version": "unirouter.release-execution.v1", "requirements": []modelprofile.ImplementationReference{{Kind: "runtime_release", ID: "unirouter.release-execution.v1"}}})
	_, err = pool.Exec(ctx, `SELECT require_runtime_snapshot_support($1,$2)`, snapshot, wire)
	require.ErrorContains(t, err, "cannot interpret exact Release")
	m.Implementations = append(m.Implementations, modelprofile.ImplementationReference{Kind: "runtime_release", ID: "unirouter.release-execution.v1"})
	wire, err = m.CanonicalBytes()
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `SELECT require_runtime_snapshot_support($1,$2)`, snapshot, wire)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `SELECT require_runtime_snapshot_support($1,$2)`, []byte(`{"schema_version":"unirouter.release-execution.v1","requirements":[]}`), wire)
	require.Error(t, err)
}
