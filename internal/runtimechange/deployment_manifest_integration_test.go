//go:build integration

package runtimechange_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/governancesim"
	"github.com/uniroute/unirouter/internal/integrationfixture"
	"github.com/uniroute/unirouter/internal/modeldeployment"
	"github.com/uniroute/unirouter/internal/modelrouteauthority"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/testutil"
)

type unusedDeploymentSnapshotBuilder struct{}

func (unusedDeploymentSnapshotBuilder) BuildRuntimeChangeSnapshots(
	context.Context,
	string,
	[]routingpolicy.ChannelOverride,
	[]routingpolicy.ChannelOverride,
) (governancesim.RouteSnapshot, governancesim.RouteSnapshot, error) {
	return governancesim.RouteSnapshot{}, governancesim.RouteSnapshot{}, nil
}

type recordingBeginTxPool struct {
	*pgxpool.Pool
	mu      sync.Mutex
	options []pgx.TxOptions
}

func (p *recordingBeginTxPool) BeginTx(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	p.mu.Lock()
	p.options = append(p.options, options)
	p.mu.Unlock()
	return p.Pool.BeginTx(ctx, options)
}

func (p *recordingBeginTxPool) assertLatestRepeatableRead(t *testing.T) {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	require.NotEmpty(t, p.options)
	require.Equal(t, pgx.RepeatableRead, p.options[len(p.options)-1].IsoLevel)
	require.False(t, p.options[len(p.options)-1].AccessMode == pgx.ReadOnly)
}

type blockingCutoverVerifier struct {
	entered chan struct{}
	release chan struct{}
}

func newLegacyLifecycleCommand(
	t *testing.T,
	pool *pgxpool.Pool,
	fixture canonicalDeploymentFixture,
	actorUserID int64,
	mutation runtimechange.DeploymentLifecycleMutation,
) runtimechange.DeploymentLifecycleCommand {
	t.Helper()
	var actorOrgID int64
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT org_id FROM users WHERE id=$1`, actorUserID).Scan(&actorOrgID))
	identity := fmt.Sprintf("manifest/%s/%s/%d/%d", t.Name(), mutation.Action, actorUserID, mutation.ExpectedReleaseVersion)
	keyHash := sha256.Sum256([]byte("key/" + identity))
	fingerprint := sha256.Sum256([]byte("fingerprint/" + identity))
	operation := sha256.Sum256([]byte("operation/" + identity))
	operationID := "manifest-" + fmt.Sprintf("%x", operation[:16])
	return runtimechange.DeploymentLifecycleCommand{
		Mutation:           mutation,
		CustomerOrgID:      fixture.OrgID,
		ActorUserID:        actorUserID,
		ActorOrgID:         actorOrgID,
		IdempotencyKeyHash: fmt.Sprintf("%x", keyHash),
		RequestFingerprint: "hmac-sha256:" + fmt.Sprintf("%x", fingerprint),
		ServerOperationID:  operationID,
		Audit: runtimechange.DeploymentLifecycleAudit{
			RequestID: operationID,
			Source:    "integration_test",
		},
	}
}

func authorLegacyCutoverRelease(
	t *testing.T,
	pool *pgxpool.Pool,
	manager *runtimechange.Manager,
	fixture canonicalDeploymentFixture,
	reason string,
) runtimechange.DeploymentRelease {
	t.Helper()
	embedding := fixture
	embedding.OperationID = "openai.embeddings.v1"
	embedding.OperationRevision = 1
	embedding.ProtocolProfileID = "integration.openai.embeddings.v1"
	embedding.ProtocolVersion = "1.0.0"
	_, err := pool.Exec(t.Context(), `
INSERT INTO model_deployment_operations (
    deployment_id,operation_id,support_state,evidence_state,evidence_ref,verified_at,
    revision,protocol_profile_id,protocol_profile_version)
VALUES ($1,$2,'enabled','live_probed','legacy cutover operation set',NOW(),1,$3,$4)
ON CONFLICT (deployment_id,operation_id) DO NOTHING`,
		embedding.DeploymentID, embedding.OperationID,
		embedding.ProtocolProfileID, embedding.ProtocolVersion)
	require.NoError(t, err)
	insertSuccessfulProbe(t, pool, embedding, "legacy-cutover-embeddings-"+fixture.ModelName)
	return integrationfixture.AuthorSealedDeploymentRelease(t, pool, manager, integrationfixture.AtomicRelease{
		PublicModelID: fixture.PublicModelID,
		CustomerOrgID: fixture.OrgID,
		ActorUserID:   fixture.UserID,
		ActorOrgID:    fixture.OrgID,
		Selections: []runtimechange.DeploymentReleaseSelection{{
			DeploymentID: fixture.DeploymentID,
			OperationID:  fixture.OperationID,
		}, {
			DeploymentID: embedding.DeploymentID,
			OperationID:  embedding.OperationID,
		}},
		Reason: reason,
	})
}

func atomicReleaseFixture(
	fixture canonicalDeploymentFixture,
	reason string,
) integrationfixture.AtomicRelease {
	return integrationfixture.AtomicRelease{
		PublicModelID: fixture.PublicModelID,
		CustomerOrgID: fixture.OrgID,
		ActorUserID:   fixture.UserID,
		ActorOrgID:    fixture.OrgID,
		Selections: []runtimechange.DeploymentReleaseSelection{{
			DeploymentID: fixture.DeploymentID,
			OperationID:  fixture.OperationID,
		}},
		Reason: reason,
	}
}

func applyLegacyLifecycle(
	t *testing.T,
	pool *pgxpool.Pool,
	manager *runtimechange.Manager,
	fixture canonicalDeploymentFixture,
	actorUserID int64,
	mutation runtimechange.DeploymentLifecycleMutation,
) (runtimechange.DeploymentLifecycleReceipt, error) {
	t.Helper()
	return manager.ApplyDeploymentLifecycle(
		t.Context(), newLegacyLifecycleCommand(t, pool, fixture, actorUserID, mutation),
	)
}

func (v blockingCutoverVerifier) Verify(
	ctx context.Context,
	tx pgx.Tx,
	_ int64,
	_ int64,
	_ int64,
	_ int64,
) (string, string, error) {
	var isolation string
	if err := tx.QueryRow(ctx, `SHOW transaction_isolation`).Scan(&isolation); err != nil {
		return "", "", err
	}
	if isolation != "repeatable read" && isolation != "serializable" {
		return "", "", fmt.Errorf("cutover verifier received %q isolation", isolation)
	}
	close(v.entered)
	select {
	case <-ctx.Done():
		return "", "", ctx.Err()
	case <-v.release:
	}
	hash := "sha256:" + strings.Repeat("d", 64)
	return hash, hash, nil
}

type canonicalDeploymentFixture struct {
	OrgID              int64
	UserID             int64
	PublicModelID      int64
	ModelName          string
	ChannelID          int64
	ChannelEpoch       int64
	ConnectionID       int64
	ConnectionRevision int64
	DeploymentID       int64
	DeploymentRevision int64
	UpstreamModel      string
	CredentialID       int64
	CredentialRevision int64
	OperationID        string
	OperationRevision  int64
	ProtocolProfileID  string
	ProtocolVersion    string
}

type activeV2ReconcileSnapshot struct {
	Connection string
	Deployment string
	Credential string
	LinkCount  int
	Runtime    string
	Sequences  string
}

func activeV2ReconcileState(
	t *testing.T,
	pool *pgxpool.Pool,
	fixture canonicalDeploymentFixture,
) activeV2ReconcileSnapshot {
	t.Helper()
	ctx := context.Background()
	var snapshot activeV2ReconcileSnapshot
	require.NoError(t, pool.QueryRow(ctx, `
SELECT ROW(connection.name,connection.provider,connection.base_url,
           connection.adapter_type,connection.adapter_config::text,
           connection.admin_state,connection.config_revision,connection.updated_at)::text,
       ROW(deployment.upstream_model,deployment.admin_state,
           deployment.config_revision,deployment.priority,deployment.routing_weight,
           deployment.max_context,deployment.default_max_output,
           deployment.circuit_state,deployment.circuit_revision,deployment.fail_count,
           deployment.half_open_until,deployment.half_open_probe_until,
           deployment.half_open_probe_token,deployment.updated_at)::text,
       ROW(binding.status,binding.key_enc='',binding.retired_at IS NOT NULL,
           binding.state_revision,binding.fail_count,binding.last_used_at,
           binding.cooldown_until,binding.updated_at)::text,
       (SELECT COUNT(*) FROM credential_binding_deployments link
         WHERE link.binding_id=binding.id)
  FROM provider_connections connection
  JOIN model_deployments deployment ON deployment.connection_id=connection.id
  JOIN provider_credential_bindings binding ON binding.connection_id=connection.id
 WHERE connection.id=$1 AND deployment.id=$2 AND binding.id=$3`,
		fixture.ConnectionID, fixture.DeploymentID, fixture.CredentialID,
	).Scan(&snapshot.Connection, &snapshot.Deployment, &snapshot.Credential, &snapshot.LinkCount))
	require.NoError(t, pool.QueryRow(ctx, `
SELECT CONCAT_WS('|',
       (SELECT COALESCE(STRING_AGG(
           release.id::text || ':' || release.stage || ':' || release.version::text || ':' ||
           COALESCE(release.route_items_hash,''),',' ORDER BY release.id),'')
          FROM runtime_change_releases release),
       (SELECT COALESCE(STRING_AGG(
           slot.model_name || ':' || COALESCE(slot.stable_release_id::text,'') || ':' ||
           COALESCE(slot.canary_release_id::text,'') || ':' || slot.slot_version::text,
           ',' ORDER BY slot.model_name),'')
          FROM runtime_change_slots slot))`).Scan(&snapshot.Runtime))
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COALESCE(STRING_AGG(
           sequencename || ':' || COALESCE(last_value::text,'NULL'),
           ',' ORDER BY sequencename),'')
  FROM pg_sequences
 WHERE schemaname='public'`).Scan(&snapshot.Sequences))
	return snapshot
}

