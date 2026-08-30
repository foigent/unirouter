package modelcapabilitypublication

import (
	"context"
	"fmt"
)

// verifyNoRetainedPublicationEvidence rejects guarded downgrade when any
// immutable Probe, Approval, provider-attempt, or execution Freeze evidence
// still exists. Freeze snapshots do not expose a reversible capability tuple,
// so their downgrade boundary is deliberately global and conservative.
func verifyNoRetainedPublicationEvidence(ctx context.Context, q rowQuerier, declaration Declaration) error {
	var frozen int64
	if err := q.QueryRow(ctx, `SELECT
    (SELECT COUNT(*) FROM freeze_idempotency WHERE execution_snapshot_hash IS NOT NULL) +
    (SELECT COUNT(*) FROM billing_provider_attempts WHERE execution_snapshot_hash IS NOT NULL)`).Scan(&frozen); err != nil {
		return fmt.Errorf("check retained execution Freeze evidence: %w", err)
	}
	if frozen != 0 {
		return fmt.Errorf("%w: retained execution Freeze evidence prevents publication down", ErrPlanConflict)
	}
	for _, binding := range declaration.BindingAppends {
		args := []any{
			binding.OperationID, binding.CapabilityID,
			binding.ExecutionProfileID, binding.ExecutionProfileVersion,
			binding.ProtocolProfileID, binding.ProtocolProfileVersion,
			binding.ProbeContractID, binding.ProbeContractVersion,
		}
		var probes, approvals int64
		if err := q.QueryRow(ctx, `SELECT COUNT(*) FROM model_capability_probes
WHERE operation_id=$1 AND capability_id=$2
  AND execution_profile_id=$3 AND execution_profile_version=$4
  AND protocol_profile_id=$5 AND protocol_profile_version=$6
  AND probe_contract_id=$7 AND probe_contract_version=$8`, args...).Scan(&probes); err != nil {
			return fmt.Errorf("check retained capability Probes: %w", err)
		}
		if err := q.QueryRow(ctx, `SELECT COUNT(*) FROM model_capability_routing_approvals
WHERE operation_id=$1 AND capability_id=$2
  AND execution_profile_id=$3 AND execution_profile_version=$4
  AND protocol_profile_id=$5 AND protocol_profile_version=$6
  AND probe_contract_id=$7 AND probe_contract_version=$8`, args...).Scan(&approvals); err != nil {
			return fmt.Errorf("check retained capability Approvals: %w", err)
		}
		if probes != 0 || approvals != 0 {
			return fmt.Errorf("%w: retained Probe/Approval prevents publication down", ErrPlanConflict)
		}
		contractRef := "capability_contract:" + binding.ProbeContractID + "@" + binding.ProbeContractVersion
		var attempts int64
		if err := q.QueryRow(ctx, `SELECT COUNT(*) FROM billing_provider_attempts
WHERE route_capability_evidence @> jsonb_build_array(jsonb_build_object(
    'capability_id',$1::text,'evidence_ref',$2::text))`, binding.CapabilityID, contractRef).Scan(&attempts); err != nil {
			return fmt.Errorf("check retained provider-attempt capability evidence: %w", err)
		}
		if attempts != 0 {
			return fmt.Errorf("%w: retained provider-attempt capability evidence prevents publication down", ErrPlanConflict)
		}
	}
	return nil
}
