package modeldeployment

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/modelgov"
	"github.com/uniroute/unirouter/internal/modellist"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

const legacyRuntimeCandidateVersion = "unirouter.runtime-change-candidate.v1"

type legacyChannel struct {
	ID                   int64
	Name                 string
	Provider             string
	BaseURL              string
	Models               string
	ModelMapping         json.RawMessage
	AdapterType          string
	AdapterConfig        json.RawMessage
	MaxContext           int
	DefaultMaxOutput     int
	Priority             int
	Status               string
	CircuitState         string
	HalfOpenUntil        *time.Time
	FailCount            int
	DegradationThreshold int
	RoutingWeight        int
	RoutingEpoch         int64
	CircuitEpoch         int64
	HalfOpenProbeUntil   *time.Time
	HalfOpenProbeToken   string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type deploymentKey struct {
	ChannelID int64
	Model     string
}

type deploymentSourceIdentity struct {
	ChannelID int64
	Model     string
	Upstream  string
}

type deploymentRecord struct {
	ID           int64
	ConnectionID int64
	PublicModel  string
	Upstream     string
}

type credentialBindingRecord struct {
	ID     int64
	Active bool
}

type legacyCredential struct {
	ID            int64
	AllowedModels string
	Status        string
	StateEpoch    int64
	FailCount     int
	LastUsedAt    *time.Time
	CooldownUntil *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
	KeyEnc        string
}

type releaseCandidate struct {
	ContractVersion string                          `json:"contract_version"`
	ModelName       string                          `json:"model_name"`
	Overrides       []routingpolicy.ChannelOverride `json:"overrides"`
}

type legacyProbe struct {
	ChannelID       int64
	PublicModel     string
	Upstream        string
	Reported        string
	RoutingEpoch    int64
	CredentialID    sql.NullInt64
	CredentialState int64
	Success         bool
	StatusCode      int
	LatencyMS       int64
	ErrorSummary    string
	TestedBy        sql.NullInt64
	TestedAt        time.Time
}

type legacyRelease struct {
	ID               int64
	ModelName        string
	CandidateVersion string
	Candidate        []byte
}

// ReconcileLegacyProjection idempotently materializes the additive canonical
// model-deployment schema from legacy channel state for one explicitly chosen
// customer organization. It commits safe mappings even when the completeness
// report is blocked, then returns ErrIncomplete so callers cannot treat a
// partial projection as cutover-ready. Once any v2 release is current it
// returns ErrCanonicalAuthority before writing: canonical state is never
// reconstructed in reverse from legacy tables.
func ReconcileLegacyProjection(ctx context.Context, database db.DBQuerier, customerOrgID int64) (Report, error) {
	report := Report{
		ContractVersion: ReconcileContractVersion,
		CustomerOrgID:   customerOrgID,
		GeneratedAt:     time.Now().UTC(),
		BlockingIssues:  make([]Issue, 0),
		Warnings:        make([]Issue, 0),
		Limitations: []string{
			"legacy_operation_is_unknown_and_represented_only_as_legacy.any.v1",
			"legacy_capabilities_are_unknown_and_no_capability_support_is_inferred",
			"legacy_call_and_provider_attempt_operation_evidence_is_unavailable",
			"runtime_route_revision_items_are_parity_only_and_not_p1_routing_authority",
		},
	}
	if database == nil || customerOrgID <= 0 {
		return report, fmt.Errorf("modeldeployment: database and positive customer org ID are required")
	}

	tx, err := database.Begin(ctx)
	if err != nil {
		return report, fmt.Errorf("modeldeployment: begin reconcile: %w", err)
	}
	defer tx.Rollback(ctx)

	// Legacy projection is a verify-preparation writer, never a reverse
	// canonical writer. Deployment lifecycle transactions take the same table
	// locks in ROW EXCLUSIVE mode before any lower-level row locks. Taking SHARE
	// here first gives one fixed order and prevents a concurrent v2 cutover from
	// appearing after this guard but before projection writes commit.
	for _, table := range []string{"runtime_change_releases", "runtime_change_slots"} {
		if _, err := tx.Exec(ctx, "LOCK TABLE "+table+" IN SHARE MODE"); err != nil {
			return report, fmt.Errorf("modeldeployment: lock runtime assignment authority: %w", err)
		}
	}
	var hasCurrentV2 bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1
      FROM runtime_change_slots slot
     CROSS JOIN LATERAL (
           VALUES (slot.stable_release_id), (slot.canary_release_id)
     ) current_release(release_id)
      JOIN runtime_change_releases release ON release.id=current_release.release_id
     WHERE release.candidate_version='unirouter.runtime-change-candidate.v2'
)`).Scan(&hasCurrentV2); err != nil {
		return report, fmt.Errorf("modeldeployment: inspect current runtime authority: %w", err)
	}
	if hasCurrentV2 {
		addBlocking(&report, "canonical_runtime_authority_active", "runtime_assignment", strconv.FormatInt(customerOrgID, 10),
			"legacy reconciliation is disabled after any v2 release becomes current; use read-only authority check")
		sortIssues(report.BlockingIssues)
		return report, ErrCanonicalAuthority
	}

	// Reconciliation is deployment-global projection work. Hold a table-level
	// share lock while resolving the root set so a concurrent organization
	// insert/update cannot make a previously unambiguous choice stale before
	// this transaction commits canonical rows.
	if _, err := tx.Exec(ctx, `LOCK TABLE orgs IN SHARE MODE`); err != nil {
		return report, fmt.Errorf("modeldeployment: lock customer organization authority: %w", err)
	}
	rows, err := tx.Query(ctx, `
SELECT id
  FROM orgs
 WHERE status='active' AND org_type='customer' AND parent_id IS NULL
 ORDER BY id`)
	if err != nil {
		return report, fmt.Errorf("modeldeployment: resolve active root customer organizations: %w", err)
	}
	rootCustomerIDs := make([]int64, 0, 2)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return report, fmt.Errorf("modeldeployment: scan active root customer organization: %w", err)
		}
		rootCustomerIDs = append(rootCustomerIDs, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return report, fmt.Errorf("modeldeployment: read active root customer organizations: %w", err)
	}
	rows.Close()
	if len(rootCustomerIDs) != 1 {
		return report, fmt.Errorf("modeldeployment: deployment must contain exactly one active root customer organization (found %d)", len(rootCustomerIDs))
	}
	if rootCustomerIDs[0] != customerOrgID {
		return report, fmt.Errorf("modeldeployment: requested customer organization does not match the sole active root customer")
	}
	channels, err := loadLegacyChannels(ctx, tx)
	if err != nil {
		return report, err
	}
	report.Counts.LegacyConnections = len(channels)
	providerFamilies := configuredProviderFamilies(channels)
	deployments := make(map[deploymentKey]deploymentRecord)
	currentChannelIDs := make(map[int64]struct{}, len(channels))
	currentCredentialIDs := make(map[int64]struct{})
	trustedDeploymentChannels := make(map[int64]bool, len(channels))
	currentDeploymentIdentities := make(map[deploymentSourceIdentity]struct{})

	for _, channel := range channels {
		currentChannelIDs[channel.ID] = struct{}{}
		trustedDeploymentChannels[channel.ID] = true
		models := modellist.Parse(channel.Models)
		report.Counts.LegacyConfiguredModels += len(models)
		credentials, credentialErr := loadLegacyCredentials(ctx, tx, channel.ID)
		if credentialErr != nil {
			return report, credentialErr
		}
		for _, credential := range credentials {
			currentCredentialIDs[credential.ID] = struct{}{}
			if credential.Status != "retired" {
				report.Counts.LegacyCredentials++
			}
		}
		mapping, mappingErr := decodeModelMapping(channel.ModelMapping)
		if mappingErr != nil {
			trustedDeploymentChannels[channel.ID] = false
			addBlocking(&report, "model_mapping_invalid", "channel", strconv.FormatInt(channel.ID, 10), mappingErr.Error())
		}
		upstreams := make(map[string]string, len(models))
		for _, publicModel := range models {
			key := deploymentKey{ChannelID: channel.ID, Model: publicModel}
			if mappingErr != nil {
				continue
			}
			upstream := publicModel
			if mapped := strings.TrimSpace(mapping[publicModel]); mapped != "" {
				upstream = mapped
			}
			if !validBoundedIdentity(publicModel, 100) {
				trustedDeploymentChannels[channel.ID] = false
				addBlocking(&report, "public_model_identity_invalid", "channel_model", deploymentResourceID(key), "public model cannot be represented by the current model registry")
				continue
			}
			if !validBoundedIdentity(upstream, 200) {
				trustedDeploymentChannels[channel.ID] = false
				addBlocking(&report, "upstream_model_identity_invalid", "channel_model", deploymentResourceID(key), "upstream model is empty, too long, or contains control characters")
				continue
			}
			upstreams[publicModel] = upstream
			currentDeploymentIdentities[deploymentSourceIdentity{ChannelID: channel.ID, Model: publicModel, Upstream: upstream}] = struct{}{}
		}
		if connectionErr := validateLegacyConnection(channel); connectionErr != nil {
			trustedDeploymentChannels[channel.ID] = false
			addBlocking(&report, "provider_connection_invalid", "channel", strconv.FormatInt(channel.ID, 10), connectionErr.Error())
			continue
		}
		connectionID, syncErr := upsertConnection(ctx, tx, channel)
		if syncErr != nil {
			if errors.Is(syncErr, ErrRetiredIdentity) {
				trustedDeploymentChannels[channel.ID] = false
				addBlocking(&report, "provider_connection_retired", "channel", strconv.FormatInt(channel.ID, 10), syncErr.Error())
				continue
			}
			return report, syncErr
		}
		for _, publicModel := range models {
			key := deploymentKey{ChannelID: channel.ID, Model: publicModel}
			upstream, representable := upstreams[publicModel]
			if !representable {
				continue
			}
			publicModelID, identityErr := ensurePublicModel(ctx, tx, customerOrgID, publicModel, providerFamilies[publicModel])
			if identityErr != nil {
				trustedDeploymentChannels[channel.ID] = false
				code := "public_model_identity_unavailable"
				switch {
				case errors.Is(identityErr, modelgov.ErrModelIdentityConflict):
					code = "equivalent_model_identity_conflict"
				case errors.Is(identityErr, modelgov.ErrModelRetired):
					code = "public_model_retired"
				}
				addBlocking(&report, code, "channel_model", deploymentResourceID(key), identityErr.Error())
				continue
			}
			deployment, retiredPrevious, deploymentErr := reconcileDeployment(
				ctx, tx, customerOrgID, publicModelID, publicModel, upstream, connectionID, channel,
			)
			if deploymentErr != nil {
				trustedDeploymentChannels[channel.ID] = false
				addBlocking(&report, "deployment_identity_conflict", "channel_model", deploymentResourceID(key), deploymentErr.Error())
				continue
			}
			if retiredPrevious {
				report.Counts.DeploymentsRetired++
			}
			deployments[key] = deployment
			operationRef := fmt.Sprintf("legacy:channel:%d", channel.ID)
			tag, err := tx.Exec(ctx, `
INSERT INTO model_deployment_operations (
    deployment_id,operation_id,support_state,evidence_state,
    evidence_ref,verified_at,revision,protocol_profile_id,protocol_profile_version,
    created_at,updated_at)
VALUES ($1,$2,'enabled','declared',$3,NULL,1,$4,$5,NOW(),NOW())
ON CONFLICT (deployment_id,operation_id) DO NOTHING`, deployment.ID, LegacyOperationID, operationRef,
				LegacyProbeProfileID, LegacyProbeProfileVersion)
			if err != nil {
				return report, fmt.Errorf("modeldeployment: reconcile legacy operation: %w", err)
			}
			if tag.RowsAffected() == 0 {
				var supportState, evidenceState, evidenceRef string
				var verifiedAt *time.Time
				var revision int64
				if err := tx.QueryRow(ctx, `
SELECT support_state,evidence_state,evidence_ref,verified_at,revision
  FROM model_deployment_operations
 WHERE deployment_id=$1 AND operation_id=$2
 FOR UPDATE`, deployment.ID, LegacyOperationID).Scan(
					&supportState, &evidenceState, &evidenceRef, &verifiedAt, &revision,
				); err != nil {
					return report, fmt.Errorf("modeldeployment: read legacy operation: %w", err)
				}
				if supportState != "enabled" || evidenceState != "declared" ||
					evidenceRef != operationRef || verifiedAt != nil || revision != 1 {
					addBlocking(&report, "deployment_operation_conflict", "model_deployment", strconv.FormatInt(deployment.ID, 10), "existing legacy operation differs from the deterministic projection")
				}
			}
		}

		for _, credential := range credentials {
			binding, bindingErr := upsertCredentialBinding(ctx, tx, connectionID, credential)
			if bindingErr != nil {
				if errors.Is(bindingErr, ErrRetiredIdentity) {
					addBlocking(&report, "credential_binding_retired", "channel_key", strconv.FormatInt(credential.ID, 10), bindingErr.Error())
					continue
				}
				return report, bindingErr
			}
			if !binding.Active {
				// A terminal binding is an immutable security and audit fact. Keep
				// its deployment links as provenance; routing already excludes it by
				// status, and deleting the links would let a later reconciliation
				// erase the exact scope of an explicit credential revocation.
				continue
			}
			if err := reconcileCredentialDeploymentLinks(ctx, tx, &report, customerOrgID, channel, credential, binding.ID, deployments); err != nil {
				return report, err
			}
		}
	}
	if err := retireMissingLegacyProjection(
		ctx, tx, &report, customerOrgID, currentChannelIDs, currentCredentialIDs,
		trustedDeploymentChannels, currentDeploymentIdentities,
	); err != nil {
		return report, err
	}

	if err := reconcileProbes(ctx, tx, &report, customerOrgID, deployments); err != nil {
		return report, err
	}
	if err := reconcileRouteItems(ctx, tx, &report, customerOrgID, deployments); err != nil {
		return report, err
	}
	if err := attributeEvidence(ctx, tx, &report, customerOrgID); err != nil {
		return report, err
	}
	if err := loadCanonicalCounts(ctx, tx, &report, customerOrgID); err != nil {
		return report, err
	}

	sortIssues(report.BlockingIssues)
	sortIssues(report.Warnings)
	report.Ready = len(report.BlockingIssues) == 0 && countsComplete(report.Counts)
	if !report.Ready && len(report.BlockingIssues) == 0 {
		addBlocking(&report, "canonical_projection_count_mismatch", "reconcile", strconv.FormatInt(customerOrgID, 10), "canonical row counts do not cover the legacy source")
		sortIssues(report.BlockingIssues)
	}
	if err := tx.Commit(ctx); err != nil {
		return report, fmt.Errorf("modeldeployment: commit reconcile: %w", err)
	}
	if !report.Ready {
		return report, ErrIncomplete
	}
	return report, nil
}

func loadLegacyChannels(ctx context.Context, tx pgx.Tx) ([]legacyChannel, error) {
	rows, err := tx.Query(ctx, `
SELECT id,name,provider,base_url,models,COALESCE(model_mapping,'{}'::jsonb),
       COALESCE(NULLIF(BTRIM(adapter_type),''),'openai_compatible'),
       COALESCE(adapter_config,'{}'::jsonb),max_context,default_max_output,
       priority,status,circuit_state,half_open_until,fail_count,degradation_threshold,
       routing_weight,routing_epoch,circuit_epoch,half_open_probe_until,
       COALESCE(half_open_probe_token,''),created_at,updated_at
  FROM channels ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("modeldeployment: list legacy channels: %w", err)
	}
	defer rows.Close()
	result := make([]legacyChannel, 0)
	for rows.Next() {
		var item legacyChannel
		if err := rows.Scan(
			&item.ID, &item.Name, &item.Provider, &item.BaseURL, &item.Models, &item.ModelMapping,
			&item.AdapterType, &item.AdapterConfig, &item.MaxContext, &item.DefaultMaxOutput,
			&item.Priority, &item.Status, &item.CircuitState, &item.HalfOpenUntil, &item.FailCount,
			&item.DegradationThreshold, &item.RoutingWeight, &item.RoutingEpoch, &item.CircuitEpoch,
			&item.HalfOpenProbeUntil, &item.HalfOpenProbeToken, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("modeldeployment: scan legacy channel: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("modeldeployment: read legacy channels: %w", err)
	}
	return result, nil
}

func configuredProviderFamilies(channels []legacyChannel) map[string]string {
	providers := make(map[string]map[string]struct{})
	for _, channel := range channels {
		for _, model := range modellist.Parse(channel.Models) {
			if providers[model] == nil {
				providers[model] = make(map[string]struct{})
			}
			providers[model][strings.TrimSpace(channel.Provider)] = struct{}{}
		}
	}
	result := make(map[string]string, len(providers))
	for model, values := range providers {
		if len(values) != 1 {
			result[model] = "mixed"
			continue
		}
		for provider := range values {
			result[model] = provider
		}
	}
	return result
}

func upsertConnection(ctx context.Context, tx pgx.Tx, channel legacyChannel) (int64, error) {
	state := "enabled"
	if channel.Status == "disabled" {
		state = "disabled"
	}
	const updateExisting = `
UPDATE provider_connections
   SET name=$2,provider=$3,base_url=$4,adapter_type=$5,adapter_config=$6,
       admin_state=$7,config_revision=$8,updated_at=$9
 WHERE legacy_channel_id=$1 AND admin_state <> 'retired'
RETURNING id`
	updateArgs := []any{
		channel.ID, channel.Name, channel.Provider, channel.BaseURL, channel.AdapterType,
		channel.AdapterConfig, state, channel.RoutingEpoch, channel.UpdatedAt,
	}
	insertArgs := []any{
		channel.ID, channel.Name, channel.Provider, channel.BaseURL, channel.AdapterType,
		channel.AdapterConfig, state, channel.RoutingEpoch, channel.CreatedAt, channel.UpdatedAt,
	}
	var id int64
	err := tx.QueryRow(ctx, updateExisting, updateArgs...).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("modeldeployment: reconcile provider connection %d: %w", channel.ID, err)
	}

	// An UPDATE takes the provider_connections RowExclusive relation lock even
	// when no row matches. Acquire the exact License resource advisory only after
	// that relation lock, matching the M79 trigger/migration order. Recheck while
	// holding the advisory so concurrent reconcilers cannot both take the absent
	// path; only a genuinely new logical connection reaches INSERT and consumes a
	// seat in the BEFORE INSERT trigger.
	if _, err := tx.Exec(ctx, `
SELECT pg_advisory_xact_lock(
    hashtextextended('unirouter.commercial_limit.active_channels', 0)
)`); err != nil {
		return 0, fmt.Errorf("modeldeployment: lock provider connection License authority %d: %w", channel.ID, err)
	}
	err = tx.QueryRow(ctx, updateExisting, updateArgs...).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("modeldeployment: recheck provider connection %d: %w", channel.ID, err)
	}
	err = tx.QueryRow(ctx, `
SELECT id
  FROM provider_connections
 WHERE legacy_channel_id=$1`, channel.ID).Scan(&id)
	if err == nil {
		return 0, fmt.Errorf("%w: provider connection for legacy channel %d", ErrRetiredIdentity, channel.ID)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("modeldeployment: inspect provider connection %d: %w", channel.ID, err)
	}
	err = tx.QueryRow(ctx, `
INSERT INTO provider_connections (
    legacy_channel_id,name,provider,base_url,adapter_type,adapter_config,
    admin_state,config_revision,created_at,updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
RETURNING id`, insertArgs...).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("modeldeployment: create provider connection %d: %w", channel.ID, err)
	}
	return id, nil
}

