package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/installstate"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/signing"
	"github.com/uniroute/unirouter/internal/strictjson"
	"github.com/uniroute/unirouter/internal/upgradebundle"
)

const communityMaintenanceSchema = "unirouter.community-maintenance.v1"

// This is a binding document, never a reusable approval. In particular, it
// cannot supply public keys, change the configured environment or admit a
// predecessor other than the one named by this artifact.
type communityMaintenanceInput struct {
	SchemaVersion                   string                              `json:"schema_version"`
	InstallationID                  string                              `json:"installation_id"`
	InstallationPlanDigest          string                              `json:"installation_plan_digest"`
	SourcePackage                   string                              `json:"source_package"`
	CandidatePackage                string                              `json:"candidate_package"`
	ExpectedSequence                int64                               `json:"expected_sequence"`
	ExpectedSourcePackageSHA256     string                              `json:"expected_source_package_sha256"`
	ExpectedSourceManifestSHA256    string                              `json:"expected_source_manifest_sha256"`
	ExpectedCandidatePackageSHA256  string                              `json:"expected_candidate_package_sha256"`
	ExpectedCandidateManifestSHA256 string                              `json:"expected_candidate_manifest_sha256"`
	ExpectedTrustedRingSHA256       string                              `json:"expected_trusted_ring_sha256"`
	ExpectedEnvironment             string                              `json:"expected_environment"`
	ExpectedDatabaseSystemID        string                              `json:"expected_database_system_id"`
	ExpectedDatabase                string                              `json:"expected_database"`
	ExpectedDatabaseOID             int64                               `json:"expected_database_oid"`
	ExpectedIngressHead             string                              `json:"expected_ingress_head"`
	Retirements                     []runtimegeneration.RetirementInput `json:"retirements"`
}

type communityMaintenanceResult struct {
	SchemaVersion          string                               `json:"schema_version"`
	Status                 string                               `json:"status"`
	SourceSequence         int64                                `json:"source_sequence"`
	CandidateSequence      int64                                `json:"candidate_sequence"`
	CandidatePackageSHA256 string                               `json:"candidate_package_sha256"`
	TrustedRingSHA256      string                               `json:"trusted_ring_sha256"`
	Retirements            []runtimegeneration.RetirementResult `json:"retirements"`
}

