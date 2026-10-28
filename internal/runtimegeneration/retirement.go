package runtimegeneration

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uniroute/unirouter/internal/schemaauthority"
)

// RetirementInput binds one host-proved exit to the exact retained database
// identity. HostProofSHA256 is produced by the existing host owner; the database
// owner independently verifies every predicate it can observe.
type RetirementInput struct {
	InstanceID               string `json:"instance_id"`
	ArtifactDigest           string `json:"artifact_digest"`
	ManifestDigest           string `json:"manifest_digest"`
	CompatibilityEpoch       string `json:"compatibility_epoch"`
	ExpectedSchemaHead       string `json:"expected_schema_head"`
	ExpectedRevision         int64  `json:"expected_revision"`
	ExpectedIngressHead      string `json:"expected_ingress_head"`
	ExpectedEnvironment      string `json:"expected_environment"`
	ExpectedDatabaseSystemID string `json:"expected_database_system_id"`
	HostProofSHA256          string `json:"host_proof_sha256"`
}

type RetirementResult struct {
	Status          string `json:"status"`
	InstanceID      string `json:"instance_id"`
	Revision        int64  `json:"revision"`
	AttemptCount    int64  `json:"attempt_count"`
	HostProofSHA256 string `json:"host_proof_sha256"`
}

var decimalSystemID = regexp.MustCompile(`^[0-9]+$`)
var hexDigest = regexp.MustCompile(`^[a-f0-9]{64}$`)
var historicalSchemaHead = regexp.MustCompile(`^000[0-9]{3}_[a-z0-9_]+$`)

func (r RetirementInput) validate() error {
	if err := r.validateIdentity(); err != nil {
		return err
	}
	if r.ExpectedEnvironment != "development" && r.ExpectedEnvironment != "test" {
		return fmt.Errorf("exact historical retirement identity and host proof required")
	}
	return nil
}

func (r RetirementInput) validateIdentity() error {
	identity := Identity{Instance: r.InstanceID, Artifact: r.ArtifactDigest}
	if err := identity.Validate(); err != nil {
		return err
	}
	if !hexDigest.MatchString(r.ManifestDigest) ||
		r.CompatibilityEpoch != CompatibilityEpoch ||
		!historicalSchemaHead.MatchString(r.ExpectedSchemaHead) ||
		r.ExpectedRevision < 1 || r.ExpectedIngressHead == "" ||
		!decimalSystemID.MatchString(r.ExpectedDatabaseSystemID) ||
		!artifactPattern.MatchString(r.HostProofSHA256) {
		return fmt.Errorf("exact historical retirement identity and host proof required")
	}
	return nil
}

type retirementReader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readRetirement(ctx context.Context, q retirementReader, r RetirementInput, forUpdate bool) (RetirementResult, error) {
	result := RetirementResult{Status: "eligible", InstanceID: r.InstanceID, Revision: r.ExpectedRevision, HostProofSHA256: r.HostProofSHA256}
	var systemID string
	if err := q.QueryRow(ctx, `SELECT system_identifier::text FROM pg_control_system()`).Scan(&systemID); err != nil {
		return result, err
	}
	if systemID != r.ExpectedDatabaseSystemID {
		return result, fmt.Errorf("retirement database identity changed")
	}
	query := `SELECT g.artifact_digest,g.compatibility_epoch,g.schema_head,g.state,g.ingress_state,
	 g.lease_expires_at<=clock_timestamp(),g.revision,m.manifest_digest,m.canonical_bytes
	 FROM runtime_generations g JOIN runtime_support_manifests m USING(artifact_digest)
	 WHERE g.instance_id=$1`
	if forUpdate {
		query += ` FOR UPDATE OF g`
	}
	var artifact, epoch, head, state, ingress, manifestDigest string
	var expired bool
	var revision int64
	var manifestWire []byte
	if err := q.QueryRow(ctx, query, r.InstanceID).Scan(&artifact, &epoch, &head, &state, &ingress, &expired, &revision, &manifestDigest, &manifestWire); err != nil {
		return result, err
	}
	if artifact != r.ArtifactDigest || epoch != r.CompatibilityEpoch || head != r.ExpectedSchemaHead ||
		manifestDigest != r.ManifestDigest || revision != r.ExpectedRevision || state == "stopped" ||
		ingress != "absent" || !expired {
		return result, fmt.Errorf("historical generation predecessor changed or is still active")
	}
	manifest, err := VerifyManifest(manifestWire, manifestDigest)
	if err != nil {
		return result, err
	}
	if manifest.CompatibilityEpoch != epoch || !slices.Contains(manifest.SchemaHeads, head) {
		return result, fmt.Errorf("historical generation manifest does not support its original head")
	}
	var ingressHead, ingressStatus string
	var afterWire []byte
	if err := q.QueryRow(ctx, `SELECT c.change_id,c.status,c.membership_after FROM runtime_ingress_changes c
	 WHERE NOT EXISTS(SELECT 1 FROM runtime_ingress_changes n WHERE n.predecessor_id=c.change_id)`).Scan(&ingressHead, &ingressStatus, &afterWire); err != nil {
		return result, err
	}
	if ingressHead != r.ExpectedIngressHead || ingressStatus != "applied" {
		return result, fmt.Errorf("retirement ingress head is not the exact applied predecessor")
	}
	var members []Identity
	if err := json.Unmarshal(afterWire, &members); err != nil {
		return result, err
	}
	for _, member := range members {
		if member.Instance == r.InstanceID {
			return result, fmt.Errorf("retiring generation remains an ingress receiver")
		}
	}
	var unresolved int64
	if err := q.QueryRow(ctx, `SELECT count(*)::bigint,
	 count(*) FILTER (WHERE (
	   (a.state='settled' AND a.financial_resolved_at IS NOT NULL AND f.status='settled') OR
	   (a.state='reconciled' AND a.financial_resolved_at IS NOT NULL AND f.status='cancelled')
	 ) IS NOT TRUE)::bigint
	 FROM billing_provider_attempts a LEFT JOIN freeze_idempotency f
	 ON f.org_id=a.org_id AND f.request_id=a.request_id
	 WHERE a.runtime_instance_id=$1`, r.InstanceID).Scan(&result.AttemptCount, &unresolved); err != nil {
		return result, err
	}
	if unresolved != 0 {
		return result, fmt.Errorf("historical generation has unresolved financial attempts")
	}
	return result, nil
}