func ensurePublicModel(ctx context.Context, tx pgx.Tx, orgID int64, name, providerFamily string) (int64, error) {
	if _, err := modelgov.ActivatedIdentity(ctx, tx, orgID, name); err != nil {
		return 0, err
	}
	var id int64
	err := tx.QueryRow(ctx, `SELECT id FROM model_registry WHERE org_id=$1 AND model_name=$2`, orgID, name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("read public model: %w", err)
	}
	err = tx.QueryRow(ctx, `
INSERT INTO model_registry (
    org_id,model_name,display_name,provider_family,lifecycle,context_length,
    output_limit,capability_tags,data_residency,pricing_model_name,
    approved_catalog,migration_recommendation,created_at,updated_at)
VALUES ($1,$2,$2,$3,'staging',0,0,'','',$2,FALSE,'',NOW(),NOW())
ON CONFLICT (org_id,model_name) DO UPDATE SET model_name=EXCLUDED.model_name
RETURNING id`, orgID, name, providerFamily).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create public model: %w", err)
	}
	return id, nil
}

func reconcileDeployment(
	ctx context.Context,
	tx pgx.Tx,
	orgID, publicModelID int64,
	publicModel, upstream string,
	connectionID int64,
	channel legacyChannel,
) (deploymentRecord, bool, error) {
	savepoint, err := tx.Begin(ctx)
	if err != nil {
		return deploymentRecord{}, false, fmt.Errorf("begin deployment reconcile savepoint: %w", err)
	}
	defer savepoint.Rollback(ctx)
	record, retiredPrevious, err := reconcileDeploymentInTx(
		ctx, savepoint, orgID, publicModelID, publicModel, upstream, connectionID, channel,
	)
	if err != nil {
		return deploymentRecord{}, false, err
	}
	if err := savepoint.Commit(ctx); err != nil {
		return deploymentRecord{}, false, fmt.Errorf("commit deployment reconcile savepoint: %w", err)
	}
	return record, retiredPrevious, nil
}

