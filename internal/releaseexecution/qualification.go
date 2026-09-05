package releaseexecution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/executionevidence"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/strictjson"
)

type Reader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type CapabilityTarget struct {
	CustomerOrgID, PublicModelID, DeploymentID               int64
	OperationID, ExecutionProfileID, ExecutionProfileVersion string
	ProtocolProfileID, ProtocolProfileVersion                string
}

type CapabilityDecision struct {
	Pin   CapabilityPin
	Entry executionevidence.CapabilityEntry
}

var ErrCapabilityUnavailable = errors.New("exact capability qualification is unavailable")

// ReadCapabilityDecision reads original immutable Approval/Probe/binding
// facts, never the mutable qualification views. At authoring it also captures
// the exact candidate selector revision. Later reads verify the sealed pin and
// current safety state without consulting any current selector or Model Profile.
func ReadCapabilityDecision(ctx context.Context, q Reader, target CapabilityTarget, approvalID int64, pinned *CapabilityPin, lockMutable bool) (CapabilityDecision, error) {
	if q == nil || target.CustomerOrgID <= 0 || target.PublicModelID <= 0 || target.DeploymentID <= 0 || approvalID <= 0 {
		return CapabilityDecision{}, fmt.Errorf("exact capability decision target is required")
	}
	const query = `SELECT a.capability_probe_id,a.capability_id,a.probe_contract_id,a.probe_contract_version,a.approved_at,
       jsonb_build_array('unirouter.capability-binding-facts.v1',contract.operation_id,contract.capability_id,contract.execution_profile_id,contract.execution_profile_version,contract.protocol_profile_id,contract.protocol_profile_version,contract.probe_contract_id,contract.probe_contract_version,contract.expected_provider,contract.expected_upstream_model,contract.public_path,contract.introduced_migration,contract.introduced_publication_id,contract.profile_definition_digest)::text,
       jsonb_build_array(jsonb_build_array('unirouter.capability-approval-facts.v1',a.approval_id,a.capability_probe_id,a.customer_org_id,a.deployment_id,a.connection_id,a.operation_id,a.capability_id,a.execution_profile_id,a.execution_profile_version,a.protocol_profile_id,a.protocol_profile_version,a.probe_contract_id,a.probe_contract_version,a.connection_revision,a.deployment_revision,a.operation_revision,a.capability_support_revision,a.credential_binding_id,a.credential_state_revision,to_char(a.credential_link_created_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),a.approved_by,a.approved_by_org_id,a.reason,a.source_kind,a.source_ref,to_char(a.approved_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')),jsonb_build_array('unirouter.capability-probe-facts.v1',p.capability_probe_id,p.customer_org_id,p.deployment_id,p.connection_id,p.operation_id,p.capability_id,p.execution_profile_id,p.execution_profile_version,p.protocol_profile_id,p.protocol_profile_version,p.probe_contract_id,p.probe_contract_version,p.provider,p.base_url,p.adapter_type,p.adapter_config,p.upstream_model,p.connection_revision,p.deployment_revision,p.operation_revision,p.capability_support_revision,p.credential_binding_id,p.credential_state_revision,to_char(p.credential_link_created_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),p.provider_success,p.success,p.status_code,p.latency_ms,p.reported_model,p.error_summary,p.request_summary,p.response_summary,p.source_kind,p.source_ref,p.source_fingerprint,p.tested_by,to_char(p.tested_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')))::text,
       COALESCE(
         a.customer_org_id=p.customer_org_id AND a.deployment_id=p.deployment_id AND a.connection_id=p.connection_id
         AND a.operation_id=p.operation_id AND a.capability_id=p.capability_id
         AND a.execution_profile_id=p.execution_profile_id AND a.execution_profile_version=p.execution_profile_version
         AND a.protocol_profile_id=p.protocol_profile_id AND a.protocol_profile_version=p.protocol_profile_version
         AND a.probe_contract_id=p.probe_contract_id AND a.probe_contract_version=p.probe_contract_version
         AND a.connection_revision=p.connection_revision AND a.deployment_revision=p.deployment_revision
         AND a.operation_revision=p.operation_revision AND a.capability_support_revision=p.capability_support_revision
         AND a.credential_binding_id=p.credential_binding_id AND a.credential_state_revision=p.credential_state_revision
         AND a.credential_link_created_at=p.credential_link_created_at
         AND p.success AND p.source_kind='admin.model_capability_probe.v1'
         AND a.source_kind='admin.model_capability_routing_approval.v1'
         AND a.source_ref='capability_probe:'||p.capability_probe_id::text
         AND contract.expected_provider=p.provider AND contract.expected_upstream_model=p.upstream_model
         AND c.config_revision=a.connection_revision AND c.provider=p.provider AND c.base_url=p.base_url
         AND c.adapter_type=p.adapter_type AND COALESCE(c.adapter_config,'{}'::jsonb)=p.adapter_config
         AND d.config_revision=a.deployment_revision AND d.upstream_model=p.upstream_model
         AND o.revision=a.operation_revision AND o.protocol_profile_id=a.protocol_profile_id
         AND o.protocol_profile_version=a.protocol_profile_version
         AND cap.support_revision=a.capability_support_revision
         AND c.admin_state='enabled' AND d.admin_state='enabled' AND o.support_state='enabled' AND cap.support_state='enabled'
         AND model.lifecycle<>'retired'
         AND (SELECT count(*)=1 AND min(id)=$2 FROM orgs WHERE status='active' AND org_type='customer' AND parent_id IS NULL),false)
  FROM model_capability_routing_approvals a
  JOIN model_capability_probes p ON p.capability_probe_id=a.capability_probe_id
  JOIN model_capability_probe_contract_bindings contract
    ON (contract.operation_id,contract.capability_id,contract.execution_profile_id,contract.execution_profile_version,
        contract.protocol_profile_id,contract.protocol_profile_version,contract.probe_contract_id,contract.probe_contract_version)=
       (a.operation_id,a.capability_id,a.execution_profile_id,a.execution_profile_version,
        a.protocol_profile_id,a.protocol_profile_version,a.probe_contract_id,a.probe_contract_version)
  JOIN model_deployments d ON d.id=a.deployment_id AND d.connection_id=a.connection_id AND d.customer_org_id=a.customer_org_id
  JOIN provider_connections c ON c.id=d.connection_id
  JOIN model_registry model ON model.id=d.public_model_id AND model.org_id=a.customer_org_id
  JOIN model_deployment_operations o ON o.deployment_id=d.id AND o.operation_id=a.operation_id
  JOIN model_deployment_capabilities cap ON cap.deployment_id=d.id AND cap.operation_id=a.operation_id AND cap.capability_id=a.capability_id
 WHERE a.approval_id=$1 AND a.customer_org_id=$2 AND d.public_model_id=$3 AND a.deployment_id=$4
   AND a.operation_id=$5 AND a.execution_profile_id=$6 AND a.execution_profile_version=$7
   AND a.protocol_profile_id=$8 AND a.protocol_profile_version=$9`
	suffix := ""
	if lockMutable {
		suffix = " FOR SHARE OF cap"
	}
	var result CapabilityDecision
	result.Pin = CapabilityPin{DeploymentID: target.DeploymentID, OperationID: target.OperationID, ApprovalID: approvalID, DecisionContract: DecisionContract}
	var binding, evidence string
	var qualified bool
	var approvedAt time.Time
	if err := q.QueryRow(ctx, query+suffix, approvalID, target.CustomerOrgID, target.PublicModelID, target.DeploymentID, target.OperationID, target.ExecutionProfileID, target.ExecutionProfileVersion, target.ProtocolProfileID, target.ProtocolProfileVersion).Scan(
		&result.Pin.ProbeID, &result.Pin.CapabilityID, &result.Pin.ProbeContractID, &result.Pin.ProbeContractVersion, &approvedAt, &binding, &evidence, &qualified); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return CapabilityDecision{}, fmt.Errorf("%w: retained decision absent", ErrCapabilityUnavailable)
		}
		return CapabilityDecision{}, fmt.Errorf("read exact capability decision: %w", err)
	}
	if !qualified {
		return CapabilityDecision{}, fmt.Errorf("%w: pinned capability evidence is stale or current safety state denies execution", ErrCapabilityUnavailable)
	}
	var err error
	result.Pin.BindingDigest, err = canonicalDigest([]byte(binding))
	if err != nil {
		return CapabilityDecision{}, err
	}
	result.Pin.EvidenceDigest, err = canonicalDigest([]byte(evidence))
	if err != nil {
		return CapabilityDecision{}, err
	}
	if pinned == nil {
		var selector string
		if err := q.QueryRow(ctx, `SELECT jsonb_build_array('unirouter.capability-selector-revision.v1',s.operation_id,s.capability_id,s.execution_profile_id,s.execution_profile_version,s.protocol_profile_id,s.protocol_profile_version,s.probe_contract_id,s.probe_contract_version,to_char(s.updated_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'))::text
FROM model_capability_current_probe_contracts s WHERE operation_id=$1 AND capability_id=$2 AND execution_profile_id=$3
AND execution_profile_version=$4 AND protocol_profile_id=$5 AND protocol_profile_version=$6 AND probe_contract_id=$7 AND probe_contract_version=$8`,
			target.OperationID, result.Pin.CapabilityID, target.ExecutionProfileID, target.ExecutionProfileVersion, target.ProtocolProfileID, target.ProtocolProfileVersion, result.Pin.ProbeContractID, result.Pin.ProbeContractVersion).Scan(&selector); err != nil {
			return CapabilityDecision{}, fmt.Errorf("authoring selector differs from chosen decision: %w", err)
		}
		result.Pin.SelectorDigest, err = canonicalDigest([]byte(selector))
		if err != nil {
			return CapabilityDecision{}, err
		}
	} else {
		result.Pin.SelectorDigest = pinned.SelectorDigest
		if result.Pin != *pinned {
			return CapabilityDecision{}, fmt.Errorf("retained capability facts differ from sealed decision")
		}
	}
	approvedAt = approvedAt.UTC()
	result.Entry = executionevidence.CapabilityEntry{CapabilityID: result.Pin.CapabilityID, SupportState: "enabled", EvidenceState: "production_approved", EvidenceRef: "capability_approval:" + strconv.FormatInt(approvalID, 10), VerifiedAt: &approvedAt, Revision: approvalID}
	return result, nil
}

