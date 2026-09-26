package runtimegeneration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/schemaauthority"
)

const LeaseTTL = 45 * time.Second
const LeaseRefresh = 10 * time.Second

var ErrIncompatible = errors.New("runtime authority is incompatible")

var instancePattern = regexp.MustCompile(`^uri_[a-f0-9]{32}$`)
var artifactPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

type Identity struct {
	Instance string `json:"instance"`
	Artifact string `json:"artifact_digest"`
}

func (i Identity) Validate() error {
	if !instancePattern.MatchString(i.Instance) || !artifactPattern.MatchString(i.Artifact) {
		return fmt.Errorf("exact runtime instance and artifact are required")
	}
	return nil
}

// Renew registers/renews this process only. It never joins or withdraws ingress.
// Reusing an instance or Artifact identity with other immutable bytes fails.
// The returned monotonic local deadline is conservative relative to DB expiry.
func Renew(ctx context.Context, pool *pgxpool.Pool, identity Identity, manifest Manifest, artifactLedger []string, draining bool) (time.Time, string, error) {
	started := time.Now()
	if err := identity.Validate(); err != nil {
		return time.Time{}, "", err
	}
	wire, err := manifest.CanonicalBytes()
	if err != nil {
		return time.Time{}, "", err
	}
	digest, _ := manifest.Digest()
	state := "serving"
	if draining {
		state = "draining"
	}
	// Renewing an unchanged identity grants no ingress or execution authority.
	// It must not wait behind a Publication's full static projection validation,
	// otherwise normal control-plane work could expire a healthy serving lease.
	ledger, head, err := readLeaseLedger(ctx, pool, artifactLedger)
	if err != nil {
		return time.Time{}, head, err
	}
	if err := requireLeaseManifest(manifest, head); err != nil {
		return time.Time{}, head, err
	}
	updated, err := pool.Exec(ctx, `UPDATE runtime_generations g SET lease_expires_at=clock_timestamp()+$6*interval '1 second',revision=g.revision+1,updated_at=clock_timestamp()
FROM runtime_support_manifests m WHERE g.instance_id=$1 AND g.artifact_digest=$2 AND g.compatibility_epoch=$3 AND g.schema_head=$4 AND g.state=$5
AND m.artifact_digest=g.artifact_digest AND m.manifest_digest=$7 AND m.canonical_bytes=$8
AND $9::text[]=(SELECT array_agg(version::text ORDER BY version) FROM schema_migrations)`, identity.Instance, identity.Artifact, CompatibilityEpoch, head, state, int(LeaseTTL/time.Second), digest, wire, ledger)
	if err != nil {
		return time.Time{}, head, err
	}
	if updated.RowsAffected() == 1 {
		return started.Add(LeaseTTL - 5*time.Second), head, nil
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return time.Time{}, "", err
	}
	defer tx.Rollback(ctx)
	if err := schemaauthority.LockExecutionCompatibility(ctx, tx); err != nil {
		return time.Time{}, "", err
	}
	_, head, err = readLeaseLedger(ctx, tx, artifactLedger)
	if err != nil {
		return time.Time{}, head, err
	}
	if err := requireLeaseManifest(manifest, head); err != nil {
		return time.Time{}, head, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO runtime_support_manifests(artifact_digest,manifest_digest,canonical_bytes) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, identity.Artifact, digest, wire); err != nil {
		return time.Time{}, "", err
	}
	var retained []byte
	if err := tx.QueryRow(ctx, `SELECT canonical_bytes FROM runtime_support_manifests WHERE artifact_digest=$1`, identity.Artifact).Scan(&retained); err != nil {
		return time.Time{}, "", err
	}
	if !bytes.Equal(wire, retained) {
		return time.Time{}, "", fmt.Errorf("%w: artifact support manifest changed", ErrIncompatible)
	}
	// A process may drain only after explicit withdrawal. If SIGTERM arrives
	// early, renewal fails and the membership remains a conservative blocker.
	_, err = tx.Exec(ctx, `INSERT INTO runtime_generations(instance_id,artifact_digest,compatibility_epoch,schema_head,state,lease_expires_at)
VALUES($1,$2,$3,$4,$5,clock_timestamp()+$6*interval '1 second')
ON CONFLICT(instance_id) DO UPDATE SET state=EXCLUDED.state,schema_head=EXCLUDED.schema_head,lease_expires_at=EXCLUDED.lease_expires_at,revision=runtime_generations.revision+1,updated_at=clock_timestamp()
WHERE runtime_generations.artifact_digest=EXCLUDED.artifact_digest AND runtime_generations.compatibility_epoch=EXCLUDED.compatibility_epoch AND runtime_generations.state<>'stopped'
RETURNING instance_id`, identity.Instance, identity.Artifact, CompatibilityEpoch, head, state, int(LeaseTTL/time.Second))
	if err != nil {
		return time.Time{}, "", err
	}
	var storedArtifact, storedState string
	if err := tx.QueryRow(ctx, `SELECT artifact_digest,state FROM runtime_generations WHERE instance_id=$1`, identity.Instance).Scan(&storedArtifact, &storedState); err != nil {
		return time.Time{}, "", err
	}
	if storedArtifact != identity.Artifact || storedState != state {
		return time.Time{}, "", fmt.Errorf("%w: runtime instance cannot be reused", ErrIncompatible)
	}
	if err := tx.Commit(ctx); err != nil {
		return time.Time{}, "", fmt.Errorf("lease commit unconfirmed: %w", err)
	}
	return started.Add(LeaseTTL - 5*time.Second), head, nil
}