func communityManifestSHA256(m upgradebundle.Manifest) string {
	raw, _ := json.Marshal(m)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func communityTrustRingSHA256(encoded string) (string, error) {
	if strictjson.Validate([]byte(encoded)) != nil {
		return "", fmt.Errorf("configured installation trust ring is not strict JSON")
	}
	ring, err := signing.ParsePublicKeyRing(encoded, "UPGRADE_PUBLIC_KEYS")
	if err != nil || len(ring) == 0 {
		return "", fmt.Errorf("community maintenance requires the existing installation's configured UPGRADE_PUBLIC_KEYS")
	}
	// Parsed key bytes give one identity regardless of equivalent base64/hex
	// encodings or JSON key ordering. Public keys are never printed here.
	raw, err := json.Marshal(ring)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func (in communityMaintenanceInput) validate(cfg *config.Config) error {
	if cfg == nil || in.SchemaVersion != communityMaintenanceSchema ||
		in.InstallationID == "" || in.InstallationPlanDigest == "" ||
		in.SourcePackage == "" || in.CandidatePackage == "" || in.SourcePackage == in.CandidatePackage ||
		in.ExpectedSequence < 1 || in.ExpectedEnvironment != cfg.RuntimeEnvironment ||
		(in.ExpectedEnvironment != "production" && in.ExpectedEnvironment != "development" && in.ExpectedEnvironment != "test") ||
		in.ExpectedDatabaseSystemID == "" || in.ExpectedDatabase == "" || in.ExpectedDatabaseOID <= 0 || in.ExpectedIngressHead == "" || len(in.Retirements) > 32 {
		return fmt.Errorf("exact community maintenance installation and finite transition required")
	}
	for _, s := range []string{in.InstallationPlanDigest, in.ExpectedSourcePackageSHA256, in.ExpectedSourceManifestSHA256, in.ExpectedCandidatePackageSHA256, in.ExpectedCandidateManifestSHA256, in.ExpectedTrustedRingSHA256} {
		raw, err := hex.DecodeString(s)
		if err != nil || len(raw) != sha256.Size || hex.EncodeToString(raw) != s {
			return fmt.Errorf("exact community maintenance digests required")
		}
	}
	for _, name := range []string{in.SourcePackage, in.CandidatePackage} {
		info, err := os.Lstat(name)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("community maintenance packages must be retained regular files")
		}
	}
	seen := make(map[string]bool, len(in.Retirements))
	for _, r := range in.Retirements {
		if seen[r.InstanceID] || r.ExpectedIngressHead != in.ExpectedIngressHead || r.ExpectedEnvironment != in.ExpectedEnvironment || r.ExpectedDatabaseSystemID != in.ExpectedDatabaseSystemID || r.ExpectedSchemaHead != schemaauthority.XInsightCommunicationHead {
			return fmt.Errorf("community retirement binding differs from the exact installation")
		}
		seen[r.InstanceID] = true
	}
	return nil
}

// verifyCommunityMaintenancePackages anchors the configured signing key to the
// unchanged installed package digest AND its recorded historical signature.
// A new valid signature from an unrelated key/ring cannot retire production.
// The incoming package is checked at current time; only historical source
// verification uses its actual database-recorded admission time.
func verifyCommunityMaintenancePackages(in communityMaintenanceInput, cfg *config.Config, state installedReleaseState, recorded upgradebundle.Manifest, recordedAt time.Time, now time.Time) (upgradebundle.Verification, string, error) {
	var zero upgradebundle.Verification
	ringSHA, err := communityTrustRingSHA256(cfg.UpgradePublicKeys)
	if err != nil {
		return zero, "", err
	}
	if ringSHA != in.ExpectedTrustedRingSHA256 {
		return zero, "", fmt.Errorf("installed trust ring binding changed")
	}
	if state.ReleaseSequence != in.ExpectedSequence || state.PackageSHA256 != in.ExpectedSourcePackageSHA256 ||
		state.TargetSchema != schemaauthority.XInsightCommunicationHead || state.TrustedKeyID == "" ||
		recorded.ReleaseSequence != state.ReleaseSequence || recorded.Version != state.Version || recorded.Commit != state.Commit ||
		recorded.TargetSchema != state.TargetSchema || recorded.KeyID != state.TrustedKeyID ||
		communityManifestSHA256(recorded) != in.ExpectedSourceManifestSHA256 {
		return zero, "", fmt.Errorf("installed M127 release binding changed")
	}
	source, err := verifyInstalledPackage(in.SourcePackage, cfg.UpgradePublicKeys, recorded.MinCurrentSequence, recorded.TargetSchema, recordedAt, 0)
	if err != nil {
		return zero, "", fmt.Errorf("retained installed signature: %w", err)
	}
	if source.BundleSHA256 != state.PackageSHA256 || !reflect.DeepEqual(source.Manifest, recorded) {
		return zero, "", fmt.Errorf("retained installed package does not match native release history")
	}
	candidate, err := verifyInstalledPackage(in.CandidatePackage, cfg.UpgradePublicKeys, state.ReleaseSequence, state.TargetSchema, now, 0)
	if err != nil {
		return zero, "", err
	}
	m := candidate.Manifest
	if candidate.BundleSHA256 != in.ExpectedCandidatePackageSHA256 || communityManifestSHA256(m) != in.ExpectedCandidateManifestSHA256 ||
		m.KeyID != state.TrustedKeyID ||
		m.MinCurrentSchema != schemaauthority.XInsightCommunicationHead || m.TargetSchema != schemaauthority.UpstreamReleaseObservationHead ||
		m.Version != cfg.RuntimeVersion || m.Commit != cfg.RuntimeCommit {
		return zero, "", fmt.Errorf("candidate must bind this installed key and exact M127 to M128 artifact")
	}
	return candidate, ringSHA, nil
}

func runCommunityMaintenance(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, in communityMaintenanceInput, apply bool) (communityMaintenanceResult, error) {
	var result communityMaintenanceResult
	if apply && len(in.Retirements) == 0 {
		return result, fmt.Errorf("maintenance-retire requires a nonempty exact writer batch; use maintenance-check for readback")
	}
	if err := in.validate(cfg); err != nil {
		return result, err
	}
	options := pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}
	if apply {
		options = pgx.TxOptions{IsoLevel: pgx.ReadCommitted}
	}
	tx, err := pool.BeginTx(ctx, options)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(context.Background())
	if apply {
		if err = migrate.LockMaintenanceTx(ctx, tx); err != nil {
			return result, err
		}
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('unirouter.release_state',0))`); err != nil {
			return result, err
		}
		if err = schemaauthority.LockExecutionCompatibility(ctx, tx); err != nil {
			return result, err
		}
		if _, err = tx.Exec(ctx, `LOCK TABLE schema_migrations IN SHARE MODE`); err != nil {
			return result, err
		}
	}
	snapshot, err := installstate.RequireOrdinary(ctx, tx)
	if err != nil {
		return result, err
	}
	artifact, err := schemaauthority.ExpectedLedger(cfg.MigrationsDir)
	if err != nil {
		return result, err
	}
	if len(artifact) < 2 || artifact[len(artifact)-1] != schemaauthority.UpstreamReleaseObservationHead ||
		!slices.Equal(snapshot.Ledger, artifact[:len(artifact)-1]) || snapshot.Record == nil || snapshot.Record.State != "completed" ||
		snapshot.Record.Plan.InstallationID != in.InstallationID || snapshot.Record.Plan.Digest != in.InstallationPlanDigest {
		return result, fmt.Errorf("community maintenance requires the exact complete M127 installation")
	}
	var systemID, database string
	var databaseOID int64
	if err = tx.QueryRow(ctx, `SELECT (SELECT system_identifier::text FROM pg_control_system()),current_database(),oid::bigint FROM pg_database WHERE datname=current_database()`).Scan(&systemID, &database, &databaseOID); err != nil {
		return result, err
	}
	if systemID != in.ExpectedDatabaseSystemID || database != in.ExpectedDatabase || databaseOID != in.ExpectedDatabaseOID {
		return result, fmt.Errorf("community maintenance database identity changed")
	}
	state, err := readInstalledReleaseState(ctx, tx, apply)
	if err != nil {
		return result, err
	}
	var recordedRaw, stateRaw []byte
	var recordedAt time.Time
	if err = tx.QueryRow(ctx, `SELECT h.manifest,h.recorded_at,s.manifest FROM release_history h JOIN release_state s ON s.release_sequence=h.release_sequence AND s.singleton=true WHERE h.release_sequence=$1 AND h.package_sha256=$2 AND h.version=s.version AND h.commit=s.commit AND h.target_schema=s.target_schema AND h.trusted_key_id=s.trusted_key_id`, state.ReleaseSequence, state.PackageSHA256).Scan(&recordedRaw, &recordedAt, &stateRaw); err != nil {
		return result, err
	}
	var recorded, stateManifest upgradebundle.Manifest
	if json.Unmarshal(recordedRaw, &recorded) != nil || json.Unmarshal(stateRaw, &stateManifest) != nil || !reflect.DeepEqual(recorded, stateManifest) {
		return result, fmt.Errorf("installed release singleton and history disagree")
	}
	candidate, ringSHA, err := verifyCommunityMaintenancePackages(in, cfg, state, recorded, recordedAt, time.Now().UTC())
	if err != nil {
		return result, err
	}
	// Refuse an incomplete fleet witness. Retirement remains per exact native
	// identity, but all listed transitions commit together or all roll back.
	rows, err := tx.Query(ctx, `SELECT instance_id FROM runtime_generations WHERE state<>'stopped' ORDER BY instance_id`)
	if err != nil {
		return result, err
	}
	var live []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return result, err
		}
		live = append(live, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	named := make([]string, 0, len(in.Retirements))
	for _, r := range in.Retirements {
		named = append(named, r.InstanceID)
	}
	slices.Sort(named)
	if !slices.Equal(live, named) {
		return result, fmt.Errorf("community maintenance fleet is incomplete or changed")
	}
	var ingressReady bool
	if err = tx.QueryRow(ctx, `SELECT
 NOT EXISTS(SELECT 1 FROM runtime_ingress_changes WHERE status IN ('pending','ambiguous'))
 AND (SELECT count(*) FROM runtime_ingress_changes c WHERE NOT EXISTS(SELECT 1 FROM runtime_ingress_changes n WHERE n.predecessor_id=c.change_id))=1
 AND EXISTS(SELECT 1 FROM runtime_ingress_changes c WHERE c.change_id=$1 AND c.status='applied' AND c.membership_after='[]'::jsonb
   AND NOT EXISTS(SELECT 1 FROM runtime_ingress_changes n WHERE n.predecessor_id=c.change_id))`, in.ExpectedIngressHead).Scan(&ingressReady); err != nil {
		return result, err
	}
	if !ingressReady {
		return result, fmt.Errorf("community maintenance requires exact completed withdrawal of all ingress receivers")
	}
	result = communityMaintenanceResult{SchemaVersion: communityMaintenanceSchema, Status: "eligible", SourceSequence: state.ReleaseSequence, CandidateSequence: candidate.Manifest.ReleaseSequence, CandidatePackageSHA256: candidate.BundleSHA256, TrustedRingSHA256: ringSHA, Retirements: make([]runtimegeneration.RetirementResult, 0, len(in.Retirements))}
	for _, r := range in.Retirements {
		checked, e := runtimegeneration.CheckM127MaintenanceRetirementTx(ctx, tx, r, apply)
		if e != nil {
			return result, e
		}
		result.Retirements = append(result.Retirements, checked)
	}
	if apply {
		for i, r := range in.Retirements {
			result.Retirements[i], err = runtimegeneration.ApplyM127MaintenanceRetirementTx(ctx, tx, r)
			if err != nil {
				return result, err
			}
		}
		result.Status = "retired"
	}
	if err = tx.Commit(ctx); err != nil {
		return result, fmt.Errorf("community maintenance outcome unconfirmed; read back before repeating: %w", err)
	}
	return result, nil
}
