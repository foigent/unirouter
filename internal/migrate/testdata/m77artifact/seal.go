// Package m77artifact is a frozen integration-test harness for the supported
// M77 SealDeploymentDraft recovery operation. Code under testdata is excluded
// from production package discovery; the M78 runtime must not regain this
// legacy writer.
package m77artifact

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

type database interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

// Artifact represents the still-running M77 binary used to drain a legal
// draft before the schema is upgraded. Its public method deliberately keeps
// the old operation name while the current M78 Manager remains fail-closed.
type Artifact struct {
	database database
}

func New(database database) Artifact {
	return Artifact{database: database}
}

// SealDeploymentDraft performs the M77 draft-to-validated transition. The
// M70/M75/M77 database guards remain the authority for manifest membership,
// current route revisions, executable evidence, and public qualification.
func (artifact Artifact) SealDeploymentDraft(
	ctx context.Context,
	releaseRef string,
	expectedVersion, actorUserID, customerOrgID int64,
) error {
	releaseRef = strings.TrimSpace(strings.ToLower(releaseRef))
	if releaseRef == "" || expectedVersion <= 0 || actorUserID <= 0 || customerOrgID <= 0 {
		return fmt.Errorf("invalid M77 seal identity")
	}
	tx, err := artifact.database.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return fmt.Errorf("begin M77 seal: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck -- rollback after commit is harmless

	var releaseID int64
	var stage string
	var version int64
	var unsealed, authorized bool
	err = tx.QueryRow(ctx, `
SELECT release.id,release.stage,release.version,
       release.route_items_sealed_at IS NULL,
       EXISTS (
           SELECT 1
             FROM users actor
             JOIN orgs actor_org ON actor_org.id=actor.org_id
            WHERE actor.id=$2 AND actor.org_id=$3 AND actor.status='active'
              AND actor_org.status='active'
       )
  FROM runtime_change_releases release
  JOIN model_registry model
    ON model.id=release.public_model_id AND model.org_id=$3
 WHERE release.release_ref=$1
   AND release.candidate_version='unirouter.runtime-change-candidate.v2'
   AND model.lifecycle<>'retired'
 FOR UPDATE OF release`, releaseRef, actorUserID, customerOrgID).Scan(
		&releaseID, &stage, &version, &unsealed, &authorized,
	)
	if err != nil {
		return fmt.Errorf("read M77 deployment draft: %w", err)
	}
	if !authorized || stage != "draft" || version != expectedVersion || !unsealed {
		return fmt.Errorf("M77 deployment draft seal conflict")
	}

	tag, err := tx.Exec(ctx, `
UPDATE runtime_change_releases
   SET stage='validated',version=version+1,validated_by=$2,
       validated_at=transaction_timestamp(),
       route_items_sealed_at=transaction_timestamp(),
       updated_at=transaction_timestamp()
 WHERE id=$1 AND stage='draft' AND version=$3
   AND route_items_sealed_at IS NULL`, releaseID, actorUserID, expectedVersion)
	if err != nil {
		return fmt.Errorf("seal M77 deployment draft: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("M77 deployment draft seal conflict")
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO runtime_change_events (
  release_id,sequence,action,from_stage,to_stage,actor_user_id,
  expected_version,resulting_version,detail)
VALUES ($1,2,'validate','draft','validated',$2,$3,$3::BIGINT+1,
        '{"runtime_mutation":"none","manifest_state":"sealed"}'::jsonb)`,
		releaseID, actorUserID, expectedVersion,
	); err != nil {
		return fmt.Errorf("append M77 deployment seal event: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO audit_logs (org_id,user_id,action,resource,resource_id,detail)
VALUES ($1,$2,'runtime.deployment_manifest.seal','runtime_change_release',$3::BIGINT::TEXT,
        jsonb_build_object('release_ref',$4::TEXT,'expected_version',$5::BIGINT))`,
		customerOrgID, actorUserID, releaseID, releaseRef, expectedVersion,
	); err != nil {
		return fmt.Errorf("audit M77 deployment seal: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit M77 deployment seal: %w", err)
	}
	return nil
}