// CheckRetirement performs no writes. Apply repeats every predicate under the
// compatibility domain lock and a row lock; a stale check grants no authority.
func CheckRetirement(ctx context.Context, pool *pgxpool.Pool, r RetirementInput) (RetirementResult, error) {
	if err := r.validate(); err != nil {
		return RetirementResult{}, err
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return RetirementResult{}, err
	}
	defer tx.Rollback(context.Background())
	result, err := readRetirement(ctx, tx, r, false)
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func ApplyRetirement(ctx context.Context, pool *pgxpool.Pool, r RetirementInput) (RetirementResult, error) {
	if err := r.validate(); err != nil {
		return RetirementResult{}, err
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return RetirementResult{}, err
	}
	defer tx.Rollback(context.Background())
	if err := schemaauthority.LockExecutionCompatibility(ctx, tx); err != nil {
		return RetirementResult{}, err
	}
	result, err := readRetirement(ctx, tx, r, true)
	if err != nil {
		return result, err
	}
	result, err = applyRetirementTx(ctx, tx, r, result)
	if err != nil {
		return result, err
	}
	if err := tx.Commit(ctx); err != nil {
		return result, fmt.Errorf("retirement commit unconfirmed: %w", err)
	}
	return result, nil
}

func applyRetirementTx(ctx context.Context, tx pgx.Tx, r RetirementInput, result RetirementResult) (RetirementResult, error) {
	tag, err := tx.Exec(ctx, `UPDATE runtime_generations SET state='stopped',revision=revision+1,updated_at=clock_timestamp()
	 WHERE instance_id=$1 AND artifact_digest=$2 AND revision=$3 AND state<>'stopped'
	 AND ingress_state='absent' AND lease_expires_at<=clock_timestamp()`, r.InstanceID, r.ArtifactDigest, r.ExpectedRevision)
	if err != nil {
		return result, err
	}
	if tag.RowsAffected() != 1 {
		return result, fmt.Errorf("historical generation changed before stop")
	}
	result.Status = "stopped"
	result.Revision++
	return result, nil
}

// CheckM127MaintenanceRetirementTx and ApplyM127MaintenanceRetirementTx are
// transaction participants of the signed community upgrade owner. That owner
// verifies the installed package, configured trust ring and exact M127 -> M128
// candidate under its release/migration locks before calling these functions.
// They do not commit, grant serving authority or widen legacy stop commands.
func CheckM127MaintenanceRetirementTx(ctx context.Context, tx pgx.Tx, r RetirementInput, forUpdate bool) (RetirementResult, error) {
	if err := r.validateIdentity(); err != nil {
		return RetirementResult{}, err
	}
	if r.ExpectedSchemaHead != schemaauthority.XInsightCommunicationHead ||
		(r.ExpectedEnvironment != "production" && r.ExpectedEnvironment != "development" && r.ExpectedEnvironment != "test") {
		return RetirementResult{}, fmt.Errorf("community retirement requires the exact M127 maintenance predecessor")
	}
	return readRetirement(ctx, tx, r, forUpdate)
}

func ApplyM127MaintenanceRetirementTx(ctx context.Context, tx pgx.Tx, r RetirementInput) (RetirementResult, error) {
	result, err := CheckM127MaintenanceRetirementTx(ctx, tx, r, true)
	if err != nil {
		return result, err
	}
	return applyRetirementTx(ctx, tx, r, result)
}