func canonicalDigest(wire []byte) (string, error) {
	canonical, err := strictjson.Canonicalize(wire)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

// ReadSnapshot checks the indexed Release pins against retained canonical bytes.
// A legacy Release is returned explicitly as nil; absence never synthesizes a
// new-format snapshot or silently upgrades historical evidence.
func ReadSnapshot(ctx context.Context, q Reader, releaseID int64) (*Snapshot, error) {
	expanded, err := schemaauthority.HasCapabilityExpand(ctx, q)
	if err != nil {
		return nil, err
	}
	if !expanded {
		return nil, nil
	}
	var wire []byte
	var digest, definition, format string
	var modelID int64
	var profileID, profileVersion, routeHash string
	var contextLimit, outputLimit int
	if err := q.QueryRow(ctx, `SELECT COALESCE(execution_snapshot_digest,''),execution_snapshot_bytes,COALESCE(profile_definition_digest,''),COALESCE(execution_snapshot_format,''),
COALESCE(public_model_id,0),COALESCE(execution_profile_id,''),COALESCE(execution_profile_version,''),COALESCE(public_context_limit,0),COALESCE(public_output_limit,0),COALESCE(route_items_hash,'')
FROM runtime_change_releases WHERE id=$1`, releaseID).Scan(&digest, &wire, &definition, &format, &modelID, &profileID, &profileVersion, &contextLimit, &outputLimit, &routeHash); err != nil {
		return nil, err
	}
	if digest == "" {
		if len(wire) > 0 || definition != "" || format != "" {
			return nil, fmt.Errorf("partial Release snapshot")
		}
		return nil, nil
	}
	s, err := VerifySnapshot(wire, digest)
	if err != nil {
		return nil, err
	}
	if s.PublicModelID != modelID || s.ProfileDefinitionDigest != definition || s.SchemaVersion != format || s.ExecutionProfileID != profileID || s.ExecutionProfileVersion != profileVersion || s.PublicContextLimit != contextLimit || s.PublicOutputLimit != outputLimit || s.RouteItemsHash != routeHash {
		return nil, fmt.Errorf("Release snapshot and indexed execution pins differ")
	}
	return &s, nil
}