func TestDeploymentLifecycleReceiptLegacyCutoverIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	fixture := createCanonicalDeploymentFixture(t, pool, "sealed", "active", 100, 100)
	recordedPool := &recordingBeginTxPool{Pool: pool}
	planner, err := modelrouting.NewPlanner(recordedPool, routingpolicy.Default())
	require.NoError(t, err)
	manager, err := runtimechange.NewManagerWithLegacyCutoverVerifier(
		recordedPool, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default(), planner,
	)
	require.NoError(t, err)
	unverifiedManager, err := runtimechange.NewManager(
		recordedPool, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default(),
	)
	require.NoError(t, err)
	legacyReleaseID := insertLegacyStableRelease(t, pool, fixture)
	var independentPublicModelID, independentDeploymentID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_registry (
  org_id,model_name,display_name,provider_family,lifecycle,approved_catalog,
  context_length,output_limit)
VALUES ($1,$2,$2,'custom','ga',TRUE,128000,8192)
RETURNING id`, fixture.OrgID, fixture.ModelName+"-independent").Scan(&independentPublicModelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployments (
  customer_org_id,public_model_id,connection_id,upstream_model,admin_state,
  config_revision,priority,routing_weight,max_context,default_max_output,
  degradation_threshold)
VALUES ($1,$2,$3,$4,'enabled',1,50,50,128000,4096,10)
RETURNING id`, fixture.OrgID, independentPublicModelID, fixture.ConnectionID,
		fixture.UpstreamModel+"-independent").Scan(&independentDeploymentID))
	_, err = pool.Exec(ctx, `
INSERT INTO credential_binding_deployments (binding_id,deployment_id,connection_id)
VALUES ($1,$2,$3)`, fixture.CredentialID, independentDeploymentID, fixture.ConnectionID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_operations (
  deployment_id,operation_id,support_state,evidence_state,evidence_ref,verified_at,
  revision,protocol_profile_id,protocol_profile_version)
VALUES ($1,'openai.chat_completions.v1','enabled','live_probed','independent-option',NOW(),
        1,$2,$3)`, independentDeploymentID, fixture.ProtocolProfileID, fixture.ProtocolVersion)
	require.NoError(t, err)
	presealOptions, err := manager.ListDeploymentReleaseAuthoringOptions(ctx, fixture.OrgID)
	require.NoError(t, err, "one legal unsealed v1 model must not fail the whole collection")
	var presealLegacy, independentOption *runtimechange.DeploymentReleaseAuthoringOption
	for index := range presealOptions.Models {
		switch presealOptions.Models[index].PublicModel.ID {
		case fixture.PublicModelID:
			presealLegacy = &presealOptions.Models[index]
		case independentPublicModelID:
			independentOption = &presealOptions.Models[index]
		}
	}
	require.NotNil(t, presealLegacy)
	require.False(t, presealLegacy.StructurallyAvailable)
	require.Equal(t, "runtime_stable_release_not_authorable", presealLegacy.ReasonCode)
	require.Nil(t, presealLegacy.StableRelease)
	require.Nil(t, presealLegacy.Slot.StableReleaseID)
	require.NotNil(t, independentOption)
	require.True(t, independentOption.StructurallyAvailable)
	require.NotNil(t, independentOption.Baseline)
	require.Equal(t, runtimechange.DeploymentReleaseBaselineInitial, independentOption.Baseline.Kind)
	require.NotEmpty(t, independentOption.SelectionOptions)
	retiredDeploymentID := insertRetiredLegacyDeployment(t, pool, fixture, "sealed-old-mapping")
	foreignUserID := insertForeignDeploymentActor(t, pool, fixture.OrgID, "sealed-cutover")

	_, err = manager.ResolveDeployment(ctx, fixture.OrgID, fixture.ModelName, "before-cutover")
	require.ErrorIs(t, err, runtimechange.ErrUnavailable)
	legacyAssignment, err := manager.Resolve(ctx, fixture.ModelName, "before-cutover")
	require.NoError(t, err)
	require.NotEmpty(t, legacyAssignment.Overrides)
	legacySeal, err := manager.PrepareLegacyCompatibilitySeal(
		ctx, legacyReleaseRef(fixture), 1, fixture.OrgID, fixture.PublicModelID,
	)
	require.NoError(t, err)
	require.Equal(t, legacyReleaseID, legacySeal.ReleaseID)
	authoringOptions, err := manager.ListDeploymentReleaseAuthoringOptions(ctx, fixture.OrgID)
	require.NoError(t, err)
	var legacyAuthoringOption *runtimechange.DeploymentReleaseAuthoringOption
	for index := range authoringOptions.Models {
		if authoringOptions.Models[index].PublicModel.ID == fixture.PublicModelID {
			legacyAuthoringOption = &authoringOptions.Models[index]
			break
		}
	}
	require.NotNil(t, legacyAuthoringOption)
	require.True(t, legacyAuthoringOption.StructurallyAvailable)
	require.Equal(t, "ready_for_authoring", legacyAuthoringOption.ReasonCode)
	require.NotNil(t, legacyAuthoringOption.Baseline)
	require.Equal(t, runtimechange.DeploymentReleaseBaselineLegacyCutover, legacyAuthoringOption.Baseline.Kind)
	require.Equal(t, int64(1), legacyAuthoringOption.Baseline.ExpectedSlotVersion)
	require.Equal(t, legacyReleaseRef(fixture), legacyAuthoringOption.Baseline.StableReleaseRef)
	require.NotNil(t, legacyAuthoringOption.StableRelease)
	require.Equal(t, runtimechange.CandidateVersion, legacyAuthoringOption.StableRelease.CandidateVersion)
	require.Empty(t, legacyAuthoringOption.StableRelease.ExecutionProfileID)
	require.Empty(t, legacyAuthoringOption.StableRelease.ExecutionProfileVersion)
	require.Zero(t, legacyAuthoringOption.StableRelease.PublicContextLimit)
	require.Zero(t, legacyAuthoringOption.StableRelease.PublicOutputLimit)
	require.Nil(t, legacyAuthoringOption.Slot.AssignedPublicModelID)
	var foreignOrgID, foreignPublicModelID int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT org_id FROM users WHERE id=$1`, foreignUserID).Scan(&foreignOrgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_registry (org_id,model_name,display_name,provider_family,lifecycle)
VALUES ($1,$2,$2,'custom','ga') RETURNING id`, foreignOrgID, fixture.ModelName).Scan(&foreignPublicModelID))
	foreignOptions, err := manager.ListDeploymentReleaseAuthoringOptions(ctx, foreignOrgID)
	require.NoError(t, err)
	require.Len(t, foreignOptions.Models, 1)
	foreignOption := foreignOptions.Models[0]
	require.Equal(t, foreignPublicModelID, foreignOption.PublicModel.ID)
	require.False(t, foreignOption.StructurallyAvailable)
	require.Equal(t, "runtime_slot_identity_mismatch", foreignOption.ReasonCode)
	require.Nil(t, foreignOption.Baseline)
	require.Nil(t, foreignOption.StableRelease, "foreign release summary must never cross the tenant read boundary")
	require.Nil(t, foreignOption.Slot.AssignedPublicModelID)
	require.Nil(t, foreignOption.Slot.StableReleaseID)
	require.Nil(t, foreignOption.Slot.CanaryReleaseID)
	var sealedDeploymentID int64
	require.NoError(t, pool.QueryRow(ctx, `
SELECT deployment_id
  FROM runtime_route_revision_items
 WHERE release_id=$1 AND ordinal=1`, legacyReleaseID).Scan(&sealedDeploymentID))
	require.Equal(t, fixture.DeploymentID, sealedDeploymentID)
	require.NotEqual(t, retiredDeploymentID, sealedDeploymentID)
	_, err = pool.Exec(ctx, `UPDATE runtime_change_releases SET candidate=candidate || '{"tampered":true}'::jsonb WHERE id=$1`, legacyReleaseID)
	require.ErrorContains(t, err, "immutable")
	incomplete := integrationfixture.AuthorSealedDeploymentRelease(t, pool, manager, integrationfixture.AtomicRelease{
		PublicModelID: fixture.PublicModelID,
		CustomerOrgID: fixture.OrgID,
		ActorUserID:   fixture.UserID,
		ActorOrgID:    fixture.OrgID,
		Selections: []runtimechange.DeploymentReleaseSelection{{
			DeploymentID: fixture.DeploymentID,
			OperationID:  fixture.OperationID,
		}},
		Reason: "a chat-only target must not replace the v1 embeddings surface",
	})
	_, err = applyLegacyLifecycle(t, pool, manager, fixture, fixture.UserID,
		runtimechange.DeploymentLifecycleMutation{
			Action:                 runtimechange.DeploymentLifecycleCutoverLegacy,
			ReleaseRef:             incomplete.ReleaseRef,
			ExpectedReleaseVersion: incomplete.Version,
			ExpectedSlotVersion:    1,
			Reason:                 "reject an incomplete target operation set",
		},
	)
	require.ErrorIs(t, err, runtimechange.ErrConflict)
	assertDeploymentReleaseState(t, pool, incomplete.ID, "validated", incomplete.Version, 2)
	var stillLegacy bool
	require.NoError(t, pool.QueryRow(ctx, `
SELECT public_model_id IS NULL AND stable_release_id=$2
  FROM runtime_change_slots WHERE model_name=$1`,
		fixture.ModelName, legacyReleaseID).Scan(&stillLegacy))
	require.True(t, stillLegacy, "operation-set drift must leave the legacy slot untouched")

	sealed := authorLegacyCutoverRelease(
		t, pool, manager, fixture, "author an equivalent canonical deployment route",
	)
	inspection, err := manager.InspectDeploymentLifecycle(ctx, sealed.ReleaseRef, fixture.OrgID)
	require.NoError(t, err)
	require.True(t, inspection.Slot.Exists)
	require.Nil(t, inspection.Slot.AssignedPublicModelID)
	require.Equal(t, legacyReleaseID, *inspection.Slot.StableReleaseID)
	require.NotNil(t, inspection.StableParent)
	require.Equal(t, runtimechange.CandidateVersion, inspection.StableParent.CandidateVersion)
	require.Equal(t, legacyReleaseID, inspection.StableParent.ID)
	require.Equal(t, runtimechange.DeploymentCandidateVersion, sealed.CandidateVersion)
	require.Equal(t, legacyReleaseID, *sealed.ParentStableReleaseID)
	require.NotNil(t, sealed.RouteItemsSealedAt)
	var candidateCount, itemCount int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT jsonb_array_length(candidate->'deployment_routes'),
       (SELECT COUNT(*) FROM runtime_route_revision_items WHERE release_id=$1)
  FROM runtime_change_releases WHERE id=$1`, sealed.ID).Scan(&candidateCount, &itemCount))
	require.Equal(t, 2, candidateCount)
	require.Equal(t, 2, itemCount)

	_, err = pool.Exec(ctx, `
UPDATE runtime_change_slots
   SET stable_release_id=$1,public_model_id=$2
 WHERE model_name=$3`, sealed.ID, fixture.PublicModelID, fixture.ModelName)
	require.ErrorContains(t, err, "stable slot requires an active release")
	_, err = pool.Exec(ctx, `
UPDATE runtime_change_slots
   SET stable_release_id=NULL,canary_release_id=$1,canary_basis_points=100,public_model_id=$2
 WHERE model_name=$3`, sealed.ID, fixture.PublicModelID, fixture.ModelName)
	require.ErrorContains(t, err, "canary slot requires a canary release")

	cutover := runtimechange.DeploymentLifecycleMutation{
		Action:                 runtimechange.DeploymentLifecycleCutoverLegacy,
		ReleaseRef:             sealed.ReleaseRef,
		ExpectedReleaseVersion: sealed.Version,
		ExpectedSlotVersion:    1,
		Reason:                 "move legacy traffic to the atomically authored release",
	}
	_, err = applyLegacyLifecycle(t, pool, unverifiedManager, fixture, fixture.UserID, cutover)
	require.ErrorIs(t, err, runtimechange.ErrUnavailable)
	_, err = applyLegacyLifecycle(t, pool, manager, fixture, foreignUserID, cutover)
	require.ErrorIs(t, err, runtimechange.ErrConflict)
	assertDeploymentReleaseState(t, pool, sealed.ID, "validated", sealed.Version, 2)
	_, err = pool.Exec(ctx, `UPDATE model_registry SET lifecycle='retired' WHERE id=$1`, fixture.PublicModelID)
	require.NoError(t, err)
	_, err = applyLegacyLifecycle(t, pool, manager, fixture, fixture.UserID, cutover)
	require.ErrorIs(t, err, runtimechange.ErrConflict)
	assertDeploymentReleaseState(t, pool, sealed.ID, "validated", sealed.Version, 2)
	_, err = pool.Exec(ctx, `UPDATE model_registry SET lifecycle='ga' WHERE id=$1`, fixture.PublicModelID)
	require.NoError(t, err)

	ambiguousPool := &commitAmbiguityPool{Pool: pool, failNextCommit: true}
	ambiguousPlanner, err := modelrouting.NewPlanner(ambiguousPool, routingpolicy.Default())
	require.NoError(t, err)
	ambiguousManager, err := runtimechange.NewManagerWithLegacyCutoverVerifier(
		ambiguousPool, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default(), ambiguousPlanner,
	)
	require.NoError(t, err)
	cutoverCommand := newLegacyLifecycleCommand(t, pool, fixture, fixture.UserID, cutover)
	cutoverReceipt, err := ambiguousManager.ApplyDeploymentLifecycle(ctx, cutoverCommand)
	require.NoError(t, err)
	require.False(t, cutoverReceipt.IdempotencyReplay,
		"same server operation commit-ACK recovery remains the original result")
	laterReplay := cutoverCommand
	laterReplay.ServerOperationID = cutoverCommand.ServerOperationID + "-later"
	laterReplay.Audit.RequestID = laterReplay.ServerOperationID
	replayedCutover, err := manager.ApplyDeploymentLifecycle(ctx, laterReplay)
	require.NoError(t, err)
	require.True(t, replayedCutover.IdempotencyReplay)
	require.Equal(t, cutoverReceipt.ReceiptID, replayedCutover.ReceiptID)
	require.Equal(t, cutoverReceipt.Slot, replayedCutover.Slot)
	require.Equal(t, cutoverReceipt.Releases, replayedCutover.Releases)
	require.NotEmpty(t, cutoverReceipt.LegacyPlanHash)
	require.Equal(t, cutoverReceipt.LegacyPlanHash, cutoverReceipt.DeploymentPlanHash)
	require.Nil(t, cutoverReceipt.Slot.BeforePublicModelID)
	require.Equal(t, fixture.PublicModelID, *cutoverReceipt.Slot.AfterPublicModelID)
	require.Len(t, cutoverReceipt.Releases, 2)
	recordedPool.assertLatestRepeatableRead(t)
	activated, err := manager.GetDeploymentRelease(ctx, sealed.ReleaseRef)
	require.NoError(t, err)
	require.Equal(t, "active", activated.Stage)

	var slotPublicModelID, stableReleaseID int64
	var parentStage string
	require.NoError(t, pool.QueryRow(ctx, `
SELECT public_model_id,stable_release_id,
       (SELECT stage FROM runtime_change_releases WHERE id=$2)
  FROM runtime_change_slots WHERE model_name=$1`, fixture.ModelName, legacyReleaseID).Scan(
		&slotPublicModelID, &stableReleaseID, &parentStage,
	))
	require.Equal(t, fixture.PublicModelID, slotPublicModelID)
	require.Equal(t, activated.ID, stableReleaseID)
	require.Equal(t, "rolled_back", parentStage)

	assignment, err := manager.ResolveDeployment(ctx, fixture.OrgID, fixture.ModelName, "after-cutover")
	require.NoError(t, err)
	require.Equal(t, runtimechange.DeploymentAssignmentVersion, assignment.ContractVersion)
	require.Equal(t, activated.ReleaseRef, assignment.ReleaseRef)
	require.Equal(t, activated.RouteItemsHash, assignment.RouteItemsHash)
	require.Len(t, assignment.Routes, 2)
	operations := map[string]bool{}
	for _, route := range assignment.Routes {
		require.Equal(t, fixture.DeploymentID, route.DeploymentID)
		operations[route.ModelOperationID] = true
	}
	require.Equal(t, map[string]bool{
		"openai.chat_completions.v1": true,
		"openai.embeddings.v1":       true,
	}, operations)
	_, err = pool.Exec(ctx, `
UPDATE provider_credential_bindings
   SET status='disabled',state_revision=state_revision+1
 WHERE id=$1`, fixture.CredentialID)
	require.NoError(t, err, "canonical route failure must not block emergency rollback")
	fixture.CredentialRevision++

	rollback := runtimechange.DeploymentLifecycleMutation{
		Action:                 runtimechange.DeploymentLifecycleRollbackLegacy,
		ReleaseRef:             activated.ReleaseRef,
		ExpectedReleaseVersion: activated.Version,
		ExpectedSlotVersion:    cutoverReceipt.Slot.AfterVersion,
		Reason:                 "restore the executable legacy authority",
	}
	_, err = applyLegacyLifecycle(t, pool, manager, fixture, foreignUserID, rollback)
	require.ErrorIs(t, err, runtimechange.ErrConflict)
	assertDeploymentReleaseState(t, pool, activated.ID, "active", activated.Version, 3)

	rollbackReceipt, err := applyLegacyLifecycle(t, pool, manager, fixture, fixture.UserID, rollback)
	require.NoError(t, err)
	require.NotEmpty(t, rollbackReceipt.LegacyPlanHash)
	require.Empty(t, rollbackReceipt.DeploymentPlanHash)
	require.Equal(t, fixture.PublicModelID, *rollbackReceipt.Slot.BeforePublicModelID)
	require.Nil(t, rollbackReceipt.Slot.AfterPublicModelID)
	recordedPool.assertLatestRepeatableRead(t)
	rolledBack, err := manager.GetDeploymentRelease(ctx, activated.ReleaseRef)
	require.NoError(t, err)
	require.Equal(t, "validated", rolledBack.Stage)
	_, err = manager.ResolveDeployment(ctx, fixture.OrgID, fixture.ModelName, "after-rollback")
	require.ErrorIs(t, err, runtimechange.ErrUnavailable)
	legacyAssignment, err = manager.Resolve(ctx, fixture.ModelName, "after-rollback")
	require.NoError(t, err)
	require.NotEmpty(t, legacyAssignment.Overrides)
	var legacySlot bool
	require.NoError(t, pool.QueryRow(ctx, `
SELECT public_model_id IS NULL,stable_release_id,
       (SELECT stage FROM runtime_change_releases WHERE id=$2)
  FROM runtime_change_slots WHERE model_name=$1`, fixture.ModelName, legacyReleaseID).Scan(
		&legacySlot, &stableReleaseID, &parentStage,
	))
	require.True(t, legacySlot)
	require.Equal(t, legacyReleaseID, stableReleaseID)
	require.Equal(t, "active", parentStage)

	var recoveredCredentialRevision int64
	require.NoError(t, pool.QueryRow(ctx, `
UPDATE provider_credential_bindings
   SET status='active',state_revision=state_revision+1
 WHERE id=$1
 RETURNING state_revision`, fixture.CredentialID).Scan(&recoveredCredentialRevision))
	_, err = pool.Exec(ctx, `
UPDATE channel_keys SET state_epoch=$2 WHERE channel_id=$1 AND status='active'`,
		fixture.ChannelID, recoveredCredentialRevision)
	require.NoError(t, err)
	fixture.CredentialRevision = recoveredCredentialRevision
	insertSuccessfulProbe(t, pool, fixture, "credential-recovered-after-rollback")
	embedding := fixture
	embedding.OperationID = "openai.embeddings.v1"
	embedding.OperationRevision = 1
	embedding.ProtocolProfileID = "integration.openai.embeddings.v1"
	embedding.ProtocolVersion = "1.0.0"
	insertSuccessfulProbe(t, pool, embedding, "embeddings-credential-recovered-after-rollback")
	recutover := cutover
	recutover.ExpectedReleaseVersion = rolledBack.Version
	recutover.ExpectedSlotVersion = rollbackReceipt.Slot.AfterVersion
	recutover.Reason = "reverify parity after recovery"
	recutoverReceipt, err := applyLegacyLifecycle(t, pool, manager, fixture, fixture.UserID, recutover)
	require.NoError(t, err)
	require.Equal(t, recutoverReceipt.LegacyPlanHash, recutoverReceipt.DeploymentPlanHash)
	activated, err = manager.GetDeploymentRelease(ctx, rolledBack.ReleaseRef)
	require.NoError(t, err)
	require.Equal(t, "active", activated.Stage)

	_, err = pool.Exec(ctx, `UPDATE runtime_route_revision_items SET weight=101 WHERE release_id=$1`, activated.ID)
	require.ErrorContains(t, err, "qualification evidence is immutable")
	_, err = pool.Exec(ctx, `DELETE FROM runtime_route_revision_items WHERE release_id=$1`, activated.ID)
	require.ErrorContains(t, err, "qualification evidence is immutable")
	_, err = pool.Exec(ctx, `
INSERT INTO runtime_route_revision_items (
    release_id,ordinal,deployment_id,operation_id,expected_connection_revision,
    expected_deployment_revision,expected_operation_revision,
    protocol_profile_id,protocol_profile_version,admission,source_candidate_version)
VALUES ($1,2,$2,$3,$4,$5,$6,$7,$8,'allow',$9)`, activated.ID, fixture.DeploymentID,
		fixture.OperationID, fixture.ConnectionRevision, fixture.DeploymentRevision, fixture.OperationRevision,
		fixture.ProtocolProfileID, fixture.ProtocolVersion,
		runtimechange.DeploymentCandidateVersion)
	require.ErrorContains(t, err, "append-only")
	_, err = pool.Exec(ctx, `UPDATE runtime_change_releases SET route_items_hash=$2 WHERE id=$1`,
		activated.ID, "sha256:"+strings.Repeat("f", 64))
	require.ErrorContains(t, err, "immutable")
	_, err = pool.Exec(ctx, `UPDATE channels SET status='disabled',routing_epoch=routing_epoch+1 WHERE id=$1`, fixture.ChannelID)
	require.NoError(t, err)
	staleRollback := rollback
	staleRollback.ExpectedReleaseVersion = activated.Version
	staleRollback.ExpectedSlotVersion = recutoverReceipt.Slot.AfterVersion
	staleRollback.Reason = "stale legacy authority must block rollback"
	_, err = applyLegacyLifecycle(t, pool, manager, fixture, fixture.UserID, staleRollback)
	require.ErrorIs(t, err, runtimechange.ErrConflict)

	var legacyReceipts, legacyAuditRows, receiptEvents int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_deployment_lifecycle_receipts
 WHERE customer_org_id=$1 AND action IN ('cutover_legacy','rollback_legacy')
   AND status='completed'`, fixture.OrgID).Scan(&legacyReceipts))
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM audit_logs
 WHERE org_id=$1 AND action IN (
   'model_deployment.lifecycle.cutover_legacy',
   'model_deployment.lifecycle.rollback_legacy')`, fixture.OrgID).Scan(&legacyAuditRows))
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM runtime_change_events
 WHERE detail ? 'lifecycle_receipt_id' AND detail->>'legacy_bridge'='true'`).Scan(&receiptEvents))
	require.Equal(t, 3, legacyReceipts)
	require.Equal(t, legacyReceipts, legacyAuditRows)
	require.Equal(t, 2*legacyReceipts, receiptEvents)

	var releaseCountBeforeDown, eventCountBeforeDown int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM runtime_change_releases`).Scan(&releaseCountBeforeDown))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM runtime_change_events`).Scan(&eventCountBeforeDown))
	down, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t), "000078_atomic_model_release_authoring.down.sql",
	))
	require.NoError(t, err)
	downgradeCanonicalConnectionLicenseAuthorityForTest(t, pool)
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "durable legacy lifecycle evidence exists")
	var migrationPresent, authoringColumnPresent bool
	var releaseCountAfterDown, eventCountAfterDown, receiptCountAfterDown int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT
  EXISTS(SELECT 1 FROM schema_migrations
          WHERE version='000078_atomic_model_release_authoring'),
  EXISTS(SELECT 1 FROM information_schema.columns
          WHERE table_schema='public' AND table_name='runtime_change_releases'
            AND column_name='authoring_receipt_id'),
  (SELECT COUNT(*) FROM runtime_change_releases),
  (SELECT COUNT(*) FROM runtime_change_events),
  (SELECT COUNT(*) FROM model_deployment_lifecycle_receipts
    WHERE action IN ('cutover_legacy','rollback_legacy'))`).Scan(
		&migrationPresent, &authoringColumnPresent, &releaseCountAfterDown,
		&eventCountAfterDown, &receiptCountAfterDown,
	))
	require.True(t, migrationPresent)
	require.True(t, authoringColumnPresent)
	require.Equal(t, releaseCountBeforeDown, releaseCountAfterDown)
	require.Equal(t, eventCountBeforeDown, eventCountAfterDown)
	require.Equal(t, legacyReceipts, receiptCountAfterDown)
}

