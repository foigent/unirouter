package runtimegeneration

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"

	"github.com/jackc/pgx/v5"
)

var upgradeAdmissionID = regexp.MustCompile(`^ucu_[a-f0-9]{32}-admit(-[a-f0-9]{32})?$`)

// AdmissionRecovery is accepted only by the signed single-host upgrade owner.
// HostProof names that owner's durable proxy and writer exit observations.
type AdmissionRecovery struct {
	ChangeID     string            `json:"change_id"`
	ConfigDigest string            `json:"configuration_sha256"`
	HostProof    string            `json:"host_proof_sha256"`
	Departed     []RetirementInput `json:"departed"`
}

type AdmissionWithdrawal struct {
	ChangeID string `json:"change_id"`
	Status   string `json:"status"`
}

// RecoverAdmissionTx runs under the upgrade owner's migration, release and
// execution locks. It has no host effects and cannot complete an admission.
func RecoverAdmissionTx(ctx context.Context, tx pgx.Tx, in AdmissionRecovery, apply bool) (AdmissionWithdrawal, error) {
	var result AdmissionWithdrawal
	if !upgradeAdmissionID.MatchString(in.ChangeID) || !hexDigest.MatchString(in.ConfigDigest) ||
		!hexDigest.MatchString(in.HostProof) || len(in.Departed) < 1 || len(in.Departed) > 2 ||
		(len(in.Departed) == 2 && in.Departed[0].InstanceID >= in.Departed[1].InstanceID) {
		return result, fmt.Errorf("exact upgrade admission and its departed writer proofs required")
	}
	result.ChangeID = in.ChangeID + "-withdrawn"
	observation, err := json.Marshal(struct {
		CancelledAdmission string            `json:"cancelled_admission"`
		HostProof          string            `json:"host_proof_sha256"`
		Departed           []RetirementInput `json:"departed"`
	}{in.ChangeID, in.HostProof, in.Departed})
	if err != nil {
		return result, err
	}
	admission, status, err := ReadIngress(ctx, tx, in.ChangeID)
	if err != nil {
		return result, err
	}
	var head string
	if err := tx.QueryRow(ctx, `SELECT c.change_id FROM runtime_ingress_changes c
	WHERE NOT EXISTS (SELECT 1 FROM runtime_ingress_changes n WHERE n.predecessor_id=c.change_id)`).Scan(&head); err != nil {
		return result, err
	}
	if admission.DesiredConfigDigest != in.ConfigDigest {
		return result, fmt.Errorf("admission recovery configuration changed")
	}
	if status == "cancelled" {
		var matches bool
		if err := tx.QueryRow(ctx, `SELECT status='applied' AND observation=$3::jsonb AND predecessor_id=$2
		FROM runtime_ingress_changes WHERE change_id=$1`, result.ChangeID, in.ChangeID, string(observation)).Scan(&matches); err != nil {
			return result, err
		}
		if !matches || head != result.ChangeID {
			return result, fmt.Errorf("admission withdrawal conflicts with the retained receipt or head")
		}
		result.Status = "applied"
		return result, nil
	}
	if (status != "pending" && status != "ambiguous") || head != in.ChangeID ||
		len(admission.Before) != 0 || len(admission.After) != len(in.Departed) {
		return result, fmt.Errorf("only the exact unresolved upgrade admission may be withdrawn")
	}
	predecessor, predecessorStatus, err := ReadIngress(ctx, tx, admission.PredecessorID)
	if err != nil {
		return result, err
	}
	if predecessorStatus != "applied" || len(predecessor.After) != 0 ||
		predecessor.DesiredConfigDigest != admission.ExpectedConfigDigest {
		return result, fmt.Errorf("original upgrade withdrawal does not match")
	}

	query := `SELECT g.instance_id,g.artifact_digest,g.ingress_state,g.revision,g.lease_expires_at<=clock_timestamp(),g.schema_head,g.compatibility_epoch,m.manifest_digest
	FROM runtime_generations g JOIN runtime_support_manifests m USING(artifact_digest) WHERE g.state<>'stopped' OR g.ingress_state<>'absent' ORDER BY g.instance_id`
	if apply {
		query += " FOR UPDATE OF g"
	}
	rows, err := tx.Query(ctx, query)
	if err != nil {
		return result, err
	}
	var current []Identity
	for rows.Next() {
		var id Identity
		var membership, schema, epoch, manifest string
		var revision int64
		var expired bool
		if err := rows.Scan(&id.Instance, &id.Artifact, &membership, &revision, &expired, &schema, &epoch, &manifest); err != nil {
			rows.Close()
			return result, err
		}
		i := len(current)
		if i >= len(in.Departed) || id != (Identity{in.Departed[i].InstanceID, in.Departed[i].ArtifactDigest}) ||
			membership != "pending_join" || !expired || revision != in.Departed[i].ExpectedRevision ||
			schema != in.Departed[i].ExpectedSchemaHead || epoch != in.Departed[i].CompatibilityEpoch || manifest != in.Departed[i].ManifestDigest {
			rows.Close()
			return result, fmt.Errorf("exact admission writers are not all withdrawn and lease-expired")
		}
		current = append(current, id)
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	if !slices.Equal(current, admission.After) {
		return result, fmt.Errorf("exact admission writer set changed")
	}
	for _, input := range in.Departed {
		if err := input.validateIdentity(); err != nil || input.ExpectedIngressHead != result.ChangeID {
			return result, fmt.Errorf("invalid admission retirement proof")
		}
		if !apply {
			if err := verifyRetirementAttempts(ctx, tx, input.InstanceID, nil); err != nil {
				return result, err
			}
		}
	}
	result.Status = "eligible"
	if !apply {
		return result, nil
	}

	_, err = tx.Exec(ctx, `UPDATE runtime_ingress_changes SET status='cancelled',completed_at=clock_timestamp()
	WHERE change_id=$1`, admission.ChangeID)
	if err != nil {
		return result, err
	}
	members, err := json.Marshal(admission.After)
	if err != nil {
		return result, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO runtime_ingress_changes(change_id,predecessor_id,expected_config_digest,
	desired_config_digest,desired_config_bytes,membership_before,membership_after,status,observation,completed_at)
	VALUES($1,$2,$3,$3,$4,$5,'[]','applied',$6,clock_timestamp())`,
		result.ChangeID, admission.ChangeID, in.ConfigDigest, admission.DesiredConfigBytes, members, observation)
	if err != nil {
		return result, err
	}
	for _, original := range in.Departed {
		_, err := tx.Exec(ctx, `UPDATE runtime_generations SET ingress_state='absent',revision=revision+1,
		updated_at=clock_timestamp() WHERE instance_id=$1`, original.InstanceID)
		if err != nil {
			return result, err
		}
		original.ExpectedRevision++
		if _, err := ApplyCommunityMaintenanceRetirementTx(ctx, tx, original, original.ExpectedSchemaHead); err != nil {
			return result, err
		}
	}
	result.Status = "applied"
	return result, nil
}
