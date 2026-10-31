package migrate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uniroute/unirouter/internal/installstate"
	"github.com/uniroute/unirouter/internal/schemaauthority"
)

// FreshOwner cannot be constructed outside this package. Its authority is the
// one physical connection holding the existing session migration lock, never
// a cached read-only preflight, another connection, or an empty fleet manifest.
type FreshOwner struct {
	conn   *pgxpool.Conn
	plan   installstate.Plan
	dir    string
	active bool
}

// WithFreshOwner claims the original ledger before M1 or resumes its exact
// active envelope. All schema transactions and publication hooks use this same
// connection, including when pool.MaxConns is one.
func WithFreshOwner(ctx context.Context, pool *pgxpool.Pool, dir string, plan installstate.Plan, resume bool, fn func(*FreshOwner) error) error {
	if err := plan.Validate(); err != nil {
		return err
	}
	if err := verifyFreshFiles(dir, plan.Artifact); err != nil {
		return err
	}
	conn, release, err := acquireSessionLock(ctx, pool)
	if err != nil {
		return err
	}
	defer release()
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	target, err := installstate.ReadTarget(ctx, tx, plan.Target.Environment)
	if err != nil {
		return err
	}
	if target != plan.Target {
		return fmt.Errorf("fresh-install target identity changed")
	}
	s, err := installstate.Read(ctx, tx)
	if err != nil {
		return err
	}
	if s.Record == nil {
		shape, err := installstate.FreshShape(ctx, tx)
		if err != nil {
			return err
		}
		if shape != plan.FreshShape {
			return fmt.Errorf("fresh database shape changed since Plan")
		}
		if _, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS public.schema_migrations (
 version VARCHAR(64) PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`); err != nil {
			return err
		}
		r := installstate.Record{Plan: plan, State: "active", References: []installstate.Reference{}}
		if err := installstate.Write(ctx, tx, r); err != nil {
			return err
		}
	} else if !resume || s.Record.Plan.Digest != plan.Digest {
		return fmt.Errorf("installation already claimed; resume its exact original Plan")
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	o := &FreshOwner{conn: conn, plan: plan, dir: dir, active: true}
	defer func() { o.active = false; o.conn = nil }()
	if _, err := o.Snapshot(ctx); err != nil {
		return err
	}
	return fn(o)
}

func verifyFreshFiles(dir string, a installstate.Artifact) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	count := 0
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("nonregular migration artifact: %s", entry.Name())
		}
		wire, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return err
		}
		if a.Files[entry.Name()] != installstate.SHA(wire) {
			return fmt.Errorf("fresh artifact differs: %s", entry.Name())
		}
		count++
	}
	if count != len(a.Files) {
		return fmt.Errorf("fresh artifact inventory changed")
	}
	return nil
}

func (o *FreshOwner) Snapshot(ctx context.Context) (installstate.Snapshot, error) {
	if o == nil || !o.active || o.conn == nil || o.conn.Conn().IsClosed() {
		return installstate.Snapshot{}, fmt.Errorf("fresh owner is no longer live")
	}
	s, err := installstate.Read(ctx, o.conn)
	if err != nil {
		return s, err
	}
	if s.Record == nil || s.Record.Plan.Digest != o.plan.Digest {
		return s, fmt.Errorf("fresh owner envelope mismatch")
	}
	if len(s.Ledger) > len(o.plan.Artifact.Ledger) || !slices.Equal(s.Ledger, o.plan.Artifact.Ledger[:len(s.Ledger)]) {
		return s, fmt.Errorf("fresh ledger is not the exact artifact prefix")
	}
	return s, nil
}

// Read runs original publication planning/recovery on the locked connection.
// The callback must remain read-only; schema mutation goes through the methods
// below and cannot transfer ownership to another backend PID.
func (o *FreshOwner) Read(ctx context.Context, fn func(*pgxpool.Conn) error) error {
	if _, err := o.Snapshot(ctx); err != nil {
		return err
	}
	return fn(o.conn)
}

func (o *FreshOwner) requireActive(ctx context.Context) (installstate.Snapshot, error) {
	s, err := o.Snapshot(ctx)
	if err == nil && s.Record.State != "active" {
		err = fmt.Errorf("installation is already completed")
	}
	return s, err
}

func (o *FreshOwner) Baseline(ctx context.Context, ceiling string) error {
	if _, err := o.requireActive(ctx); err != nil {
		return err
	}
	return runOnConnection(ctx, o.conn, o.dir, runOptions{fresh: o, ceiling: ceiling, requireExactPrefix: true})
}

// AdoptReference is called only after the independently validated Plan file
// and its parent directory have been fsynced. No schema progress is inferred
// from an unadopted file or a local receipt.
func (o *FreshOwner) AdoptReference(ctx context.Context, ref installstate.Reference) error {
	s, err := o.requireActive(ctx)
	if err != nil {
		return err
	}
	if ref.Committed {
		return fmt.Errorf("cannot adopt precommitted publication proof")
	}
	for _, old := range s.Record.References {
		if old.PlanDigest == ref.PlanDigest {
			return fmt.Errorf("Plan reference already adopted")
		}
	}
	r := *s.Record
	r.References = append(slices.Clone(r.References), ref)
	tx, err := o.conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := installstate.Write(ctx, tx, r); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (o *FreshOwner) Publication(ctx context.Context, allowed []string, hooks PublicationHooks, digest string) error {
	s, err := o.requireActive(ctx)
	if err != nil {
		return err
	}
	if len(allowed) == 0 || allowed[len(allowed)-1] != hooks.Version || hooks.Before == nil || hooks.ApplyProjection == nil || hooks.VerifyInTx == nil || hooks.VerifyPostCommit == nil {
		return fmt.Errorf("fresh publication requires original complete hooks and exact prefix")
	}
	index := -1
	for i, ref := range s.Record.References {
		if ref.Publication == hooks.Version {
			index = i
		}
	}
	if index < 0 || s.Record.References[index].PlanDigest != digest || s.Record.References[index].Committed {
		return fmt.Errorf("publication has no matching active adopted Plan")
	}
	return runOnConnection(ctx, o.conn, o.dir, runOptions{fresh: o, allowedMigrations: slices.Clone(allowed), ceiling: hooks.Version, requireExactPrefix: true, publication: &hooks,
		beforeCommit: func(ctx context.Context, tx pgx.Tx, version string) error {
			if version != hooks.Version {
				return nil
			}
			actual, err := installstate.Read(ctx, tx)
			if err != nil {
				return err
			}
			if actual.Record == nil || actual.Record.Plan.Digest != o.plan.Digest || actual.Record.State != "active" {
				return fmt.Errorf("publication installation binding changed")
			}
			r := *actual.Record
			r.References = slices.Clone(r.References)
			if index >= len(r.References) || r.References[index].PlanDigest != digest {
				return fmt.Errorf("publication reference changed")
			}
			r.References[index].Committed = true
			return installstate.Write(ctx, tx, r)
		},
	})
}

// Expand is a fresh-only schema owner, not an online compatible-expand with a
// fictitious empty fleet. The final callback verifies static authority and
// certifies M118 using the original implementation. Only the exact Artifact
// ceiling commits the completion envelope, now M121 for new installations.
func (o *FreshOwner) Expand(ctx context.Context, version string, final func(context.Context, pgx.Tx) error) error {
	s, err := o.requireActive(ctx)
	if err != nil {
		return err
	}
	if err := validateFreshExpansion(o.plan.Artifact.Ledger, s.Ledger, version, final != nil); err != nil {
		return err
	}
	return runOnConnection(ctx, o.conn, o.dir, runOptions{fresh: o, freshExpand: version, ceiling: version, requireExactPrefix: true,
		beforeCommit: func(ctx context.Context, tx pgx.Tx, v string) error {
			if final == nil {
				return nil
			}
			if err := final(ctx, tx); err != nil {
				return err
			}
			if v != o.plan.Artifact.Ledger[len(o.plan.Artifact.Ledger)-1] {
				return nil
			}
			actual, err := installstate.Read(ctx, tx)
			if err != nil {
				return err
			}
			if actual.Record == nil || actual.Record.Plan.Digest != o.plan.Digest || actual.Record.State != "active" || !slices.Equal(actual.Ledger, o.plan.Artifact.Ledger) {
				return fmt.Errorf("final installation binding is incomplete")
			}
			r := *actual.Record
			r.State = "completed"
			r.Completion = &installstate.Completion{LedgerSHA256: installstate.LedgerSHA(actual.Ledger), MigrationSHA256: "sha256:" + o.plan.Artifact.Files[v+".up.sql"], ManifestSHA256: o.plan.Artifact.ManifestDigest}
			return installstate.Write(ctx, tx, r)
		},
	})
}

func validateFreshExpansion(artifact, applied []string, version string, hasVerifier bool) error {
	if version != schemaauthority.CapabilityExpandHead && version != schemaauthority.StreamCounterExpandHead && version != schemaauthority.SourceFactsExpandHead && version != schemaauthority.ObservationExpandHead && version != schemaauthority.BudgetSnapshotExpandHead && version != schemaauthority.ContentCaptureExpandHead && version != schemaauthority.ContentReclaimExpandHead && version != schemaauthority.PreDispatchExpandHead && (version != schemaauthority.ScopedContentExpandHead && version != schemaauthority.XInsightPolicyExpandHead && (version != schemaauthority.XInsightAdoptionExpandHead && version != schemaauthority.XInsightCredentialExpandHead && version != schemaauthority.CommercialLicenseRetirementHead && version != schemaauthority.XInsightCommunicationHead && version != schemaauthority.UpstreamReleaseObservationHead && version != schemaauthority.UpstreamObservationRestoreHead)) {
		return fmt.Errorf("unknown fresh expansion")
	}
	if len(applied) >= len(artifact) || !slices.Equal(applied, artifact[:len(applied)]) || artifact[len(applied)] != version {
		return fmt.Errorf("fresh expansion is not the next exact migration")
	}
	if (version == schemaauthority.BudgetSnapshotExpandHead || (version == schemaauthority.ContentCaptureExpandHead || version == schemaauthority.ContentReclaimExpandHead || version == schemaauthority.PreDispatchExpandHead || (version == schemaauthority.ScopedContentExpandHead || (version == schemaauthority.XInsightPolicyExpandHead || (version == schemaauthority.XInsightAdoptionExpandHead || version == schemaauthority.XInsightCredentialExpandHead || version == schemaauthority.CommercialLicenseRetirementHead || version == schemaauthority.XInsightCommunicationHead || (version == schemaauthority.UpstreamReleaseObservationHead || version == schemaauthority.UpstreamObservationRestoreHead)))))) != hasVerifier {
		return fmt.Errorf("M118 certification and M119/M120/M121 completion require their atomic verifiers")
	}
	return nil
}