func downgradeCanonicalConnectionLicenseAuthorityForTest(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := t.Context()
	for _, name := range []string{
		"000096_kimi_k26_stream_probe_contract_v2.down.sql",
		"000095_kimi_k26_chat_core_qualification.down.sql",
		"000094_deepseek_v4_pro_chat_core_qualification.down.sql",
		"000093_deepseek_function_tool_probe_contract_v2.down.sql",
		"000092_deepseek_reasoning_tool_choice_contract.down.sql",
		"000091_deepseek_chat_tools_json_capabilities.down.sql",
		"000090_deepseek_chat_reasoning_capabilities.down.sql",
		"000089_deepseek_chat_sse_usage_contract_v2.down.sql",
		"000088_model_capability_execution_qualification.down.sql",
		"000087_anthropic_messages_tools_public_binding.down.sql",
		"000086_exact_public_execution_bindings.down.sql",
		"000085_model_execution_plan_freeze_fence.down.sql",
	} {
		down, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), name))
		require.NoError(t, err)
		_, err = pool.Exec(ctx, string(down))
		require.NoError(t, err, name)
	}
	currencyDraftDown, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t), "000084_official_rate_source_currency_drafts.down.sql",
	))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(currencyDraftDown))
	require.NoError(t, err)
	unitMeterDown, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t), "000083_versioned_unit_meter_billing.down.sql",
	))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(unitMeterDown))
	require.NoError(t, err)
	sealedContractDown, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t), "000082_sealed_model_execution_contracts.down.sql",
	))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(sealedContractDown))
	require.NoError(t, err)
	profileDown, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t), "000081_versioned_model_execution_profiles.down.sql",
	))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(profileDown))
	require.NoError(t, err)
	observationDown, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t), "000080_observation_provider_cost_reconciliation.down.sql",
	))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(observationDown))
	require.NoError(t, err)
	// These legacy-bridge fixtures intentionally use distinct display names for
	// their two projections. Normalize only the legacy source immediately before
	// the M79 down proof; production rollback never performs reconciliation.
	_, err = pool.Exec(ctx, `
UPDATE channels legacy
	SET status='active',
		name=connection.name,
       provider=connection.provider,
       base_url=connection.base_url,
       adapter_type=connection.adapter_type,
       adapter_config=connection.adapter_config,
       routing_epoch=connection.config_revision
  FROM provider_connections connection
 WHERE connection.admin_state='enabled'
   AND connection.legacy_channel_id=legacy.id`)
	require.NoError(t, err)
	down, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t), "000079_canonical_connection_license_authority.down.sql",
	))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.NoError(t, err)
}
func TestReconcileLegacyProjectionRejectsActiveV2RuntimeAuthorityWithoutWrites(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	fixture := createCanonicalDeploymentFixture(t, pool, "active-v2-reconcile", "active", 100, 100)
	_, err := pool.Exec(ctx, `UPDATE orgs SET org_type='customer' WHERE id=$1`, fixture.OrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
UPDATE channels
   SET model_mapping=jsonb_build_object($2::text,$3::text)
 WHERE id=$1`, fixture.ChannelID, fixture.ModelName, fixture.UpstreamModel)
	require.NoError(t, err)
	planner, err := modelrouting.NewPlanner(pool, routingpolicy.Default())
	require.NoError(t, err)
	manager, err := runtimechange.NewManagerWithLegacyCutoverVerifier(
		pool, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default(), planner,
	)
	require.NoError(t, err)
	legacyReleaseID := insertLegacyStableRelease(t, pool, fixture)
	_, err = manager.PrepareLegacyCompatibilitySeal(
		ctx, legacyReleaseRef(fixture), 1, fixture.OrgID, fixture.PublicModelID,
	)
	require.NoError(t, err)
	sealed := authorLegacyCutoverRelease(
		t, pool, manager, fixture,
		"active v2 runtime health must outlive legacy projection refresh",
	)
	cutoverReceipt, err := applyLegacyLifecycle(
		t, pool, manager, fixture, fixture.UserID,
		runtimechange.DeploymentLifecycleMutation{
			Action:                 runtimechange.DeploymentLifecycleCutoverLegacy,
			ReleaseRef:             sealed.ReleaseRef,
			ExpectedReleaseVersion: sealed.Version,
			ExpectedSlotVersion:    1,
			Reason:                 "activate canonical authority for reconcile guard",
		},
	)
	require.NoError(t, err)
	require.Equal(t, cutoverReceipt.LegacyPlanHash, cutoverReceipt.DeploymentPlanHash)
	activated, err := manager.GetDeploymentRelease(ctx, sealed.ReleaseRef)
	require.NoError(t, err)

	canonicalHalfOpenUntil := time.Now().UTC().Add(2 * time.Minute).Truncate(time.Microsecond)
	_, err = pool.Exec(ctx, `
UPDATE model_deployments
   SET circuit_state='open',circuit_revision=circuit_revision+1,fail_count=9,
       half_open_until=$2,updated_at=NOW()
 WHERE id=$1`, fixture.DeploymentID, canonicalHalfOpenUntil)
	require.NoError(t, err)
	canonicalLastUsedAt := time.Now().UTC().Truncate(time.Microsecond)
	canonicalCooldownUntil := canonicalLastUsedAt.Add(3 * time.Minute)
	_, err = pool.Exec(ctx, `
UPDATE provider_credential_bindings
   SET state_revision=state_revision+1,fail_count=4,last_used_at=$2,
       cooldown_until=$3,updated_at=NOW()
 WHERE id=$1`, fixture.CredentialID, canonicalLastUsedAt, canonicalCooldownUntil)
	require.NoError(t, err)
	// Canonical safety controls are authoritative after a sealed-v2 cutover.
	// A later explicit reconcile/apply must not infer an implicit resume from
	// the still-active legacy rows or resurrect a terminally scrubbed secret.
	safety, err := modelrouteauthority.New(pool, routingpolicy.Default())
	require.NoError(t, err)
	appendAudit := func(ctx context.Context, tx pgx.Tx, record modelrouteauthority.SafetyAuditRecord) error {
		detail, marshalErr := json.Marshal(record.Detail)
		if marshalErr != nil {
			return marshalErr
		}
		_, insertErr := tx.Exec(ctx, `
INSERT INTO audit_logs (org_id,user_id,action,resource,resource_id,detail)
VALUES ($1,$2,$3,$4,$5,$6::jsonb)`, fixture.OrgID, fixture.UserID,
			record.Action, record.ResourceType, record.ResourceID, string(detail))
		return insertErr
	}
	paused, err := safety.PauseConnection(ctx, modelrouteauthority.PauseConnectionRequest{
		Mode: modelrouting.ModeCanonical, CustomerOrgID: fixture.OrgID,
		ActorUserID: fixture.UserID, ConnectionID: fixture.ConnectionID,
		ExpectedConfigRevision: fixture.ConnectionRevision,
	}, appendAudit)
	require.NoError(t, err)
	require.True(t, paused.Changed)
	revoked, err := safety.RevokeCredential(ctx, modelrouteauthority.RevokeCredentialRequest{
		Mode: modelrouting.ModeCanonical, CustomerOrgID: fixture.OrgID,
		ActorUserID: fixture.UserID, CredentialBindingID: fixture.CredentialID,
		ExpectedStateRevision: fixture.CredentialRevision + 1,
	}, appendAudit)
	require.NoError(t, err)
	require.True(t, revoked.Changed)
	_, err = safety.Apply(ctx, modelrouteauthority.ApplyRequest{
		Mode: modelrouting.ModeCanonical, CustomerOrgID: fixture.OrgID,
	})
	require.ErrorIs(t, err, modelrouteauthority.ErrApplyMode)
	var credentialLinkCountBefore int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM credential_binding_deployments WHERE binding_id=$1`, fixture.CredentialID).Scan(&credentialLinkCountBefore))
	require.Positive(t, credentialLinkCountBefore)

	var itemCountBefore int
	var itemHashBefore string
	var stableBefore, slotVersionBefore int64
	require.NoError(t, pool.QueryRow(ctx, `
SELECT release.route_items_hash,
       (SELECT COUNT(*) FROM runtime_route_revision_items item WHERE item.release_id=release.id),
       slot.stable_release_id,slot.slot_version
  FROM runtime_change_releases release
  JOIN runtime_change_slots slot ON slot.stable_release_id=release.id
 WHERE release.id=$1`, activated.ID).Scan(
		&itemHashBefore, &itemCountBefore, &stableBefore, &slotVersionBefore,
	))
	stateBefore := activeV2ReconcileState(t, pool, fixture)
	_, err = safety.Apply(ctx, modelrouteauthority.ApplyRequest{
		Mode: modelrouting.ModeVerify, CustomerOrgID: fixture.OrgID,
	})
	require.ErrorIs(t, err, modelrouteauthority.ErrApplyPhase)
	require.Equal(t, stateBefore, activeV2ReconcileState(t, pool, fixture),
		"verify apply after v2 assignment must be read-only and fail closed")

	report, err := modeldeployment.ReconcileLegacyProjection(ctx, pool, fixture.OrgID)
	require.ErrorIs(t, err, modeldeployment.ErrCanonicalAuthority)
	require.False(t, report.Ready)
	require.Len(t, report.BlockingIssues, 1)
	require.Equal(t, "canonical_runtime_authority_active", report.BlockingIssues[0].Code)
	require.Equal(t, stateBefore, activeV2ReconcileState(t, pool, fixture),
		"rejected reverse reconciliation must not change tables or sequences")
	require.Zero(t, report.Counts.CurrentRouteOverrides,
		"active v2 authority must be rejected before projection work")
	require.Zero(t, report.Counts.CurrentRouteItems,
		"active v2 authority must be rejected before projection work")

	var circuitState string
	var circuitRevision int64
	var deploymentFailCount int
	var preservedHalfOpenUntil *time.Time
	require.NoError(t, pool.QueryRow(ctx, `
SELECT circuit_state,circuit_revision,fail_count,half_open_until
  FROM model_deployments WHERE id=$1`, fixture.DeploymentID).Scan(
		&circuitState, &circuitRevision, &deploymentFailCount, &preservedHalfOpenUntil,
	))
	require.Equal(t, "open", circuitState)
	require.Equal(t, fixture.ChannelEpoch+1, circuitRevision)
	require.Equal(t, 9, deploymentFailCount)
	require.NotNil(t, preservedHalfOpenUntil)
	require.WithinDuration(t, canonicalHalfOpenUntil, *preservedHalfOpenUntil, time.Microsecond)

	var connectionState, credentialStatus, credentialSecret string
	require.NoError(t, pool.QueryRow(ctx, `
SELECT admin_state FROM provider_connections WHERE id=$1`, fixture.ConnectionID).Scan(&connectionState))
	require.Equal(t, "disabled", connectionState, "sealed-v2 authority must preserve an explicit canonical pause")

	var credentialRevision int64
	var credentialFailCount int
	var preservedLastUsedAt, preservedCooldownUntil *time.Time
	require.NoError(t, pool.QueryRow(ctx, `
SELECT status,key_enc,state_revision,fail_count,last_used_at,cooldown_until
  FROM provider_credential_bindings WHERE id=$1`, fixture.CredentialID).Scan(
		&credentialStatus, &credentialSecret, &credentialRevision, &credentialFailCount,
		&preservedLastUsedAt, &preservedCooldownUntil,
	))
	require.Equal(t, "retired", credentialStatus)
	require.Empty(t, credentialSecret)
	require.Equal(t, fixture.CredentialRevision+2, credentialRevision)
	require.Equal(t, 4, credentialFailCount)
	require.NotNil(t, preservedLastUsedAt)
	require.NotNil(t, preservedCooldownUntil)
	require.WithinDuration(t, canonicalLastUsedAt, *preservedLastUsedAt, time.Microsecond)
	require.WithinDuration(t, canonicalCooldownUntil, *preservedCooldownUntil, time.Microsecond)
	var credentialLinkCountAfter int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM credential_binding_deployments WHERE binding_id=$1`, fixture.CredentialID).Scan(&credentialLinkCountAfter))
	require.Equal(t, credentialLinkCountBefore, credentialLinkCountAfter,
		"terminal credential provenance links must survive reconciliation")

	var itemCountAfter int
	var itemHashAfter string
	var stableAfter, slotVersionAfter int64
	require.NoError(t, pool.QueryRow(ctx, `
