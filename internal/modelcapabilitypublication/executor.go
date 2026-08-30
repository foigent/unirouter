package modelcapabilitypublication

import (
	"context"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uniroute/unirouter/internal/migrate"
)

// HooksForPlan binds the migration runner to one exact immutable Plan. The
// caller still owns authorization and maintenance-window orchestration.
func HooksForPlan(trustedEnvironment string, authority PlanAuthority, approved Plan) (migrate.PublicationHooks, error) {
	if err := ValidatePlanAuthority(authority, approved); err != nil {
		return migrate.PublicationHooks{}, err
	}
	if approved.Validation != ValidationValid {
		return migrate.PublicationHooks{}, ErrPlanConflict
	}
	if approved.Target.Environment != trustedEnvironment {
		return migrate.PublicationHooks{}, fmt.Errorf("%w: Plan Target environment does not match trusted runtime environment", ErrPlanConflict)
	}
	revalidate := func(ctx context.Context, conn *pgxpool.Conn) error {
		target, err := ReadTargetState(ctx, conn, trustedEnvironment, authority.Declaration)
		if err != nil {
			return err
		}
		fresh, err := BuildPlan(authority, target)
		if err != nil {
			return err
		}
		if fresh.PlanDigest != approved.PlanDigest || fresh.Validation != ValidationValid {
			return fmt.Errorf("%w: locked plan digest changed: got %s want %s", ErrPlanConflict,
				fresh.PlanDigest, approved.PlanDigest)
		}
		return nil
	}
	verify := func(ctx context.Context, q rowQuerier) error {
		if err := VerifyPlanFinal(ctx, q, approved); err != nil {
			return err
		}
		return VerifyProjectionCoverage(ctx, q, approved, authority.Declaration)
	}
	expectedUp := make(map[string]string, len(authority.ArtifactMigrationFiles))
	for _, file := range authority.ArtifactMigrationFiles {
		expectedUp[file.Version] = file.UpSHA256
	}
	return migrate.PublicationHooks{
		Version:              approved.PublicationID,
		ExpectedUpSHA256:     expectedUp,
		ExpectedVerifySHA256: authority.PublicationVerifySHA256,
		Before:               revalidate,
		ApplyProjection: func(ctx context.Context, tx pgx.Tx) error {
			return ApplyProjectionPlan(ctx, tx, approved)
		},
		VerifyInTx: func(ctx context.Context, tx pgx.Tx) error {
			return verify(ctx, tx)
		},
		VerifyPostCommit: func(ctx context.Context, conn *pgxpool.Conn) error {
			return verify(ctx, conn)
		},
	}, nil
}

// DownHooksForPlan creates the guarded recovery callbacks for one exact
// committed publication. It refuses any retained typed or frozen execution
// evidence and never infers a predecessor.
func DownHooksForPlan(trustedEnvironment string, authority PlanAuthority, plan Plan) (migrate.PublicationDownHooks, error) {
	if err := ValidatePlanAuthority(authority, plan); err != nil {
		return migrate.PublicationDownHooks{}, err
	}
	if plan.Target.Environment != trustedEnvironment {
		return migrate.PublicationDownHooks{}, fmt.Errorf("%w: recovery Plan Target environment does not match trusted runtime environment", ErrPlanConflict)
	}
	verifyBefore := func(ctx context.Context, conn *pgxpool.Conn) error {
		identity, err := readTargetIdentity(ctx, conn, trustedEnvironment)
		if err != nil {
			return err
		}
		if identity != plan.Target {
			return fmt.Errorf("%w: recovery Plan does not target this database", ErrPlanConflict)
		}
		ledger, err := readLedger(ctx, conn)
		if err != nil {
			return err
		}
		if len(ledger) == 0 || ledger[len(ledger)-1] != plan.PublicationID {
			return fmt.Errorf("%w: publication is not the exact ledger ceiling", ErrPlanConflict)
		}
		if err := VerifyPlanFinal(ctx, conn, plan); err != nil {
			return err
		}
		if err := VerifyProjectionCoverage(ctx, conn, plan, authority.Declaration); err != nil {
			return err
		}
		return verifyNoRetainedPublicationEvidence(ctx, conn, authority.Declaration)
	}
	verifyAfter := func(ctx context.Context, q rowQuerier) error {
		if err := VerifyStaticProjection(ctx, q, authority.Predecessor); err != nil {
			return err
		}
		if err := verifyProjectionRollback(ctx, q, plan); err != nil {
			return err
		}
		ledger, err := readLedger(ctx, q)
		if err != nil {
			return err
		}
		if slices.Contains(ledger, plan.PublicationID) {
			return fmt.Errorf("%w: publication ledger remains after guarded down", ErrPlanConflict)
		}
		return nil
	}
	return migrate.PublicationDownHooks{
		Version:            plan.PublicationID,
		ExpectedUpSHA256:   migrationDigestMap(authority.ArtifactMigrationFiles),
		ExpectedDownSHA256: authority.PublicationDownSHA256,
		Before:             verifyBefore,
		RevertProjection: func(ctx context.Context, tx pgx.Tx) error {
			return RevertProjectionPlan(ctx, tx, plan)
		},
		VerifyInTx:       func(ctx context.Context, tx pgx.Tx) error { return verifyAfter(ctx, tx) },
		VerifyPostCommit: func(ctx context.Context, conn *pgxpool.Conn) error { return verifyAfter(ctx, conn) },
	}, nil
}

func migrationDigestMap(files []MigrationFileIdentity) map[string]string {
	digests := make(map[string]string, len(files))
	for _, file := range files {
		digests[file.Version] = file.UpSHA256
	}
	return digests
}

// ValidatePlanAuthority binds an untrusted local Plan to the current compiled
// declaration, exact artifact bytes, and deterministic final static fold.
func ValidatePlanAuthority(authority PlanAuthority, plan Plan) error {
	if err := ValidatePlan(plan); err != nil {
		return err
	}
	if plan.PublicationID != authority.Declaration.PublicationID || plan.Artifact != authority.Artifact ||
		!slices.Equal(plan.ArtifactMigrationLedger, authority.ArtifactMigrationLedger) ||
		!slices.Equal(plan.ArtifactMigrationFiles, authority.ArtifactMigrationFiles) ||
		plan.PublicationDownSHA256 != authority.PublicationDownSHA256 ||
		plan.PublicationVerifySHA256 != authority.PublicationVerifySHA256 {
		return fmt.Errorf("%w: Plan does not match current compiled artifact authority", ErrPlanConflict)
	}
	digest, err := DeclarationDigest(authority.Declaration)
	if err != nil {
		return err
	}
	if plan.DeclarationDigest != digest || authority.Predecessor.ThroughMigration != authority.Declaration.ExpectedStaticPredecessor {
		return fmt.Errorf("%w: Plan does not match current compiled declaration", ErrPlanConflict)
	}
	want, err := Fold(authority.Predecessor, []Declaration{authority.Declaration})
	if err != nil {
		return err
	}
	got, err := StaticProjectionFromPlan(plan)
	if err != nil {
		return err
	}
	if got.ThroughMigration != want.ThroughMigration || !slices.Equal(got.Bindings, want.Bindings) || !slices.Equal(got.Selectors, want.Selectors) {
		return fmt.Errorf("%w: Plan static projection does not match compiled declaration fold", ErrPlanConflict)
	}
	return nil
}