func reconcileDeploymentInTx(
	ctx context.Context,
	tx pgx.Tx,
	orgID, publicModelID int64,
	publicModel, upstream string,
	connectionID int64,
	channel legacyChannel,
) (deploymentRecord, bool, error) {
	// Connection availability and deployment availability are independent
	// control axes. Legacy has only a channel-level switch, so reconciliation
	// maps that switch to ProviderConnection and initializes a new deployment
	// enabled; it must never overwrite a later canonical deployment decision.
	state := "enabled"
	var existingID, existingModelID, existingConnectionID int64
	var existingUpstream string
	readErr := tx.QueryRow(ctx, `
SELECT id,public_model_id,connection_id,upstream_model
  FROM model_deployments
 WHERE customer_org_id=$1 AND legacy_channel_id=$2 AND legacy_public_model=$3
   AND admin_state <> 'retired'
 FOR UPDATE`, orgID, channel.ID, publicModel).Scan(
		&existingID, &existingModelID, &existingConnectionID, &existingUpstream,
	)
	retiredPrevious := false
	if readErr == nil {
		if existingModelID != publicModelID || existingConnectionID != connectionID {
			return deploymentRecord{}, false, fmt.Errorf("legacy deployment identity already maps to another public model or connection")
		}
		if existingUpstream == upstream {
			_, err := tx.Exec(ctx, `
WITH runtime_authority AS (
    SELECT EXISTS (
        SELECT 1
          FROM runtime_change_slots slot
         CROSS JOIN LATERAL (
               VALUES (slot.stable_release_id,'active'::text),
                      (slot.canary_release_id,'canary'::text)
          ) AS current_release(release_id,expected_stage)
          JOIN runtime_change_releases release
            ON release.id=current_release.release_id
           AND release.stage=current_release.expected_stage
           AND release.candidate_version='unirouter.runtime-change-candidate.v2'
           AND release.route_items_sealed_at IS NOT NULL
           AND release.public_model_id=slot.public_model_id
          JOIN runtime_route_revision_items item ON item.release_id=release.id
         WHERE slot.public_model_id IS NOT NULL AND item.deployment_id=$14
    ) AS owns_health
)
UPDATE model_deployments deployment SET
    config_revision=$1,priority=$2,routing_weight=$3,
	max_context=$4,default_max_output=$5,degradation_threshold=$6,
	circuit_state=CASE WHEN authority.owns_health THEN deployment.circuit_state ELSE $7 END,
	circuit_revision=CASE WHEN authority.owns_health THEN deployment.circuit_revision ELSE $8 END,
	fail_count=CASE WHEN authority.owns_health THEN deployment.fail_count ELSE $9 END,
	half_open_until=CASE WHEN authority.owns_health THEN deployment.half_open_until ELSE $10 END,
	half_open_probe_until=CASE
	  WHEN deployment.admin_state<>'enabled' THEN NULL
	  WHEN authority.owns_health THEN deployment.half_open_probe_until
	  ELSE $11
	END,
	half_open_probe_token=CASE
	  WHEN deployment.admin_state<>'enabled' THEN NULL
	  WHEN authority.owns_health THEN deployment.half_open_probe_token
	  ELSE NULLIF($12,'')
	END,
	updated_at=GREATEST(deployment.updated_at,$13)
  FROM runtime_authority authority
 WHERE deployment.id=$14`, channel.RoutingEpoch, channel.Priority, channel.RoutingWeight,
				channel.MaxContext, channel.DefaultMaxOutput, channel.DegradationThreshold,
				normalizedCircuitState(channel.CircuitState), channel.CircuitEpoch, max(channel.FailCount, 0),
				channel.HalfOpenUntil, channel.HalfOpenProbeUntil, channel.HalfOpenProbeToken, channel.UpdatedAt, existingID)
			if err != nil {
				return deploymentRecord{}, false, fmt.Errorf("update existing deployment: %w", err)
			}
			return deploymentRecord{ID: existingID, ConnectionID: connectionID, PublicModel: publicModel, Upstream: upstream}, false, nil
		}
		if _, err := tx.Exec(ctx, `
UPDATE model_deployments
   SET admin_state='retired',retired_at=NOW(),config_revision=config_revision+1,updated_at=NOW()
 WHERE id=$1`, existingID); err != nil {
			return deploymentRecord{}, false, fmt.Errorf("retire superseded deployment: %w", err)
		}
		retiredPrevious = true
	} else if !errors.Is(readErr, pgx.ErrNoRows) {
		return deploymentRecord{}, false, fmt.Errorf("read existing deployment: %w", readErr)
	}

	var id int64
	err := tx.QueryRow(ctx, `
INSERT INTO model_deployments (
    customer_org_id,public_model_id,connection_id,upstream_model,admin_state,
    config_revision,priority,routing_weight,max_context,default_max_output,
    degradation_threshold,circuit_state,circuit_revision,fail_count,
    half_open_until,half_open_probe_until,half_open_probe_token,
    legacy_channel_id,legacy_public_model,created_at,updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,NULLIF($17,''),$18,$19,$20,$21)
RETURNING id`, orgID, publicModelID, connectionID, upstream, state, channel.RoutingEpoch,
		channel.Priority, channel.RoutingWeight, channel.MaxContext, channel.DefaultMaxOutput,
		channel.DegradationThreshold, normalizedCircuitState(channel.CircuitState), channel.CircuitEpoch,
		max(channel.FailCount, 0), channel.HalfOpenUntil, channel.HalfOpenProbeUntil, channel.HalfOpenProbeToken,
		channel.ID, publicModel, channel.CreatedAt, channel.UpdatedAt).Scan(&id)
	if err != nil {
		return deploymentRecord{}, false, fmt.Errorf("create deployment: %w", err)
	}
	return deploymentRecord{ID: id, ConnectionID: connectionID, PublicModel: publicModel, Upstream: upstream}, retiredPrevious, nil
}