SELECT release.route_items_hash,
       (SELECT COUNT(*) FROM runtime_route_revision_items item WHERE item.release_id=release.id),
       slot.stable_release_id,slot.slot_version
  FROM runtime_change_releases release
  JOIN runtime_change_slots slot ON slot.stable_release_id=release.id
 WHERE release.id=$1`, activated.ID).Scan(
		&itemHashAfter, &itemCountAfter, &stableAfter, &slotVersionAfter,
	))
	require.Equal(t, itemHashBefore, itemHashAfter)
	require.Equal(t, itemCountBefore, itemCountAfter)
	require.Equal(t, stableBefore, stableAfter)
	require.Equal(t, slotVersionBefore, slotVersionAfter)
	var parentStage string
	require.NoError(t, pool.QueryRow(ctx, `SELECT stage FROM runtime_change_releases WHERE id=$1`, legacyReleaseID).Scan(&parentStage))
	require.Equal(t, "rolled_back", parentStage)
}

func insertForeignDeploymentActor(t *testing.T, pool *pgxpool.Pool, ownerOrgID int64, suffix string) int64 {
	t.Helper()
	ctx := context.Background()
	var foreignOrgID, foreignUserID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ($1,'active','standard') RETURNING id`, "foreign-runtime-org-"+suffix).Scan(&foreignOrgID))
	require.NotEqual(t, ownerOrgID, foreignOrgID)
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
VALUES ($1,$2,'fixture','org_admin','org_admin','active') RETURNING id`,
		foreignOrgID, "foreign-runtime-user-"+suffix).Scan(&foreignUserID))
	return foreignUserID
}

func assertDeploymentReleaseState(
	t *testing.T,
	pool *pgxpool.Pool,
	releaseID int64,
	expectedStage string,
	expectedVersion, expectedEventCount int64,
) {
	t.Helper()
	ctx := context.Background()
	var stage string
	var version, eventCount int64
	require.NoError(t, pool.QueryRow(ctx, `
