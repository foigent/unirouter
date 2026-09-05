package modelcapabilitypublication

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/strictjson"
)

var ErrPublicationCommitAmbiguous = errors.New("capability publication commit is ambiguous; read exact ledger before another write")

type ApplyResultV2 struct {
	PublicationID string `json:"publication_id"`
	PlanDigest    string `json:"plan_digest"`
	Status        string `json:"status"`
}

// ApplyV2 owns all daily capability DML. It does not execute SQL files, mutate
// Release/Slot, dispatch Providers, or rewrite evidence. Recovery is read-only;
// a commit error never causes this method to repeat the write automatically.
func ApplyV2(ctx context.Context, pool *pgxpool.Pool, a AuthorityV2, r PlanRequestV2, approved PlanV2) (ApplyResultV2, error) {
	return applyV2(ctx, pool, a, r, approved, func(ctx context.Context, tx pgx.Tx) error { return tx.Commit(ctx) })
}

// The transaction result boundary is injectable only from package tests. All
// production calls commit through pgx, with the same single writer/recovery path.
func applyV2(ctx context.Context, pool *pgxpool.Pool, a AuthorityV2, r PlanRequestV2, approved PlanV2, commit func(context.Context, pgx.Tx) error) (ApplyResultV2, error) {
	var result ApplyResultV2
	approvedBytes, err := approved.CanonicalBytes()
	if err != nil {
		return result, err
	}
	digest := sha256Hex(approvedBytes)
	result = ApplyResultV2{approved.PublicationID, digest, "pending"}
	err = migrate.WithAuthorityLock(ctx, pool, func(conn *pgxpool.Conn) error {
		// Schema/static writers are already serialized by the session owner.
		// READ COMMITTED starts business reads after the compatibility lock is
		// obtained, including a join which committed while this writer waited.
		tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		// The established domain lock also excludes concurrent joins while checking
		// the receiving fleet. It is acquired before reading any business state.
		if err := schemaauthority.LockExecutionCompatibility(ctx, tx); err != nil {
			return err
		}
		if err := validateAuthorityV2(a, r.Kind != "legacy_import"); err != nil {
			return err
		}
		var retained []byte
		err = tx.QueryRow(ctx, `SELECT plan_bytes FROM capability_publications WHERE publication_id=$1`, approved.PublicationID).Scan(&retained)
		if err == nil {
			if !bytes.Equal(retained, approvedBytes) {
				return fmt.Errorf("%w: publication ID belongs to another Plan", ErrPlanConflict)
			}
			if err := verifyRecoveryIdentityV2(ctx, tx, a, approved); err != nil {
				return err
			}
			if err := VerifyPublicationProjectionV2(ctx, tx); err != nil {
				return err
			}
			result.Status = "already_committed"
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		fresh, err := BuildPlanV2(ctx, tx, a, r)
		if err != nil {
			return err
		}
		freshBytes, err := fresh.CanonicalBytes()
		if err != nil {
			return err
		}
		if !bytes.Equal(freshBytes, approvedBytes) {
			return fmt.Errorf("%w: exact delta Plan changed", ErrPlanConflict)
		}
		if err := checkPublicationFleetV2(ctx, tx); err != nil {
			return err
		}
		target, err := json.Marshal(approved.Target)
		if err != nil {
			return err
		}
		publisher, err := json.Marshal(approved.Publisher)
		if err != nil {
			return err
		}
		var bundleBytes any
		if approved.BundleDigest != "" {
			bundleBytes = r.BundleBytes
		}
		_, err = tx.Exec(ctx, `INSERT INTO capability_publications(publication_id,predecessor_id,kind,legacy_through,legacy_projection_digest,
expected_projection_digest,result_projection_digest,plan_digest,plan_bytes,bundle_digest,bundle_bytes,materializer_contract,target_identity,publisher_identity)
VALUES($1,NULLIF($2,''),$3,NULLIF($4,''),NULLIF($5,''),$6,$7,$8,$9,NULLIF($10,''),$11,$12,$13,$14)`,
			approved.PublicationID, approved.ExpectedPredecessor, approved.Kind, approved.LegacyThrough, approved.LegacyProjectionDigest,
			approved.ExpectedProjectionDigest, approved.ResultProjectionDigest, digest, approvedBytes, approved.BundleDigest, bundleBytes, approved.MaterializerContract, target, publisher)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT set_config('unirouter.capability_publication_id',$1,true)`, approved.PublicationID); err != nil {
			return err
		}
		if approved.BundleDigest != "" {
			bundle, err := VerifyBundleV2(r.BundleBytes, approved.BundleDigest)
			if err != nil {
				return err
			}
			if err := materializeDeltaV2(ctx, tx, approved.PublicationID, bundle); err != nil {
				return err
			}
		}
		if err := VerifyPublicationProjectionV2(ctx, tx); err != nil {
			return err
		}
		if err := commit(ctx, tx); err != nil {
			return fmt.Errorf("%w: %v", ErrPublicationCommitAmbiguous, err)
		}
		result.Status = "committed"
		return nil
	})
	if !errors.Is(err, ErrPublicationCommitAmbiguous) {
		return result, err
	}
	// A separate bounded read uses a fresh connection/snapshot. No Provider or
	// financial retry semantics are inferred from a successful publication read.
	recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	recovered, recoveryErr := RecoverV2(recoveryCtx, pool, a, approved)
	if recoveryErr == nil {
		return recovered, nil
	}
	return result, fmt.Errorf("%w; recovery: %v", err, recoveryErr)
}

func RecoverV2(ctx context.Context, pool *pgxpool.Pool, a AuthorityV2, approved PlanV2) (ApplyResultV2, error) {
	result := ApplyResultV2{PublicationID: approved.PublicationID, Status: "unconfirmed"}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	if err := verifyRecoveryIdentityV2(ctx, tx, a, approved); err != nil {
		return result, err
	}
	wire, err := approved.CanonicalBytes()
	if err != nil {
		return result, err
	}
	result.PlanDigest = sha256Hex(wire)
	var actual []byte
	if err := tx.QueryRow(ctx, `SELECT plan_bytes FROM capability_publications WHERE publication_id=$1`, approved.PublicationID).Scan(&actual); err != nil {
		return result, err
	}
	if !bytes.Equal(actual, wire) {
		return result, fmt.Errorf("publication recovery Plan differs")
	}
	if err := VerifyPublicationProjectionV2(ctx, tx); err != nil {
		return result, err
	}
	result.Status = "committed_readback"
	return result, nil
}
func verifyRecoveryIdentityV2(ctx context.Context, tx pgx.Tx, a AuthorityV2, p PlanV2) error {
	identity, err := readTargetIdentity(ctx, tx, a.Environment)
	if err != nil {
		return err
	}
	if identity != p.Target {
		return fmt.Errorf("publication recovery Target mismatch")
	}
	ledger, err := readLedger(ctx, tx)
	if err != nil {
		return err
	}
	return schemaauthority.VerifyRuntimeLedger(a.ArtifactMigrationLedger, ledger)
}

// Candidate publication needs only the known storage/projection reader on
// potential receivers, not every candidate's wire implementation. Expired and
// withdrawing entries remain blockers until explicit ingress withdrawal closes.
// The caller holds the domain lock against authority changes; do not row-lock
// generations, because their unchanged heartbeats must remain able to renew.
func checkPublicationFleetV2(ctx context.Context, tx pgx.Tx) error {
	rows, err := tx.Query(ctx, `SELECT g.state,g.lease_expires_at>clock_timestamp(),g.compatibility_epoch,
 m.manifest_digest,m.canonical_bytes FROM runtime_generations g JOIN runtime_support_manifests m USING(artifact_digest)
 WHERE g.ingress_state<>'absent'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var state, epoch, digest string
		var live bool
		var wire []byte
		if err := rows.Scan(&state, &live, &epoch, &digest, &wire); err != nil {
			return err
		}
		var manifest struct {
			Schema          string                                 `json:"schema_version"`
			Epoch           string                                 `json:"compatibility_epoch"`
			Heads           []string                               `json:"schema_heads"`
			Implementations []modelprofile.ImplementationReference `json:"implementations"`
		}
		if err := decodeStrict(wire, &manifest); err != nil {
			return err
		}
		canonical, err := strictjson.Canonicalize(wire)
		if err != nil || !bytes.Equal(canonical, wire) || sha256Hex(wire) != digest || manifest.Schema != "unirouter.runtime-support.v1" ||
			manifest.Epoch != epoch || epoch != modelprofile.DefinitionRuntimeEpoch || !live || (state != "serving" && state != "draining") ||
			!slices.Contains(manifest.Heads, schemaauthority.CapabilityExpandHead) ||
			!slices.Contains(manifest.Implementations, modelprofile.ImplementationReference{Kind: "capability_projection", ID: ProjectionSchemaV2}) {
			return fmt.Errorf("potential ingress receiver cannot safely read capability publications")
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("online publication requires explicit proved ingress membership")
	}
	return nil
}

func materializeDeltaV2(ctx context.Context, tx pgx.Tx, id string, bundle BundleV2) error {
	definitions := map[string]modelprofile.Definition{}
	for _, wire := range bundle.Definitions {
		d, err := modelprofile.DecodeDefinition(wire)
		if err != nil {
			return err
		}
		definitions[d.Digest()] = d
	}
	visited := map[string]bool{}
	done := map[string]bool{}
	var insertDefinition func(string) error
	insertDefinition = func(digest string) error {
		d, found := definitions[digest]
		if !found || done[digest] {
			return nil
		}
		if visited[digest] {
			return fmt.Errorf("cyclic definition predecessor")
		}
		visited[digest] = true
		if err := insertDefinition(d.PredecessorDigest()); err != nil {
			return err
		}
		p := d.Profile()
		if _, err := tx.Exec(ctx, `INSERT INTO model_profile_definitions(definition_digest,profile_id,profile_version,product_lineage_id,predecessor_digest,schema_version,canonical_bytes,publication_id)
VALUES($1,$2,$3,$4,NULLIF($5,''),$6,$7,$8)`, d.Digest(), p.ID, p.Version, d.ProductLineageID(), d.PredecessorDigest(), modelprofile.DefinitionSchemaV1, d.CanonicalBytes(), id); err != nil {
			return err
		}
		done[digest] = true
		return nil
	}
	// Bundle canonical order is fixed; dependencies are inserted before children.
	for _, wire := range bundle.Definitions {
		d, _ := modelprofile.DecodeDefinition(wire)
		if err := insertDefinition(d.Digest()); err != nil {
			return err
		}
	}
	for _, b := range bundle.BindingDelta {
		if _, err := tx.Exec(ctx, `INSERT INTO model_capability_probe_contract_bindings(operation_id,capability_id,execution_profile_id,execution_profile_version,protocol_profile_id,protocol_profile_version,probe_contract_id,probe_contract_version,expected_provider,expected_upstream_model,public_path,introduced_migration,introduced_publication_id,profile_definition_digest)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NULL,$12,$13)`, b.OperationID, b.CapabilityID, b.ExecutionProfileID, b.ExecutionProfileVersion, b.ProtocolProfileID, b.ProtocolProfileVersion, b.ProbeContractID, b.ProbeContractVersion, b.ExpectedProvider, b.ExpectedUpstreamModel, b.PublicPath, id, b.ProfileDefinitionDigest); err != nil {
			return err
		}
	}
	for _, s := range bundle.SelectorDelta {
		args := []any{s.OperationID, s.CapabilityID, s.ExecutionProfileID, s.ExecutionProfileVersion, s.ProtocolProfileID, s.ProtocolProfileVersion, s.Final.ProbeContractID, s.Final.ProbeContractVersion}
		if s.ExpectedPredecessor == nil {
			tag, err := tx.Exec(ctx, `INSERT INTO model_capability_current_probe_contracts(operation_id,capability_id,execution_profile_id,execution_profile_version,protocol_profile_id,protocol_profile_version,probe_contract_id,probe_contract_version)
VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT DO NOTHING`, args...)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return fmt.Errorf("%w: selector absent CAS failed", ErrPlanConflict)
			}
		} else {
			args = append(args, s.ExpectedPredecessor.ProbeContractID, s.ExpectedPredecessor.ProbeContractVersion)
			tag, err := tx.Exec(ctx, `UPDATE model_capability_current_probe_contracts SET probe_contract_id=$7,probe_contract_version=$8,updated_at=clock_timestamp()
WHERE operation_id=$1 AND capability_id=$2 AND execution_profile_id=$3 AND execution_profile_version=$4 AND protocol_profile_id=$5 AND protocol_profile_version=$6 AND probe_contract_id=$9 AND probe_contract_version=$10`, args...)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return fmt.Errorf("%w: selector predecessor CAS failed", ErrPlanConflict)
			}
		}
	}
	return nil
}