func loadLegacyCredentials(ctx context.Context, tx pgx.Tx, channelID int64) ([]legacyCredential, error) {
	rows, err := tx.Query(ctx, `
SELECT id,key_enc,COALESCE(allowed_models,''),status,state_epoch,fail_count,
       last_used_at,cooldown_until,created_at,updated_at
  FROM channel_keys WHERE channel_id=$1 ORDER BY id`, channelID)
	if err != nil {
		return nil, fmt.Errorf("modeldeployment: list legacy credentials: %w", err)
	}
	defer rows.Close()
	result := make([]legacyCredential, 0)
	for rows.Next() {
		var item legacyCredential
		if err := rows.Scan(&item.ID, &item.KeyEnc, &item.AllowedModels, &item.Status, &item.StateEpoch,
			&item.FailCount, &item.LastUsedAt, &item.CooldownUntil, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("modeldeployment: scan legacy credential: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("modeldeployment: read legacy credentials: %w", err)
	}
	return result, nil
}

func upsertCredentialBinding(
	ctx context.Context,
	tx pgx.Tx,
	connectionID int64,
	credential legacyCredential,
) (credentialBindingRecord, error) {
	status := credential.Status
	switch status {
	case "active", "disabled", "invalid", "retired":
	default:
		status = "invalid"
	}
	keyEnc := credential.KeyEnc
	var retiredAt any
	if status == "retired" {
		keyEnc = ""
		retiredAt = credential.UpdatedAt
	}
	var id int64
	query := `
WITH runtime_authority AS (
    SELECT EXISTS (
        SELECT 1
          FROM provider_credential_bindings existing_binding
          JOIN credential_binding_deployments link
            ON link.binding_id=existing_binding.id
          JOIN runtime_route_revision_items item
            ON item.deployment_id=link.deployment_id
          JOIN runtime_change_releases release
            ON release.id=item.release_id
           AND release.candidate_version='unirouter.runtime-change-candidate.v2'
           AND release.route_items_sealed_at IS NOT NULL
          JOIN runtime_change_slots slot
            ON slot.public_model_id=release.public_model_id
           AND (
               (slot.stable_release_id=release.id AND release.stage='active')
               OR (slot.canary_release_id=release.id AND release.stage='canary')
           )
         WHERE existing_binding.legacy_channel_key_id=$9
    ) AS owns_health
)
INSERT INTO provider_credential_bindings (
    connection_id,key_enc,status,retired_at,state_revision,fail_count,last_used_at,
    cooldown_until,legacy_channel_key_id,created_at,updated_at)
SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11 FROM runtime_authority
ON CONFLICT (legacy_channel_key_id) DO UPDATE SET
    connection_id=EXCLUDED.connection_id,key_enc=EXCLUDED.key_enc,status=EXCLUDED.status,
    retired_at=EXCLUDED.retired_at,
    state_revision=CASE
      WHEN (SELECT owns_health FROM runtime_authority)
       AND NOT (
          provider_credential_bindings.key_enc IS DISTINCT FROM EXCLUDED.key_enc
          OR provider_credential_bindings.status IS DISTINCT FROM EXCLUDED.status
          OR provider_credential_bindings.retired_at IS DISTINCT FROM EXCLUDED.retired_at
       )
      THEN provider_credential_bindings.state_revision
      WHEN (SELECT owns_health FROM runtime_authority)
      THEN GREATEST(provider_credential_bindings.state_revision+1,EXCLUDED.state_revision)
      ELSE EXCLUDED.state_revision
    END,
    fail_count=CASE WHEN (SELECT owns_health FROM runtime_authority)
                    THEN provider_credential_bindings.fail_count ELSE EXCLUDED.fail_count END,
    last_used_at=CASE WHEN (SELECT owns_health FROM runtime_authority)
                     THEN provider_credential_bindings.last_used_at ELSE EXCLUDED.last_used_at END,
    cooldown_until=CASE WHEN (SELECT owns_health FROM runtime_authority)
                        THEN provider_credential_bindings.cooldown_until ELSE EXCLUDED.cooldown_until END,
    updated_at=CASE
      WHEN (SELECT owns_health FROM runtime_authority)
      THEN GREATEST(provider_credential_bindings.updated_at,EXCLUDED.updated_at)
      ELSE EXCLUDED.updated_at
    END
WHERE provider_credential_bindings.status <> 'retired'
RETURNING id`
	err := tx.QueryRow(ctx, query, connectionID, keyEnc, status, retiredAt, credential.StateEpoch,
		max(credential.FailCount, 0), credential.LastUsedAt, credential.CooldownUntil,
		credential.ID, credential.CreatedAt, credential.UpdatedAt).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		var existingStatus string
		if readErr := tx.QueryRow(ctx, `
SELECT id,status FROM provider_credential_bindings
 WHERE legacy_channel_key_id=$1`, credential.ID).Scan(&id, &existingStatus); readErr != nil {
			return credentialBindingRecord{}, fmt.Errorf("modeldeployment: read retired credential %d: %w", credential.ID, readErr)
		}
		if status == "retired" && existingStatus == "retired" {
			return credentialBindingRecord{ID: id, Active: false}, nil
		}
		return credentialBindingRecord{}, fmt.Errorf("%w: credential binding for legacy key %d", ErrRetiredIdentity, credential.ID)
	}
	if err != nil {
		return credentialBindingRecord{}, fmt.Errorf("modeldeployment: reconcile credential %d: %w", credential.ID, err)
	}
	return credentialBindingRecord{ID: id, Active: status != "retired"}, nil
}

func retireMissingLegacyProjection(
	ctx context.Context,
	tx pgx.Tx,
	report *Report,
	orgID int64,
	currentChannelIDs map[int64]struct{},
	currentCredentialIDs map[int64]struct{},
	trustedDeploymentChannels map[int64]bool,
	currentDeploymentIdentities map[deploymentSourceIdentity]struct{},
) error {
	type legacyDeployment struct {
		ID        int64
		ChannelID int64
		Model     string
		Upstream  string
	}
	rows, err := tx.Query(ctx, `
SELECT id,legacy_channel_id,legacy_public_model,upstream_model
  FROM model_deployments
 WHERE customer_org_id=$1 AND legacy_channel_id IS NOT NULL
   AND admin_state <> 'retired'
 ORDER BY id`, orgID)
	if err != nil {
		return fmt.Errorf("modeldeployment: list deployments eligible for retirement: %w", err)
	}
	legacyDeployments := make([]legacyDeployment, 0)
	for rows.Next() {
		var item legacyDeployment
		if err := rows.Scan(&item.ID, &item.ChannelID, &item.Model, &item.Upstream); err != nil {
			rows.Close()
			return fmt.Errorf("modeldeployment: scan deployment eligible for retirement: %w", err)
		}
		legacyDeployments = append(legacyDeployments, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("modeldeployment: read deployments eligible for retirement: %w", err)
	}
	rows.Close()
	for _, item := range legacyDeployments {
		if _, channelExists := currentChannelIDs[item.ChannelID]; channelExists {
			if !trustedDeploymentChannels[item.ChannelID] {
				continue
			}
			if _, sourceExists := currentDeploymentIdentities[deploymentSourceIdentity{
				ChannelID: item.ChannelID, Model: item.Model, Upstream: item.Upstream,
			}]; sourceExists {
				continue
			}
		}
		if _, err := tx.Exec(ctx, `
DELETE FROM credential_binding_deployments WHERE deployment_id=$1`, item.ID); err != nil {
			return fmt.Errorf("modeldeployment: unlink retiring deployment: %w", err)
		}
		tag, err := tx.Exec(ctx, `
UPDATE model_deployments
   SET admin_state='retired',retired_at=NOW(),config_revision=config_revision+1,updated_at=NOW()
 WHERE id=$1 AND admin_state <> 'retired'`, item.ID)
		if err != nil {
			return fmt.Errorf("modeldeployment: retire missing legacy deployment: %w", err)
		}
		report.Counts.DeploymentsRetired += int(tag.RowsAffected())
	}

	type legacyBinding struct {
		ID          int64
		LegacyKeyID int64
	}
	rows, err = tx.Query(ctx, `
SELECT id,legacy_channel_key_id
  FROM provider_credential_bindings
 WHERE legacy_channel_key_id IS NOT NULL AND status <> 'retired'
 ORDER BY id`)
	if err != nil {
		return fmt.Errorf("modeldeployment: list credentials eligible for retirement: %w", err)
	}
	bindings := make([]legacyBinding, 0)
	for rows.Next() {
		var item legacyBinding
		if err := rows.Scan(&item.ID, &item.LegacyKeyID); err != nil {
			rows.Close()
			return fmt.Errorf("modeldeployment: scan credential eligible for retirement: %w", err)
		}
		bindings = append(bindings, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("modeldeployment: read credentials eligible for retirement: %w", err)
	}
	rows.Close()
	for _, item := range bindings {
		if _, sourceExists := currentCredentialIDs[item.LegacyKeyID]; sourceExists {
			continue
		}
		if _, err := tx.Exec(ctx, `
DELETE FROM credential_binding_deployments WHERE binding_id=$1`, item.ID); err != nil {
			return fmt.Errorf("modeldeployment: unlink retiring credential: %w", err)
		}
		tag, err := tx.Exec(ctx, `
UPDATE provider_credential_bindings
   SET status='retired',key_enc='',retired_at=NOW(),
       state_revision=state_revision+1,updated_at=NOW()
 WHERE id=$1 AND status <> 'retired'`, item.ID)
		if err != nil {
			return fmt.Errorf("modeldeployment: retire missing legacy credential: %w", err)
		}
		report.Counts.CredentialsRetired += int(tag.RowsAffected())
	}

	type legacyConnection struct {
		ID        int64
		ChannelID int64
	}
	rows, err = tx.Query(ctx, `
SELECT id,legacy_channel_id
  FROM provider_connections
 WHERE legacy_channel_id IS NOT NULL AND admin_state <> 'retired'
 ORDER BY id`)
	if err != nil {
		return fmt.Errorf("modeldeployment: list connections eligible for retirement: %w", err)
	}
	connections := make([]legacyConnection, 0)
	for rows.Next() {
		var item legacyConnection
		if err := rows.Scan(&item.ID, &item.ChannelID); err != nil {
			rows.Close()
			return fmt.Errorf("modeldeployment: scan connection eligible for retirement: %w", err)
		}
		connections = append(connections, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("modeldeployment: read connections eligible for retirement: %w", err)
	}
	rows.Close()
	for _, item := range connections {
		if _, sourceExists := currentChannelIDs[item.ChannelID]; sourceExists {
			continue
		}
		var activeChildren int64
		if err := tx.QueryRow(ctx, `
SELECT (SELECT COUNT(*) FROM model_deployments
         WHERE connection_id=$1 AND admin_state <> 'retired')
     + (SELECT COUNT(*) FROM provider_credential_bindings
         WHERE connection_id=$1 AND status <> 'retired')`, item.ID).Scan(&activeChildren); err != nil {
			return fmt.Errorf("modeldeployment: count active connection children: %w", err)
		}
		if activeChildren != 0 {
			addBlocking(report, "connection_retirement_blocked_by_active_child", "provider_connection", strconv.FormatInt(item.ID, 10), "legacy source is missing but an active canonical deployment or credential still references the connection")
			continue
		}
		tag, err := tx.Exec(ctx, `
UPDATE provider_connections
   SET admin_state='retired',retired_at=NOW(),config_revision=config_revision+1,updated_at=NOW()
 WHERE id=$1 AND admin_state <> 'retired'`, item.ID)
		if err != nil {
			return fmt.Errorf("modeldeployment: retire missing legacy connection: %w", err)
		}
		report.Counts.ConnectionsRetired += int(tag.RowsAffected())
	}
	return nil
}

func reconcileCredentialDeploymentLinks(
	ctx context.Context,
	tx pgx.Tx,
	report *Report,
	orgID int64,
	channel legacyChannel,
	credential legacyCredential,
	bindingID int64,
	deployments map[deploymentKey]deploymentRecord,
) error {
	allowed := modellist.Parse(credential.AllowedModels)
	if len(allowed) == 0 {
		allowed = modellist.Parse(channel.Models)
	}
	configured := make(map[string]struct{})
	for _, model := range modellist.Parse(channel.Models) {
		configured[model] = struct{}{}
	}
	desired := make([]deploymentRecord, 0, len(allowed))
	desiredIDs := make([]int64, 0, len(allowed))
	for _, model := range allowed {
		if _, ok := configured[model]; !ok {
			addWarning(report, "credential_allowed_model_not_configured", "channel_key", strconv.FormatInt(credential.ID, 10), "allowed model is not configured on the legacy channel: "+model)
			continue
		}
		report.Counts.ExpectedCredentialDeploymentLinks++
		deployment, ok := deployments[deploymentKey{ChannelID: channel.ID, Model: model}]
		if !ok {
			addBlocking(report, "credential_deployment_mapping_missing", "channel_key", strconv.FormatInt(credential.ID, 10), "configured allowed model has no canonical deployment: "+model)
			continue
		}
		desired = append(desired, deployment)
		desiredIDs = append(desiredIDs, deployment.ID)
	}
	if _, err := tx.Exec(ctx, `
DELETE FROM credential_binding_deployments link
 USING model_deployments deployment
 WHERE link.binding_id=$1 AND deployment.id=link.deployment_id
   AND deployment.customer_org_id=$2
   AND NOT (link.deployment_id=ANY($3::bigint[]))`, bindingID, orgID, desiredIDs); err != nil {
		return fmt.Errorf("modeldeployment: remove obsolete credential deployment links: %w", err)
	}
	scopeEpoch := credential.UpdatedAt
	if channel.UpdatedAt.After(scopeEpoch) {
		scopeEpoch = channel.UpdatedAt
	}
	for _, deployment := range desired {
		if _, err := tx.Exec(ctx, `
INSERT INTO credential_binding_deployments (binding_id,deployment_id,connection_id,created_at)
VALUES ($1,$2,$3,$4) ON CONFLICT (binding_id,deployment_id) DO NOTHING`,
			bindingID, deployment.ID, deployment.ConnectionID, scopeEpoch); err != nil {
			return fmt.Errorf("modeldeployment: bind credential to deployment: %w", err)
		}
	}
	return nil
}

func reconcileProbes(ctx context.Context, tx pgx.Tx, report *Report, orgID int64, deployments map[deploymentKey]deploymentRecord) error {
	rows, err := tx.Query(ctx, `
SELECT channel_id,public_model,upstream_model,reported_model,routing_epoch,
       credential_id,credential_state_epoch,success,status_code,latency_ms,
       error_summary,tested_by,tested_at
  FROM channel_model_probes ORDER BY channel_id,public_model`)
	if err != nil {
		return fmt.Errorf("modeldeployment: list legacy probes: %w", err)
	}
	probes := make([]legacyProbe, 0)
	for rows.Next() {
		var probe legacyProbe
		if err := rows.Scan(&probe.ChannelID, &probe.PublicModel, &probe.Upstream, &probe.Reported, &probe.RoutingEpoch,
			&probe.CredentialID, &probe.CredentialState, &probe.Success, &probe.StatusCode, &probe.LatencyMS,
			&probe.ErrorSummary, &probe.TestedBy, &probe.TestedAt); err != nil {
			rows.Close()
			return fmt.Errorf("modeldeployment: scan legacy probe: %w", err)
		}
		probes = append(probes, probe)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("modeldeployment: read legacy probes: %w", err)
	}
	rows.Close()
	report.Counts.LegacyProbes = len(probes)

	for _, probe := range probes {
		deployment, ok := deployments[deploymentKey{ChannelID: probe.ChannelID, Model: probe.PublicModel}]
		if !ok {
			addBlocking(report, "probe_deployment_mapping_missing", "channel_probe", deploymentResourceID(deploymentKey{ChannelID: probe.ChannelID, Model: probe.PublicModel}), "legacy probe has no canonical deployment")
			continue
		}
		if strings.TrimSpace(probe.Upstream) != deployment.Upstream {
			addBlocking(report, "probe_deployment_identity_mismatch", "channel_probe", deploymentResourceID(deploymentKey{ChannelID: probe.ChannelID, Model: probe.PublicModel}), "legacy probe upstream model does not match the active canonical deployment")
			continue
		}
		var bindingID any
		bindingRevision := int64(0)
		if probe.CredentialID.Valid {
			var mapped int64
			if err := tx.QueryRow(ctx, `SELECT id FROM provider_credential_bindings WHERE legacy_channel_key_id=$1`, probe.CredentialID.Int64).Scan(&mapped); err != nil {
				addBlocking(report, "probe_credential_mapping_missing", "channel_probe", deploymentResourceID(deploymentKey{ChannelID: probe.ChannelID, Model: probe.PublicModel}), "legacy probe credential has no canonical binding")
				continue
			}
			bindingID = mapped
			bindingRevision = probe.CredentialState
		}
		var testedByValue any
		if probe.TestedBy.Valid {
			testedByValue = probe.TestedBy.Int64
		}
		sourceRef := deploymentResourceID(deploymentKey{ChannelID: probe.ChannelID, Model: probe.PublicModel})
		sourceFingerprint, fingerprintErr := legacyProbeFingerprint(
			probe.ChannelID, probe.PublicModel, probe.Upstream, probe.Reported, probe.RoutingEpoch, probe.CredentialID,
			probe.CredentialState, probe.Success, probe.StatusCode, probe.LatencyMS, probe.ErrorSummary, probe.TestedBy, probe.TestedAt,
		)
		if fingerprintErr != nil {
			return fmt.Errorf("modeldeployment: fingerprint legacy probe: %w", fingerprintErr)
		}
		tag, err := tx.Exec(ctx, `
INSERT INTO model_deployment_probes (
    deployment_id,connection_id,operation_id,probe_profile_id,probe_profile_version,
    upstream_model,reported_model,
    connection_revision,deployment_revision,credential_binding_id,
    credential_state_revision,success,status_code,latency_ms,error_summary,
    capabilities_verified,request_summary,response_summary,
    source_kind,source_ref,source_fingerprint,tested_by,tested_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$8,$9,$10,$11,$12,$13,$14,
        '[]'::jsonb,
        jsonb_build_object('source','legacy_channel_model_probe','upstream_model',$6::varchar),
        jsonb_build_object('reported_model',$7::varchar),
        'legacy.channel_model_probe',$15,$16,$17,$18)
ON CONFLICT (source_kind,source_ref,source_fingerprint) DO NOTHING`,
			deployment.ID, deployment.ConnectionID, LegacyOperationID,
			LegacyProbeProfileID, LegacyProbeProfileVersion, strings.TrimSpace(probe.Upstream),
			strings.TrimSpace(probe.Reported), probe.RoutingEpoch, bindingID, bindingRevision,
			probe.Success, probe.StatusCode, probe.LatencyMS, probe.ErrorSummary, sourceRef, sourceFingerprint,
			testedByValue, probe.TestedAt)
		if err != nil {
			return fmt.Errorf("modeldeployment: reconcile deployment probe: %w", err)
		}
		if tag.RowsAffected() == 0 {
			var exact bool
			if err := tx.QueryRow(ctx, `
SELECT deployment_id=$4
       AND connection_id=$5
       AND operation_id=$6
       AND probe_profile_id=$7
       AND probe_profile_version=$8
       AND upstream_model=$9
       AND reported_model=$10
       AND connection_revision=$11
       AND deployment_revision=$11
       AND credential_binding_id IS NOT DISTINCT FROM $12::bigint
       AND credential_state_revision=$13
       AND success=$14
       AND status_code=$15
       AND latency_ms=$16
       AND error_summary=$17
       AND capabilities_verified='[]'::jsonb
       AND request_summary=jsonb_build_object('source','legacy_channel_model_probe','upstream_model',$9::text)
       AND response_summary=jsonb_build_object('reported_model',$10::text)
       AND tested_by IS NOT DISTINCT FROM $18::bigint
       AND tested_at=$19
  FROM model_deployment_probes
 WHERE source_kind=$1 AND source_ref=$2 AND source_fingerprint=$3`,
				"legacy.channel_model_probe", sourceRef, sourceFingerprint,
				deployment.ID, deployment.ConnectionID, LegacyOperationID,
				LegacyProbeProfileID, LegacyProbeProfileVersion, strings.TrimSpace(probe.Upstream),
				strings.TrimSpace(probe.Reported), probe.RoutingEpoch, bindingID, bindingRevision,
				probe.Success, probe.StatusCode, probe.LatencyMS, probe.ErrorSummary, testedByValue, probe.TestedAt,
			).Scan(&exact); err != nil {
				return fmt.Errorf("modeldeployment: read existing deployment probe: %w", err)
			}
			if !exact {
				addBlocking(report, "probe_snapshot_conflict", "channel_probe", sourceRef, "existing append-only probe snapshot differs from the deterministic legacy source")
			}
		}
	}
	return nil
}

func reconcileRouteItems(ctx context.Context, tx pgx.Tx, report *Report, orgID int64, deployments map[deploymentKey]deploymentRecord) error {
	current := make(map[int64]struct{})
	rows, err := tx.Query(ctx, `
SELECT slot.stable_release_id
  FROM runtime_change_slots slot
  JOIN runtime_change_releases release ON release.id=slot.stable_release_id
 WHERE slot.stable_release_id IS NOT NULL
   AND release.candidate_version=$1
UNION
SELECT slot.canary_release_id
  FROM runtime_change_slots slot
  JOIN runtime_change_releases release ON release.id=slot.canary_release_id
 WHERE slot.canary_release_id IS NOT NULL
   AND release.candidate_version=$1`, legacyRuntimeCandidateVersion)
	if err != nil {
		return fmt.Errorf("modeldeployment: list current releases: %w", err)
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("modeldeployment: scan current release: %w", err)
		}
		current[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("modeldeployment: read current releases: %w", err)
	}
	rows.Close()

	releaseRows, err := tx.Query(ctx, `
SELECT id,model_name,candidate_version,candidate
  FROM runtime_change_releases
 WHERE candidate_version=$1
 ORDER BY id`, legacyRuntimeCandidateVersion)
	if err != nil {
		return fmt.Errorf("modeldeployment: list route revisions: %w", err)
	}
	releases := make([]legacyRelease, 0)
	for releaseRows.Next() {
		var release legacyRelease
		if err := releaseRows.Scan(&release.ID, &release.ModelName, &release.CandidateVersion, &release.Candidate); err != nil {
			releaseRows.Close()
			return fmt.Errorf("modeldeployment: scan route revision: %w", err)
		}
		releases = append(releases, release)
	}
	if err := releaseRows.Err(); err != nil {
		releaseRows.Close()
		return fmt.Errorf("modeldeployment: read route revisions: %w", err)
	}
	releaseRows.Close()

	for _, release := range releases {
		var candidate releaseCandidate
		if err := json.Unmarshal(release.Candidate, &candidate); err != nil || candidate.ModelName != release.ModelName || candidate.ContractVersion != release.CandidateVersion {
			if _, isCurrent := current[release.ID]; isCurrent {
				addBlocking(report, "current_route_candidate_corrupt", "runtime_release", strconv.FormatInt(release.ID, 10), "current release candidate cannot be decoded exactly")
			} else {
				addWarning(report, "historical_route_candidate_corrupt", "runtime_release", strconv.FormatInt(release.ID, 10), "historical release remains available only through its original evidence")
			}
			continue
		}
		for index, override := range candidate.Overrides {
			admission := strings.TrimSpace(strings.ToLower(override.Admission))
			if admission == "" {
				admission = routingpolicy.AdmissionInherit
			}
			_, isCurrent := current[release.ID]
			if isCurrent {
				report.Counts.CurrentRouteOverrides++
			}
			deployment, ok := deployments[deploymentKey{ChannelID: override.ChannelID, Model: release.ModelName}]
			if !ok {
				message := fmt.Sprintf("release model %q channel %d has no canonical deployment", release.ModelName, override.ChannelID)
				if isCurrent {
					addBlocking(report, "current_route_deployment_mapping_missing", "runtime_release", strconv.FormatInt(release.ID, 10), message)
				} else {
					addWarning(report, "historical_route_mapping_missing", "runtime_release", strconv.FormatInt(release.ID, 10), message)
				}
				continue
			}
			ordinal := index + 1
			var existingDeployment, connectionRevision, deploymentRevision int64
			var existingOperation, existingAdmission, existingVersion string
			var existingPriority, existingWeight *int
			readExisting := func() error {
				return tx.QueryRow(ctx, `
SELECT deployment_id,operation_id,expected_connection_revision,
       expected_deployment_revision,admission,priority,weight,source_candidate_version
  FROM runtime_route_revision_items WHERE release_id=$1 AND ordinal=$2`, release.ID, ordinal).Scan(
					&existingDeployment, &existingOperation, &connectionRevision, &deploymentRevision,
					&existingAdmission, &existingPriority, &existingWeight, &existingVersion,
				)
			}
			readErr := readExisting()
			if errors.Is(readErr, pgx.ErrNoRows) {
				tag, insertErr := tx.Exec(ctx, `
INSERT INTO runtime_route_revision_items (
    release_id,ordinal,deployment_id,operation_id,expected_connection_revision,
    expected_deployment_revision,admission,priority,weight,source_candidate_version,created_at)
VALUES ($1,$2,$3,$4,$5,$5,$6,$7,$8,$9,NOW())
ON CONFLICT (release_id,ordinal) DO NOTHING`, release.ID, ordinal, deployment.ID,
					LegacyOperationID, override.ExpectedRoutingEpoch, admission, override.Priority, override.Weight, release.CandidateVersion)
				if insertErr != nil {
					return fmt.Errorf("modeldeployment: reconcile route revision item: %w", insertErr)
				}
				if tag.RowsAffected() == 1 {
					continue
				}
				readErr = readExisting()
			}
			if readErr != nil {
				return fmt.Errorf("modeldeployment: read existing route revision item: %w", readErr)
			}
			if existingDeployment != deployment.ID || existingOperation != LegacyOperationID ||
				connectionRevision != override.ExpectedRoutingEpoch || deploymentRevision != override.ExpectedRoutingEpoch ||
				existingAdmission != admission || !equalOptionalInt(existingPriority, override.Priority) ||
				!equalOptionalInt(existingWeight, override.Weight) || existingVersion != release.CandidateVersion {
				addBlocking(report, "route_revision_item_conflict", "runtime_release", strconv.FormatInt(release.ID, 10), "existing canonical route item differs from immutable release candidate")
			}
		}
	}
	return nil
}

func attributeEvidence(ctx context.Context, tx pgx.Tx, report *Report, orgID int64) error {
	tag, err := tx.Exec(ctx, `
UPDATE call_logs call_log
   SET route_deployment_id=deployment.id
  FROM model_deployments deployment
 WHERE call_log.org_id=$1
	   AND call_log.route_deployment_id IS NULL
	   AND call_log.route_channel_id=deployment.legacy_channel_id
	   AND call_log.model=deployment.legacy_public_model
	   AND deployment.customer_org_id=$1
	   AND deployment.admin_state <> 'retired'`, orgID)
	if err != nil {
		return fmt.Errorf("modeldeployment: attribute call logs: %w", err)
	}
	report.Counts.CallLogsAttributed = int(tag.RowsAffected())

	tag, err = tx.Exec(ctx, `
UPDATE billing_provider_attempts attempt
   SET deployment_id=evidence.deployment_id
  FROM (
    SELECT org_id,request_id,attempt_id,MIN(route_deployment_id) AS deployment_id
      FROM call_logs
     WHERE org_id=$1 AND route_deployment_id IS NOT NULL
       AND NULLIF(BTRIM(attempt_id),'') IS NOT NULL
     GROUP BY org_id,request_id,attempt_id
    HAVING COUNT(DISTINCT route_deployment_id)=1
  ) evidence
 WHERE attempt.org_id=evidence.org_id
   AND attempt.request_id=evidence.request_id
   AND attempt.attempt_id=evidence.attempt_id
   AND attempt.deployment_id IS NULL`, orgID)
	if err != nil {
		return fmt.Errorf("modeldeployment: attribute provider attempts: %w", err)
	}
	report.Counts.ProviderAttemptsAttributed = int(tag.RowsAffected())
	return nil
}

func loadCanonicalCounts(ctx context.Context, tx pgx.Tx, report *Report, orgID int64) error {
	queries := []struct {
		destination *int
		query       string
		args        []any
	}{
		{&report.Counts.CanonicalConnections, `SELECT COUNT(*) FROM provider_connections WHERE legacy_channel_id IS NOT NULL AND admin_state <> 'retired'`, nil},
		{&report.Counts.CanonicalDeployments, `SELECT COUNT(*) FROM model_deployments WHERE customer_org_id=$1 AND legacy_channel_id IS NOT NULL AND admin_state <> 'retired'`, []any{orgID}},
		{&report.Counts.CanonicalCredentials, `SELECT COUNT(*) FROM provider_credential_bindings WHERE legacy_channel_key_id IS NOT NULL AND status <> 'retired'`, nil},
		{&report.Counts.CredentialDeploymentLinks, `SELECT COUNT(*) FROM credential_binding_deployments link JOIN model_deployments deployment ON deployment.id=link.deployment_id JOIN provider_credential_bindings binding ON binding.id=link.binding_id WHERE deployment.customer_org_id=$1 AND deployment.admin_state <> 'retired' AND binding.status <> 'retired'`, []any{orgID}},
		{&report.Counts.CanonicalOperations, `SELECT COUNT(*) FROM model_deployment_operations operation JOIN model_deployments deployment ON deployment.id=operation.deployment_id WHERE deployment.customer_org_id=$1 AND deployment.admin_state <> 'retired' AND operation.operation_id=$2`, []any{orgID, LegacyOperationID}},
		{&report.Counts.CanonicalProbes, `SELECT COUNT(*) FROM model_deployment_latest_probes probe JOIN model_deployments deployment ON deployment.id=probe.deployment_id WHERE deployment.customer_org_id=$1 AND deployment.admin_state <> 'retired' AND probe.operation_id=$2`, []any{orgID, LegacyOperationID}},
		{&report.Counts.CurrentRouteItems, `
SELECT COUNT(*)
  FROM runtime_route_revision_items item
  JOIN runtime_change_releases release ON release.id=item.release_id
 WHERE release.candidate_version=$1 AND item.source_candidate_version=$1
   AND item.release_id IN (
       SELECT slot.stable_release_id
         FROM runtime_change_slots slot
         JOIN runtime_change_releases current_release ON current_release.id=slot.stable_release_id
        WHERE slot.stable_release_id IS NOT NULL AND current_release.candidate_version=$1
       UNION
       SELECT slot.canary_release_id
         FROM runtime_change_slots slot
         JOIN runtime_change_releases current_release ON current_release.id=slot.canary_release_id
        WHERE slot.canary_release_id IS NOT NULL AND current_release.candidate_version=$1
	   )`, []any{legacyRuntimeCandidateVersion}},
		{&report.Counts.HistoricalRouteItems, `
SELECT COUNT(*)
  FROM runtime_route_revision_items item
  JOIN runtime_change_releases release ON release.id=item.release_id
 WHERE release.candidate_version=$1 AND item.source_candidate_version=$1
   AND item.release_id NOT IN (
       SELECT slot.stable_release_id
         FROM runtime_change_slots slot
         JOIN runtime_change_releases current_release ON current_release.id=slot.stable_release_id
        WHERE slot.stable_release_id IS NOT NULL AND current_release.candidate_version=$1
       UNION
       SELECT slot.canary_release_id
         FROM runtime_change_slots slot
         JOIN runtime_change_releases current_release ON current_release.id=slot.canary_release_id
        WHERE slot.canary_release_id IS NOT NULL AND current_release.candidate_version=$1
	   )`, []any{legacyRuntimeCandidateVersion}},
	}
	for _, item := range queries {
		if err := tx.QueryRow(ctx, item.query, item.args...).Scan(item.destination); err != nil {
			return fmt.Errorf("modeldeployment: count canonical projection: %w", err)
		}
	}
	return nil
}

func countsComplete(counts Counts) bool {
	return counts.CanonicalConnections == counts.LegacyConnections &&
		counts.CanonicalDeployments == counts.LegacyConfiguredModels &&
		counts.CanonicalCredentials == counts.LegacyCredentials &&
		counts.CredentialDeploymentLinks == counts.ExpectedCredentialDeploymentLinks &&
		counts.CanonicalOperations == counts.CanonicalDeployments &&
		counts.CanonicalProbes == counts.LegacyProbes &&
		counts.CurrentRouteItems == counts.CurrentRouteOverrides
}

func decodeModelMapping(raw json.RawMessage) (map[string]string, error) {
	result := make(map[string]string)
	if len(raw) == 0 {
		return result, nil
	}
	if err := json.Unmarshal(raw, &result); err != nil || result == nil {
		return nil, fmt.Errorf("legacy model mapping must be an object of exact model IDs")
	}
	return result, nil
}

func validateLegacyConnection(channel legacyChannel) error {
	checks := []struct {
		name  string
		value string
		limit int
	}{
		{name: "name", value: channel.Name, limit: 255},
		{name: "provider", value: channel.Provider, limit: 50},
		{name: "base_url", value: channel.BaseURL, limit: 512},
		{name: "adapter_type", value: channel.AdapterType, limit: 50},
	}
	for _, check := range checks {
		if _, err := NormalizeIdentity(check.value, check.limit); err != nil {
			return fmt.Errorf("legacy connection %s is invalid", check.name)
		}
	}
	if len(channel.AdapterConfig) > 65536 {
		return fmt.Errorf("legacy adapter config exceeds 64 KiB")
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(channel.AdapterConfig, &config); err != nil || config == nil {
		return fmt.Errorf("legacy adapter config must be a JSON object")
	}
	return nil
}

func validBoundedIdentity(value string, maxBytes int) bool {
	_, err := NormalizeIdentity(value, maxBytes)
	return err == nil
}

func normalizedCircuitState(value string) string {
	switch strings.TrimSpace(value) {
	case "healthy", "half_open", "open":
		return strings.TrimSpace(value)
	default:
		return "open"
	}
}

func equalOptionalInt(left, right *int) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func legacyProbeFingerprint(
	channelID int64,
	publicModel, upstreamModel, reportedModel string,
	routingEpoch int64,
	credentialID sql.NullInt64,
	credentialState int64,
	success bool,
	statusCode int,
	latencyMS int64,
	errorSummary string,
	testedBy sql.NullInt64,
	testedAt time.Time,
) (string, error) {
	type nullableInt64 struct {
		Value int64 `json:"value"`
		Valid bool  `json:"valid"`
	}
	payload, err := json.Marshal(struct {
		ChannelID       int64         `json:"channel_id"`
		PublicModel     string        `json:"public_model"`
		UpstreamModel   string        `json:"upstream_model"`
		ReportedModel   string        `json:"reported_model"`
		RoutingEpoch    int64         `json:"routing_epoch"`
		CredentialID    nullableInt64 `json:"credential_id"`
		CredentialState int64         `json:"credential_state"`
		Success         bool          `json:"success"`
		StatusCode      int           `json:"status_code"`
		LatencyMS       int64         `json:"latency_ms"`
		ErrorSummary    string        `json:"error_summary"`
		TestedBy        nullableInt64 `json:"tested_by"`
		TestedAt        string        `json:"tested_at"`
	}{
		ChannelID: channelID, PublicModel: publicModel,
		UpstreamModel: strings.TrimSpace(upstreamModel), ReportedModel: strings.TrimSpace(reportedModel),
		RoutingEpoch:    routingEpoch,
		CredentialID:    nullableInt64{Value: credentialID.Int64, Valid: credentialID.Valid},
		CredentialState: credentialState, Success: success, StatusCode: statusCode,
		LatencyMS: latencyMS, ErrorSummary: errorSummary,
		TestedBy: nullableInt64{Value: testedBy.Int64, Valid: testedBy.Valid},
		TestedAt: testedAt.UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func deploymentResourceID(key deploymentKey) string {
	return strconv.FormatInt(key.ChannelID, 10) + ":" + key.Model
}

func addBlocking(report *Report, code, resourceType, resourceID, message string) {
	report.BlockingIssues = append(report.BlockingIssues, Issue{Severity: "blocking", Code: code, ResourceType: resourceType, ResourceID: resourceID, Message: message})
}

func addWarning(report *Report, code, resourceType, resourceID, message string) {
	report.Warnings = append(report.Warnings, Issue{Severity: "warning", Code: code, ResourceType: resourceType, ResourceID: resourceID, Message: message})
}

func sortIssues(items []Issue) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Code != items[j].Code {
			return items[i].Code < items[j].Code
		}
		if items[i].ResourceType != items[j].ResourceType {
			return items[i].ResourceType < items[j].ResourceType
		}
		return items[i].ResourceID < items[j].ResourceID
	})
}