SELECT release.stage,release.version,
       (SELECT COUNT(*) FROM runtime_change_events event WHERE event.release_id=release.id)
  FROM runtime_change_releases release
 WHERE release.id=$1`, releaseID).Scan(&stage, &version, &eventCount))
	require.Equal(t, expectedStage, stage)
	require.Equal(t, expectedVersion, version)
	require.Equal(t, expectedEventCount, eventCount)
}

func TestDeploymentLifecycleReceiptLegacyCutoverLocksPinnedAuthorityIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	fixture := createCanonicalDeploymentFixture(t, pool, "cutover-lock", "active", 100, 100)
	verifier := blockingCutoverVerifier{entered: make(chan struct{}), release: make(chan struct{})}
	manager, err := runtimechange.NewManagerWithLegacyCutoverVerifier(
		pool, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default(), verifier,
	)
	require.NoError(t, err)
	insertLegacyStableRelease(t, pool, fixture)
	_, err = manager.PrepareLegacyCompatibilitySeal(
		ctx, legacyReleaseRef(fixture), 1, fixture.OrgID, fixture.PublicModelID,
	)
	require.NoError(t, err)
	sealed := authorLegacyCutoverRelease(
		t, pool, manager, fixture,
		"cutover authority rows must remain stable through parity commit",
	)
	cutoverCommand := newLegacyLifecycleCommand(t, pool, fixture, fixture.UserID,
		runtimechange.DeploymentLifecycleMutation{
			Action:                 runtimechange.DeploymentLifecycleCutoverLegacy,
			ReleaseRef:             sealed.ReleaseRef,
			ExpectedReleaseVersion: sealed.Version,
			ExpectedSlotVersion:    1,
			Reason:                 "hold pinned authority through parity and commit",
		},
	)
	cutoverResult := make(chan error, 1)
	go func() {
		_, cutoverErr := manager.ApplyDeploymentLifecycle(ctx, cutoverCommand)
		cutoverResult <- cutoverErr
	}()
	select {
	case <-verifier.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("cutover did not reach the parity verifier")
	}

	updateResult := make(chan error, 1)
	go func() {
		_, updateErr := pool.Exec(ctx, `
	UPDATE channels
	   SET priority=priority+1,routing_epoch=routing_epoch+1
	 WHERE id=$1`, fixture.ChannelID)
		updateResult <- updateErr
	}()
	select {
	case updateErr := <-updateResult:
		t.Fatalf("pinned deployment changed before cutover commit: %v", updateErr)
	case <-time.After(100 * time.Millisecond):
	}
	type reconcileResult struct {
		report modeldeployment.Report
		err    error
	}
	reconcileResults := make(chan reconcileResult, 1)
	go func() {
		report, reconcileErr := modeldeployment.ReconcileLegacyProjection(ctx, pool, fixture.OrgID)
		reconcileResults <- reconcileResult{report: report, err: reconcileErr}
	}()
	select {
	case result := <-reconcileResults:
		t.Fatalf("legacy reconcile crossed an in-flight v2 cutover: report=%+v err=%v", result.report, result.err)
	case <-time.After(100 * time.Millisecond):
	}
	close(verifier.release)
	select {
	case cutoverErr := <-cutoverResult:
		require.NoError(t, cutoverErr)
	case <-time.After(5 * time.Second):
		t.Fatal("cutover did not commit after parity verifier release")
	}
	select {
	case updateErr := <-updateResult:
		require.NoError(t, updateErr)
	case <-time.After(5 * time.Second):
		t.Fatal("pinned deployment update remained blocked after cutover commit")
	}
	select {
	case result := <-reconcileResults:
		require.ErrorIs(t, result.err, modeldeployment.ErrCanonicalAuthority)
		require.False(t, result.report.Ready)
	case <-time.After(5 * time.Second):
		t.Fatal("legacy reconcile remained blocked after v2 cutover commit")
	}
}

func TestDeploymentManifestAdmissionRejectsDisabledOrStaleEvidence(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	fixture := createCanonicalDeploymentFixture(t, pool, "admission", "staged", 100, 100)
	manager, err := runtimechange.NewManager(pool, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default())
	require.NoError(t, err)

	setLegacyRouteStatus(t, pool, &fixture, "disabled")
	_, err = integrationfixture.AttemptAuthorSealedDeploymentRelease(
		t, pool, manager, atomicReleaseFixture(fixture, "disabled deployment is not admissible"),
	)
	require.ErrorIs(t, err, runtimechange.ErrConflict)

	setLegacyRouteStatus(t, pool, &fixture, "staged")
	insertSuccessfulProbe(t, pool, fixture, "enabled-again-"+fixture.ModelName)
	_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_probes (
    deployment_id,connection_id,operation_id,probe_profile_id,probe_profile_version,
    upstream_model,reported_model,connection_revision,deployment_revision,
    credential_binding_id,credential_state_revision,success,status_code,latency_ms,
    error_summary,source_kind,source_ref,source_fingerprint,tested_by,tested_at)
	VALUES ($1,$2,$3,$4,$5,$6,$6,$7,$8,$9,999,FALSE,401,1,
	        'stale credential','integration.latest',$10,$11,$12,NOW())`, fixture.DeploymentID,
		fixture.ConnectionID, fixture.OperationID, fixture.ProtocolProfileID, fixture.ProtocolVersion,
		fixture.UpstreamModel, fixture.ConnectionRevision, fixture.DeploymentRevision, fixture.CredentialID,
		"latest-failed-"+fixture.ModelName, "sha256:"+strings.Repeat("d", 64), fixture.UserID)
	require.NoError(t, err)
	_, err = integrationfixture.AttemptAuthorSealedDeploymentRelease(
		t, pool, manager, atomicReleaseFixture(fixture, "latest failed probe is not admissible"),
	)
	require.ErrorIs(t, err, runtimechange.ErrConflict)
}