func readLeaseLedger(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, artifact []string) ([]string, string, error) {
	var encoded string
	if err := q.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(version ORDER BY version),'[]'::jsonb)::text FROM schema_migrations`).Scan(&encoded); err != nil {
		return nil, "", err
	}
	var ledger []string
	if err := json.Unmarshal([]byte(encoded), &ledger); err != nil {
		return nil, "", err
	}
	if err := schemaauthority.VerifyRuntimeLedger(artifact, ledger); err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrIncompatible, err)
	}
	head := ledger[len(ledger)-1]
	if head != schemaauthority.CapabilityExpandHead && head != schemaauthority.StreamCounterExpandHead && head != schemaauthority.SourceFactsExpandHead && head != schemaauthority.ObservationExpandHead && head != schemaauthority.BudgetSnapshotExpandHead && head != schemaauthority.ContentCaptureExpandHead && head != schemaauthority.ContentReclaimExpandHead && head != schemaauthority.PreDispatchExpandHead {
		return nil, head, fmt.Errorf("%w: runtime lease requires explicit expand", ErrIncompatible)
	}
	return ledger, head, nil
}

// BootstrapVerifiedBridge is called only by the explicit schema owner's After
// hook, before M114 and its initial leases commit together. It grants no ingress
// membership and never upserts a retained identity. The caller has proved the
// live process, Docker Artifact and canonical manifest on the operator channel.
func BootstrapVerifiedBridge(ctx context.Context, tx pgx.Tx, identity Identity, manifest Manifest, artifactLedger []string) error {
	if err := identity.Validate(); err != nil {
		return err
	}
	_, head, err := readLeaseLedger(ctx, tx, artifactLedger)
	if err != nil {
		return err
	}
	wire, err := manifest.CanonicalBytes()
	if err != nil {
		return err
	}
	if err := manifest.Require(head, []modelprofile.ImplementationReference{{Kind: "capability_projection", ID: publication.ProjectionSchemaV2}}); err != nil {
		return err
	}
	digest, _ := manifest.Digest()
	_, err = tx.Exec(ctx, `INSERT INTO runtime_support_manifests(artifact_digest,manifest_digest,canonical_bytes) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, identity.Artifact, digest, wire)
	if err != nil {
		return err
	}
	var retained []byte
	if err := tx.QueryRow(ctx, `SELECT canonical_bytes FROM runtime_support_manifests WHERE artifact_digest=$1`, identity.Artifact).Scan(&retained); err != nil {
		return err
	}
	if !bytes.Equal(wire, retained) {
		return fmt.Errorf("%w: bootstrap Artifact manifest changed", ErrIncompatible)
	}
	_, err = tx.Exec(ctx, `INSERT INTO runtime_generations(instance_id,artifact_digest,compatibility_epoch,schema_head,state,ingress_state,lease_expires_at)
VALUES($1,$2,$3,$4,'serving','absent',clock_timestamp()+$5*interval '1 second')`, identity.Instance, identity.Artifact, CompatibilityEpoch, head, int(LeaseTTL/time.Second))
	return err
}

// Schema advertisement alone cannot register a pre-C same-purpose writer on M119.
func requireLeaseManifest(manifest Manifest, head string) error {
	if head != schemaauthority.ContentCaptureExpandHead && head != schemaauthority.ContentReclaimExpandHead && head != schemaauthority.PreDispatchExpandHead {
		return nil
	}
	if err := manifest.Require(head, ContentLifecycleRequirements()); err != nil {
		return fmt.Errorf("%w: %v", ErrIncompatible, err)
	}
	if head == schemaauthority.PreDispatchExpandHead {
		if err := manifest.Require(head, []modelprofile.ImplementationReference{{Kind: "source_observations", ID: "unirouter.source-observation.v2"}}); err != nil {
			return fmt.Errorf("%w: %v", ErrIncompatible, err)
		}
	}
	return nil
}
