package modelcapabilitypublication_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	qualification "github.com/uniroute/unirouter/internal/modelcapabilityqualification"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestOnlineMaterializerUsesDeltaLedgerAndExactImmutableData(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	ledger, err := schemaauthority.ExpectedLedger("../../migrations")
	require.NoError(t, err)
	authority := publication.AuthorityV2{Environment: "integration", ArtifactMigrationLedger: ledger,
		Publisher: publication.PublisherIdentityV2{Artifact: publication.ArtifactIdentity{Kind: "source", Commit: strings.Repeat("a", 40), Tree: strings.Repeat("b", 40), Version: "test", PublisherExecutableDigest: strings.Repeat("c", 64)}},
		ValidateBinding: func(b publication.DefinitionBindingV2, d modelprofile.Definition) error {
			_, err := qualification.BindDefinition(qualification.Binding{OperationID: b.OperationID, CapabilityID: b.CapabilityID,
				ExecutionProfileID: b.ExecutionProfileID, ExecutionProfileVersion: b.ExecutionProfileVersion,
				ProtocolProfileID: b.ProtocolProfileID, ProtocolProfileVersion: b.ProtocolProfileVersion,
				ProbeContractID: b.ProbeContractID, ProbeContractVersion: b.ProbeContractVersion,
				ExpectedProvider: b.ExpectedProvider, ExpectedUpstreamModel: b.ExpectedUpstreamModel, PublicPath: b.PublicPath}, d)
			return err
		}}
	build := func(a publication.AuthorityV2, r publication.PlanRequestV2) (publication.PlanV2, error) {
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if err != nil {
			return publication.PlanV2{}, err
		}
		defer tx.Rollback(ctx)
		return publication.BuildPlanV2(ctx, tx, a, r)
	}
	count := func(table string) int64 {
		var n int64
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM `+pgx.Identifier{table}.Sanitize()).Scan(&n))
		return n
	}
	initialSchemaCount := count("schema_migrations")
	initialProbeCount := count("model_capability_probes")
	initialApprovalCount := count("model_capability_routing_approvals")
	importRequest := publication.PlanRequestV2{PublicationID: "import-fixture", Kind: "legacy_import"}
	importPlan, err := build(authority, importRequest)
	require.NoError(t, err)
	require.Equal(t, schemaauthority.LegacyCapabilityHead, importPlan.LegacyThrough)
	require.Equal(t, int64(0), count("capability_publications"), "plan is read-only")
	_, err = publication.ApplyV2(ctx, pool, authority, importRequest, importPlan)
	require.ErrorContains(t, err, "proved ingress membership")
	require.Equal(t, int64(0), count("capability_publications"))
	// Explicit isolated fleet fixture. It proves DB coordination semantics only,
	// never a live Nginx or Target generation.
	manifest := runtimegeneration.CurrentManifest(schemaauthority.SupportedSchemaHeads())
	wire, err := manifest.CanonicalBytes()
	require.NoError(t, err)
	digest, err := manifest.Digest()
	require.NoError(t, err)
	artifact := "sha256:" + strings.Repeat("d", 64)
	instance := "uri_" + strings.Repeat("e", 32)
	_, err = pool.Exec(ctx, `INSERT INTO runtime_support_manifests(artifact_digest,manifest_digest,canonical_bytes) VALUES($1,$2,$3)`, artifact, digest, wire)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO runtime_generations(instance_id,artifact_digest,compatibility_epoch,schema_head,state,ingress_state,lease_expires_at)
 VALUES($1,$2,$3,$4,'serving','receiving',clock_timestamp()+interval '1 hour')`, instance, artifact, runtimegeneration.CompatibilityEpoch, schemaauthority.LatestSupportedMigration)
	require.NoError(t, err)
	result, err := publication.ApplyV2(ctx, pool, authority, importRequest, importPlan)
	require.NoError(t, err)
	require.Equal(t, "committed", result.Status)
	require.NoError(t, publication.VerifyPublicationProjectionV2(ctx, pool))
	replay, err := publication.ApplyV2(ctx, pool, authority, importRequest, importPlan)
	require.NoError(t, err)
	require.Equal(t, "already_committed", replay.Status)
	p, ok := modelprofile.Lookup("glm.5.3.api", "1.9.0")
	require.True(t, ok)
	p.Version = "99.0.0"
	definition, err := modelprofile.NewDefinition(p, "glm/glm-5.3", "")
	require.NoError(t, err)
	selector := publication.Selector{OperationID: modelprofile.OperationOpenAIChat, CapabilityID: modelprofile.CapabilityFunctionTools,
		ExecutionProfileID: p.ID, ExecutionProfileVersion: p.Version, ProtocolProfileID: p.ProtocolProfile, ProtocolProfileVersion: "1.0.0",
		ProbeContractID: qualification.GLMStreamFunctionToolsProbeContractID, ProbeContractVersion: "1.0.0"}
	bundle := publication.BundleV2{SchemaVersion: publication.BundleSchemaV2, MaterializerContract: publication.MaterializerContractV2,
		Definitions: []json.RawMessage{definition.CanonicalBytes()}, BindingDelta: []publication.DefinitionBindingV2{{Selector: selector, ProfileDefinitionDigest: definition.Digest(), ExpectedProvider: p.Provider, ExpectedUpstreamModel: "glm-5.3", PublicPath: "/v1/chat/completions"}},
		SelectorDelta: []publication.SelectorTransition{{OperationID: selector.OperationID, CapabilityID: selector.CapabilityID, ExecutionProfileID: selector.ExecutionProfileID, ExecutionProfileVersion: selector.ExecutionProfileVersion, ProtocolProfileID: selector.ProtocolProfileID, ProtocolProfileVersion: selector.ProtocolProfileVersion,
			Final: publication.ContractIdentity{ProbeContractID: selector.ProbeContractID, ProbeContractVersion: selector.ProbeContractVersion}}}}
	bundleBytes, err := bundle.CanonicalBytes()
	require.NoError(t, err)
	authority.ApprovedBundleDigest = fmt.Sprintf("%x", sha256.Sum256(bundleBytes))
	authority.Publisher.Carrier = &publication.CarrierIdentityV2{Repository: "fixture/repository", Commit: strings.Repeat("f", 40), Tree: strings.Repeat("a", 40), Path: "bundle.json", WorkflowRunID: 1, ArtifactID: 2}
	request := publication.PlanRequestV2{PublicationID: "data-glm-revision", Kind: "delta", BundleBytes: bundleBytes}
	plan, err := build(authority, request)
	require.NoError(t, err)
	planBytes, err := plan.CanonicalBytes()
	require.NoError(t, err)
	require.Less(t, len(planBytes), 8000, "plan contains delta and anchors only")
	require.Len(t, plan.BindingDelta, 1)
	require.Len(t, plan.SelectorDelta, 1)
	require.Len(t, plan.DefinitionDigests, 1)
	require.NotEqual(t, plan.ExpectedProjectionDigest, plan.ResultProjectionDigest)
	require.Equal(t, int64(0), count("model_profile_definitions"))
	// Same candidate, independently named, remains tied to the old predecessor.
	staleRequest := request
	staleRequest.PublicationID = "stale-glm-revision"
	stalePlan, err := build(authority, staleRequest)
	require.NoError(t, err)
	commitCalls := 0
	result, err = publication.ApplyV2WithCommitForTest(ctx, pool, authority, request, plan, func(ctx context.Context, tx pgx.Tx) error {
		commitCalls++
		leaseCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		_, _, err := runtimegeneration.Renew(leaseCtx, pool, runtimegeneration.Identity{Instance: instance, Artifact: artifact}, manifest, ledger, false)
		if err != nil {
			return fmt.Errorf("publication starved an unchanged serving heartbeat: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return io.ErrUnexpectedEOF // DB committed; caller did not receive its ACK.
	})
	require.NoError(t, err)
	require.Equal(t, "committed_readback", result.Status)
	require.Equal(t, 1, commitCalls, "ambiguous commit must not repeat the writer")
	require.Equal(t, initialSchemaCount, count("schema_migrations"))
	require.Equal(t, initialProbeCount, count("model_capability_probes"))
	require.Equal(t, initialApprovalCount, count("model_capability_routing_approvals"))
	require.NoError(t, publication.VerifyPublicationProjectionV2(ctx, pool))
	actual, err := modelprofile.ReadDefinition(ctx, pool, definition.Digest())
	require.NoError(t, err)
	require.Equal(t, definition.CanonicalBytes(), actual.CanonicalBytes())
	bindings, err := qualification.ResolveDefinitionBindings(ctx, pool, actual, selector.OperationID, selector.ProtocolProfileID, selector.ProtocolProfileVersion)
	require.NoError(t, err)
	require.Len(t, bindings, 1)
	_, err = qualification.BuildGLMStreamFunctionToolsProbeRequest(bindings[0])
	require.NoError(t, err)
	_, compiled := modelprofile.Lookup(p.ID, p.Version)
	require.False(t, compiled)
	_, err = publication.ApplyV2(ctx, pool, authority, staleRequest, stalePlan)
	require.Error(t, err)
	require.Equal(t, int64(2), count("capability_publications"))
	recovered, err := publication.RecoverV2(ctx, pool, authority, plan)
	require.NoError(t, err)
	require.Equal(t, "committed_readback", recovered.Status)
	// Unknown commit outcome is resolved by exactly retained bytes, not by
	// assuming absence/success or rewriting the attempted identity.
	wrong := plan
	wrong.ResultProjectionDigest = strings.Repeat("0", 64)
	_, err = publication.RecoverV2(ctx, pool, authority, wrong)
	require.Error(t, err)
	// A lifetime-expired receiver remains part of ingress membership and blocks
	// further publication even though it cannot renew a serving lease.
	p.Version = "99.0.1"
	next, err := modelprofile.NewDefinition(p, definition.ProductLineageID(), definition.Digest())
	require.NoError(t, err)
	nextBundle := publication.BundleV2{SchemaVersion: publication.BundleSchemaV2, MaterializerContract: publication.MaterializerContractV2, Definitions: []json.RawMessage{next.CanonicalBytes()}, BindingDelta: []publication.DefinitionBindingV2{}, SelectorDelta: []publication.SelectorTransition{}}
	nextBytes, err := nextBundle.CanonicalBytes()
	require.NoError(t, err)
	authority.ApprovedBundleDigest = fmt.Sprintf("%x", sha256.Sum256(nextBytes))
	nextRequest := publication.PlanRequestV2{PublicationID: "next-revision", Kind: "compensation", CompensatesPublicationID: request.PublicationID, BundleBytes: nextBytes}
	nextPlan, err := build(authority, nextRequest)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE runtime_generations SET ingress_state='withdrawing',state='draining',lease_expires_at=clock_timestamp()-interval '1 second' WHERE instance_id=$1`, instance)
	require.NoError(t, err)
	_, err = publication.ApplyV2(ctx, pool, authority, nextRequest, nextPlan)
	require.ErrorContains(t, err, "potential ingress receiver")
	require.Equal(t, int64(2), count("capability_publications"))
	// Restoring this isolated lease allows the same approved Plan. A compensating
	// publication appends; the earlier definition and receipt remain byte-identical.
	_, err = pool.Exec(ctx, `UPDATE runtime_generations SET lease_expires_at=clock_timestamp()+interval '1 hour' WHERE instance_id=$1`, instance)
	require.NoError(t, err)
	commitCalls = 0
	_, err = publication.ApplyV2WithCommitForTest(ctx, pool, authority, nextRequest, nextPlan, func(ctx context.Context, tx pgx.Tx) error {
		commitCalls++
		if err := tx.Rollback(ctx); err != nil {
			return err
		}
		return io.ErrUnexpectedEOF // No committed ledger; recovery must stay uncertain.
	})
	require.ErrorIs(t, err, publication.ErrPublicationCommitAmbiguous)
	require.Equal(t, 1, commitCalls)
	require.Equal(t, int64(2), count("capability_publications"))
	_, err = publication.ApplyV2(ctx, pool, authority, nextRequest, nextPlan)
	require.NoError(t, err)
	require.Equal(t, int64(3), count("capability_publications"))
	_, err = publication.RecoverV2(ctx, pool, authority, plan)
	require.NoError(t, err, "readback of retained publication remains valid after head advances")
	require.NoError(t, publication.VerifyPublicationProjectionV2(ctx, pool))
}