func TestDeploymentManifestAdmissionIsCredentialSpecificAndScopeEpochBound(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	fixture := createCanonicalDeploymentFixture(t, pool, "credential-scope", "staged", 100, 100)
	manager, err := runtimechange.NewManager(pool, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default())
	require.NoError(t, err)

	var secondCredentialID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_credential_bindings (connection_id,key_enc,status,state_revision)
VALUES ($1,'second-fixture','active',1) RETURNING id`, fixture.ConnectionID).Scan(&secondCredentialID))
	_, err = pool.Exec(ctx, `
INSERT INTO credential_binding_deployments (binding_id,deployment_id,connection_id)
VALUES ($1,$2,$3)`, secondCredentialID, fixture.DeploymentID, fixture.ConnectionID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
	DELETE FROM credential_binding_deployments
	 WHERE binding_id=$1 AND deployment_id=$2`, fixture.CredentialID, fixture.DeploymentID)
	require.NoError(t, err)
	_, err = integrationfixture.AttemptAuthorSealedDeploymentRelease(
		t, pool, manager, atomicReleaseFixture(
			fixture, "one credential probe cannot authorize a different binding",
		),
	)
	require.ErrorIs(t, err, runtimechange.ErrConflict)

	insertSuccessfulProbeForCredential(
		t, pool, fixture, secondCredentialID, 1, "second-credential-current", time.Now().UTC().Add(time.Second),
	)
	_, err = integrationfixture.AttemptAuthorSealedDeploymentRelease(
		t, pool, manager, atomicReleaseFixture(
			fixture, "the second binding has its own successful current probe",
		),
	)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `DELETE FROM credential_binding_deployments WHERE binding_id=$1 AND deployment_id=$2`,
		secondCredentialID, fixture.DeploymentID)
	require.NoError(t, err)
	var relinkedAt time.Time
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO credential_binding_deployments (binding_id,deployment_id,connection_id,created_at)
VALUES ($1,$2,$3,NOW()+INTERVAL '1 minute') RETURNING created_at`,
		secondCredentialID, fixture.DeploymentID, fixture.ConnectionID).Scan(&relinkedAt))
	_, err = pool.Exec(ctx, `UPDATE credential_binding_deployments SET created_at=created_at WHERE binding_id=$1 AND deployment_id=$2`,
		secondCredentialID, fixture.DeploymentID)
	require.ErrorContains(t, err, "scope links are immutable")
	_, err = integrationfixture.AttemptAuthorSealedDeploymentRelease(
		t, pool, manager, atomicReleaseFixture(
			fixture, "unlink and relink creates a new credential scope epoch",
		),
	)
	require.ErrorIs(t, err, runtimechange.ErrConflict)

	insertSuccessfulProbeForCredential(
		t, pool, fixture, secondCredentialID, 1, "second-credential-after-relink", relinkedAt.Add(time.Second),
	)
	_, err = integrationfixture.AttemptAuthorSealedDeploymentRelease(
		t, pool, manager, atomicReleaseFixture(
			fixture, "a new probe after relink authorizes the new scope epoch",
		),
	)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
UPDATE provider_credential_bindings
   SET status='disabled',state_revision=state_revision+1
 WHERE id=$1`, secondCredentialID)
	require.NoError(t, err)
	nullFingerprint := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("null-credential-probe")))
	_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_probes (
    deployment_id,connection_id,operation_id,probe_profile_id,probe_profile_version,
    upstream_model,reported_model,connection_revision,deployment_revision,
    credential_binding_id,credential_state_revision,success,status_code,latency_ms,
    error_summary,source_kind,source_ref,source_fingerprint,tested_by,tested_at)
VALUES ($1,$2,$3,$4,$5,$6,$6,$7,$8,NULL,0,TRUE,200,1,'',
        'integration.null-credential','null-credential-probe',$9,$10,NOW()+INTERVAL '3 minutes')`,
		fixture.DeploymentID, fixture.ConnectionID, fixture.OperationID,
		fixture.ProtocolProfileID, fixture.ProtocolVersion, fixture.UpstreamModel,
		fixture.ConnectionRevision, fixture.DeploymentRevision, nullFingerprint, fixture.UserID)
	require.NoError(t, err)
	_, err = integrationfixture.AttemptAuthorSealedDeploymentRelease(
		t, pool, manager, atomicReleaseFixture(
			fixture, "a credential-free probe cannot authorize a keyed route",
		),
	)
	require.ErrorIs(t, err, runtimechange.ErrConflict)
}

func TestDeploymentManifestRevisionGuards(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	fixture := createCanonicalDeploymentFixture(t, pool, "revision-guards", "staged", 100, 100)
	_, err := pool.Exec(ctx, `UPDATE model_deployment_operations SET protocol_profile_version='1.0.1' WHERE deployment_id=$1 AND operation_id=$2`,
		fixture.DeploymentID, fixture.OperationID)
	require.ErrorContains(t, err, "next revision")
	_, err = pool.Exec(ctx, `
UPDATE model_deployment_operations
   SET protocol_profile_version='1.0.1',revision=revision+1
 WHERE deployment_id=$1 AND operation_id=$2`, fixture.DeploymentID, fixture.OperationID)
	require.NoError(t, err)
	fixture.OperationRevision++
	fixture.ProtocolVersion = "1.0.1"
	insertSuccessfulProbe(t, pool, fixture, "operation-profile-1.0.1")

	_, err = pool.Exec(ctx, `UPDATE provider_connections SET base_url='https://changed.invalid' WHERE id=$1`, fixture.ConnectionID)
	require.ErrorContains(t, err, "next config revision")
	_, err = pool.Exec(ctx, `UPDATE provider_connections SET config_revision=config_revision+2 WHERE id=$1`, fixture.ConnectionID)
	require.ErrorContains(t, err, "must match the legacy source epoch")
	_, err = pool.Exec(ctx, `UPDATE model_deployments SET priority=priority+1 WHERE id=$1`, fixture.DeploymentID)
	require.ErrorContains(t, err, "next config revision")
	_, err = pool.Exec(ctx, `UPDATE model_deployments SET fail_count=fail_count+1 WHERE id=$1`, fixture.DeploymentID)
	require.NoError(t, err, "sub-threshold counters are not breaker epoch changes")
	_, err = pool.Exec(ctx, `UPDATE provider_credential_bindings SET key_enc='rotated' WHERE id=$1`, fixture.CredentialID)
	require.ErrorContains(t, err, "next state revision")

	var nativeConnectionID, nativeDeploymentID, nativeCredentialID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_connections (
    name,provider,base_url,adapter_type,adapter_config,admin_state,config_revision)
VALUES ('native-revision-guard','custom','https://native.invalid','openai_compatible','{}','enabled',1)
RETURNING id`).Scan(&nativeConnectionID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployments (
    customer_org_id,public_model_id,connection_id,upstream_model,admin_state,config_revision)
VALUES ($1,$2,$3,'native-upstream','enabled',1)
RETURNING id`, fixture.OrgID, fixture.PublicModelID, nativeConnectionID).Scan(&nativeDeploymentID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_credential_bindings (connection_id,key_enc,status,state_revision)
VALUES ($1,'native-secret','active',1)
RETURNING id`, nativeConnectionID).Scan(&nativeCredentialID))
	_, err = pool.Exec(ctx, `UPDATE provider_connections SET config_revision=config_revision+2 WHERE id=$1`, nativeConnectionID)
	require.ErrorContains(t, err, "must not decrease or skip")
	_, err = pool.Exec(ctx, `
UPDATE model_deployments SET priority=priority+1,config_revision=config_revision+2 WHERE id=$1`, nativeDeploymentID)
	require.ErrorContains(t, err, "must not decrease or skip")
	_, err = pool.Exec(ctx, `
UPDATE provider_credential_bindings SET status='disabled',state_revision=state_revision+2 WHERE id=$1`, nativeCredentialID)
	require.ErrorContains(t, err, "must not decrease or skip")

	var capabilityID string
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployment_capabilities (
    deployment_id,operation_id,capability_id,support_state,evidence_state,evidence_ref,verified_at,revision)
VALUES ($1,$2,'tools','enabled','live_probed','integration capability',NOW(),1)
RETURNING capability_id`, fixture.DeploymentID, fixture.OperationID).Scan(&capabilityID))
	_, err = pool.Exec(ctx, `
UPDATE model_deployment_capabilities SET support_state='unsupported'
 WHERE deployment_id=$1 AND operation_id=$2 AND capability_id=$3`, fixture.DeploymentID, fixture.OperationID, capabilityID)
	require.ErrorContains(t, err, "next revision")
	_, err = pool.Exec(ctx, `
UPDATE model_deployment_capabilities SET support_state='unsupported',revision=revision+1
 WHERE deployment_id=$1 AND operation_id=$2 AND capability_id=$3`, fixture.DeploymentID, fixture.OperationID, capabilityID)
	require.NoError(t, err)
}

func deploymentRoute(fixture canonicalDeploymentFixture) runtimechange.DeploymentRoute {
	return runtimechange.DeploymentRoute{
		DeploymentID:               fixture.DeploymentID,
		ModelOperationID:           fixture.OperationID,
		ExpectedConnectionRevision: fixture.ConnectionRevision,
		ExpectedDeploymentRevision: fixture.DeploymentRevision,
		ExpectedOperationRevision:  fixture.OperationRevision,
		ProtocolProfileID:          fixture.ProtocolProfileID,
		ProtocolProfileVersion:     fixture.ProtocolVersion,
		Admission:                  routingpolicy.AdmissionAllow,
	}
}

func insertRetiredLegacyDeployment(
	t *testing.T,
	pool *pgxpool.Pool,
	fixture canonicalDeploymentFixture,
	suffix string,
) int64 {
	t.Helper()
	ctx := context.Background()
	var deploymentID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployments (
    customer_org_id,public_model_id,connection_id,upstream_model,admin_state,retired_at,
    config_revision,priority,routing_weight,legacy_channel_id,legacy_public_model)
VALUES ($1,$2,$3,$4,'retired',NOW(),$5,100,100,$6,$7)
RETURNING id`, fixture.OrgID, fixture.PublicModelID, fixture.ConnectionID,
		fixture.UpstreamModel+"-"+suffix, fixture.DeploymentRevision,
		fixture.ChannelID, fixture.ModelName).Scan(&deploymentID))
	_, err := pool.Exec(ctx, `
INSERT INTO model_deployment_operations (
    deployment_id,operation_id,support_state,evidence_state,evidence_ref,revision,
    protocol_profile_id,protocol_profile_version)
VALUES ($1,'legacy.any.v1','enabled','declared','retired legacy mapping',1,
        'legacy.channel_model_probe','v1')`, deploymentID)
	require.NoError(t, err)
	return deploymentID
}

func createCanonicalDeploymentFixture(
	t *testing.T,
	pool *pgxpool.Pool,
	suffix, channelStatus string,
	priority, weight int,
) canonicalDeploymentFixture {
	t.Helper()
	return createCanonicalDeploymentFixtureWithProduct(
		t, pool, suffix, channelStatus, priority, weight,
		"custom", "https://provider.invalid", "upstream-v2-model-"+suffix,
	)
}

func createCanonicalDeploymentFixtureWithProduct(
	t *testing.T,
	pool *pgxpool.Pool,
	suffix, channelStatus string,
	priority, weight int,
	provider, baseURL, upstreamModel string,
) canonicalDeploymentFixture {
	t.Helper()
	ctx := context.Background()
	fixture := canonicalDeploymentFixture{
		OrgID:              testutil.NewTestOrg(t, pool),
		ModelName:          "v2-model-" + suffix,
		OperationID:        "openai.chat_completions.v1",
		ConnectionRevision: 1,
		DeploymentRevision: 1,
		OperationRevision:  1,
		CredentialRevision: 1,
		ProtocolProfileID:  "integration.openai.chat.v1",
		ProtocolVersion:    "1.0.0",
	}
	fixture.UpstreamModel = upstreamModel
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
VALUES ($1,$2,'fixture','platform_admin','platform_admin','active') RETURNING id`,
		fixture.OrgID, "v2-user-"+suffix).Scan(&fixture.UserID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_registry (
    org_id,model_name,display_name,provider_family,lifecycle,approved_catalog,
    context_length,output_limit)
VALUES ($1,$2,$2,$3,'ga',TRUE,128000,8192) RETURNING id`, fixture.OrgID, fixture.ModelName, provider).Scan(&fixture.PublicModelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channels (
    name,provider,base_url,models,model_mapping,adapter_type,adapter_config,
    priority,routing_weight,status,circuit_state,routing_epoch)
VALUES ($1,$7,$8,$2,jsonb_build_object($2::text,$3::text),
        'openai_compatible','{}',$4,$5,$6,'healthy',1)
RETURNING id,routing_epoch`, "v2-channel-"+suffix, fixture.ModelName,
		fixture.UpstreamModel, priority, weight, channelStatus, provider, baseURL).Scan(
		&fixture.ChannelID, &fixture.ChannelEpoch,
	))
	var legacyKeyID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channel_keys (channel_id,key_enc,allowed_models,status,state_epoch)
VALUES ($1,'fixture',$2,'active',1) RETURNING id`, fixture.ChannelID, fixture.ModelName).Scan(&legacyKeyID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_connections (
    legacy_channel_id,name,provider,base_url,adapter_type,adapter_config,admin_state,config_revision)
VALUES ($1,$2,$3,$4,'openai_compatible','{}','enabled',1)
RETURNING id`, fixture.ChannelID, "v2-connection-"+suffix, provider, baseURL).Scan(&fixture.ConnectionID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployments (
    customer_org_id,public_model_id,connection_id,upstream_model,admin_state,
    config_revision,priority,routing_weight,max_context,default_max_output,
    degradation_threshold,legacy_channel_id,legacy_public_model)
VALUES ($1,$2,$3,$4,'enabled',1,$5,$6,128000,4096,10,$7,$8) RETURNING id`, fixture.OrgID,
		fixture.PublicModelID, fixture.ConnectionID, fixture.UpstreamModel,
		priority, weight, fixture.ChannelID, fixture.ModelName).Scan(&fixture.DeploymentID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_credential_bindings (
    connection_id,key_enc,status,state_revision,legacy_channel_key_id)
VALUES ($1,'fixture','active',1,$2) RETURNING id`, fixture.ConnectionID, legacyKeyID).Scan(&fixture.CredentialID))
	_, err := pool.Exec(ctx, `
INSERT INTO credential_binding_deployments (binding_id,deployment_id,connection_id)
VALUES ($1,$2,$3)`, fixture.CredentialID, fixture.DeploymentID, fixture.ConnectionID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_operations (
    deployment_id,operation_id,support_state,evidence_state,evidence_ref,verified_at,revision,
    protocol_profile_id,protocol_profile_version)
VALUES ($1,$2,'enabled','live_probed','integration fixture',NOW(),1,$3,$4)`,
		fixture.DeploymentID, fixture.OperationID, fixture.ProtocolProfileID, fixture.ProtocolVersion)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_operations (
    deployment_id,operation_id,support_state,evidence_state,evidence_ref,revision,
    protocol_profile_id,protocol_profile_version)
VALUES ($1,'legacy.any.v1','enabled','declared',$2,1,
        'legacy.channel_model_probe','v1')`, fixture.DeploymentID,
		fmt.Sprintf("legacy:channel:%d", fixture.ChannelID))
	require.NoError(t, err)
	insertSuccessfulProbe(t, pool, fixture, "initial-"+suffix)
	return fixture
}

func createAdditionalDeploymentFixture(
	t *testing.T,
	pool *pgxpool.Pool,
	base canonicalDeploymentFixture,
	suffix, channelStatus string,
	priority, weight int,
) canonicalDeploymentFixture {
	t.Helper()
	ctx := context.Background()
	fixture := canonicalDeploymentFixture{
		OrgID: base.OrgID, UserID: base.UserID, PublicModelID: base.PublicModelID,
		ModelName: base.ModelName, OperationID: base.OperationID,
		ConnectionRevision: 1, DeploymentRevision: 1, OperationRevision: 1,
		CredentialRevision: 1,
		ProtocolProfileID:  base.ProtocolProfileID, ProtocolVersion: base.ProtocolVersion,
	}
	fixture.UpstreamModel = "upstream-" + fixture.ModelName + "-" + suffix
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channels (
    name,provider,base_url,models,model_mapping,adapter_type,adapter_config,
    priority,routing_weight,status,circuit_state,routing_epoch)
VALUES ($1,'custom','https://provider.invalid',$2,jsonb_build_object($2::text,$3::text),
        'openai_compatible','{}',$4,$5,$6,'healthy',1)
RETURNING id,routing_epoch`, "v2-channel-"+suffix, fixture.ModelName,
		fixture.UpstreamModel, priority, weight, channelStatus).Scan(
		&fixture.ChannelID, &fixture.ChannelEpoch,
	))
	var legacyKeyID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channel_keys (channel_id,key_enc,allowed_models,status,state_epoch)
VALUES ($1,'fixture',$2,'active',1) RETURNING id`, fixture.ChannelID, fixture.ModelName).Scan(&legacyKeyID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_connections (
    legacy_channel_id,name,provider,base_url,adapter_type,adapter_config,admin_state,config_revision)
VALUES ($1,$2,'custom','https://provider.invalid','openai_compatible','{}','enabled',1)
RETURNING id`, fixture.ChannelID, "v2-connection-"+suffix).Scan(&fixture.ConnectionID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployments (
    customer_org_id,public_model_id,connection_id,upstream_model,admin_state,
    config_revision,priority,routing_weight,max_context,default_max_output,
    degradation_threshold,legacy_channel_id,legacy_public_model)
VALUES ($1,$2,$3,$4,'enabled',1,$5,$6,128000,4096,10,$7,$8) RETURNING id`, fixture.OrgID,
		fixture.PublicModelID, fixture.ConnectionID, fixture.UpstreamModel,
		priority, weight, fixture.ChannelID, fixture.ModelName).Scan(&fixture.DeploymentID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_credential_bindings (
    connection_id,key_enc,status,state_revision,legacy_channel_key_id)
VALUES ($1,'fixture','active',1,$2) RETURNING id`, fixture.ConnectionID, legacyKeyID).Scan(&fixture.CredentialID))
	_, err := pool.Exec(ctx, `
INSERT INTO credential_binding_deployments (binding_id,deployment_id,connection_id)
VALUES ($1,$2,$3)`, fixture.CredentialID, fixture.DeploymentID, fixture.ConnectionID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_operations (
    deployment_id,operation_id,support_state,evidence_state,evidence_ref,verified_at,revision,
    protocol_profile_id,protocol_profile_version)
VALUES ($1,$2,'enabled','live_probed','integration fixture',NOW(),1,$3,$4)`,
		fixture.DeploymentID, fixture.OperationID, fixture.ProtocolProfileID, fixture.ProtocolVersion)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_operations (
    deployment_id,operation_id,support_state,evidence_state,evidence_ref,revision,
    protocol_profile_id,protocol_profile_version)
VALUES ($1,'legacy.any.v1','enabled','declared',$2,1,
        'legacy.channel_model_probe','v1')`, fixture.DeploymentID,
		fmt.Sprintf("legacy:channel:%d", fixture.ChannelID))
	require.NoError(t, err)
	insertSuccessfulProbe(t, pool, fixture, "initial-"+suffix)
	return fixture
}

func insertSuccessfulProbe(t *testing.T, pool *pgxpool.Pool, fixture canonicalDeploymentFixture, source string) {
	t.Helper()
	insertSuccessfulProbeForCredential(t, pool, fixture, fixture.CredentialID, fixture.CredentialRevision, source, time.Now().UTC())
}

func insertSuccessfulProbeForCredential(
	t *testing.T,
	pool *pgxpool.Pool,
	fixture canonicalDeploymentFixture,
	credentialID, credentialRevision int64,
	source string,
	testedAt time.Time,
) {
	t.Helper()
	ctx := context.Background()
	fingerprint := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(source)))
	_, err := pool.Exec(ctx, `
INSERT INTO model_deployment_probes (
    deployment_id,connection_id,operation_id,probe_profile_id,probe_profile_version,
    upstream_model,reported_model,connection_revision,deployment_revision,
    credential_binding_id,credential_state_revision,success,status_code,latency_ms,
    error_summary,source_kind,source_ref,source_fingerprint,tested_by,tested_at)
VALUES ($1,$2,$3,$4,$5,$6,$6,$7,$8,$9,$10,TRUE,200,2,'',
	        'integration.route',$11,$12,$13,$14)`, fixture.DeploymentID, fixture.ConnectionID,
		fixture.OperationID, fixture.ProtocolProfileID, fixture.ProtocolVersion, fixture.UpstreamModel,
		fixture.ConnectionRevision, fixture.DeploymentRevision, credentialID, credentialRevision,
		source, fingerprint, fixture.UserID, testedAt.UTC().Truncate(time.Microsecond))
	require.NoError(t, err)
}

func replaceCanonicalCredentialWithNative(
	t *testing.T,
	pool *pgxpool.Pool,
	fixture *canonicalDeploymentFixture,
) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
DELETE FROM credential_binding_deployments
 WHERE binding_id=$1 AND deployment_id=$2`, fixture.CredentialID, fixture.DeploymentID)
	require.NoError(t, err)
	var credentialID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_credential_bindings (connection_id,key_enc,status,state_revision)
VALUES ($1,'native-cutover-fixture','active',1)
RETURNING id`, fixture.ConnectionID).Scan(&credentialID))
	var linkedAt time.Time
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO credential_binding_deployments (binding_id,deployment_id,connection_id)
VALUES ($1,$2,$3) RETURNING created_at`, credentialID, fixture.DeploymentID, fixture.ConnectionID).Scan(&linkedAt))
	fixture.CredentialID = credentialID
	fixture.CredentialRevision = 1
	insertSuccessfulProbeForCredential(
		t, pool, *fixture, credentialID, 1, "native-cutover-credential", linkedAt,
	)
}

func setLegacyRouteStatus(
	t *testing.T,
	pool *pgxpool.Pool,
	fixture *canonicalDeploymentFixture,
	status string,
) {
	t.Helper()
	ctx := context.Background()
	var revision int64
	require.NoError(t, pool.QueryRow(ctx, `
UPDATE channels SET status=$2,routing_epoch=routing_epoch+1,updated_at=NOW()
 WHERE id=$1 RETURNING routing_epoch`, fixture.ChannelID, status).Scan(&revision))
	adminState := "enabled"
	if status == "disabled" {
		adminState = "disabled"
	}
	_, err := pool.Exec(ctx, `
UPDATE provider_connections
   SET admin_state=$2,config_revision=$3,updated_at=NOW()
 WHERE id=$1`, fixture.ConnectionID, adminState, revision)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
UPDATE model_deployments
   SET admin_state=$2,config_revision=$3,updated_at=NOW()
 WHERE id=$1`, fixture.DeploymentID, adminState, revision)
	require.NoError(t, err)
	fixture.ConnectionRevision = revision
	fixture.DeploymentRevision = revision
}

func insertLegacyStableRelease(t *testing.T, pool *pgxpool.Pool, fixture canonicalDeploymentFixture) int64 {
	t.Helper()
	ctx := context.Background()
	var releaseID int64
	candidate := runtimechange.Candidate{
		ContractVersion: runtimechange.CandidateVersion,
		ModelName:       fixture.ModelName,
		Overrides: []routingpolicy.ChannelOverride{{
			ChannelID:            fixture.ChannelID,
			ExpectedRoutingEpoch: fixture.ChannelEpoch,
			Admission:            routingpolicy.AdmissionAllow,
		}},
	}
	candidateJSON, err := json.Marshal(candidate)
	require.NoError(t, err)
	candidateHash := fmt.Sprintf("sha256:%x", sha256.Sum256(candidateJSON))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO runtime_change_releases (
    release_ref,model_name,candidate_version,candidate_hash,baseline_hash,
    candidate,provider_pack_pins,simulation_report,overall_risk,stage,version,
    canary_basis_points,reason,created_by,validated_by,activated_by,
    validated_at,activated_at)
VALUES (
    $1,$2,'unirouter.runtime-change-candidate.v1',
    $3,
    'sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
    $4::jsonb,
    '[]','{}','unchanged','active',1,0,'v1 stable cutover fixture',$5,$5,$5,NOW(),NOW())
RETURNING id`, legacyReleaseRef(fixture), fixture.ModelName, candidateHash,
		string(candidateJSON), fixture.UserID).Scan(&releaseID))
	_, err = pool.Exec(ctx, `
INSERT INTO runtime_route_revision_items (
    release_id,ordinal,deployment_id,operation_id,
    expected_connection_revision,expected_deployment_revision,
    admission,source_candidate_version)
VALUES ($1,1,$2,$3,$4,$5,'allow',$6)`, releaseID, fixture.DeploymentID,
		"legacy.any.v1", fixture.ConnectionRevision, fixture.DeploymentRevision,
		runtimechange.CandidateVersion)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO runtime_change_slots (
    model_name,stable_release_id,canary_release_id,canary_basis_points,slot_version)
VALUES ($1,$2,NULL,0,1)`, fixture.ModelName, releaseID)
	require.NoError(t, err)
	return releaseID
}

func legacyReleaseRef(fixture canonicalDeploymentFixture) string {
	return "rcr_v1_" + strings.ReplaceAll(fixture.ModelName, "-", "_")
}
