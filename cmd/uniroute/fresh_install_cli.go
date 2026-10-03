package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/installstate"
	"github.com/uniroute/unirouter/internal/migrate"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
)

const freshUsage = "usage: uniroute fresh-install plan --out <new-plan.json> | check --plan <plan.json> | apply|resume --plan <plan.json> --evidence-dir <private-directory>"

func runFreshInstallCLI(ctx context.Context, args []string, cfg *config.Config) error {
	if len(args) == 0 {
		return fmt.Errorf("%s", freshUsage)
	}
	action := args[0]
	if action != "plan" && action != "check" && action != "apply" && action != "resume" {
		return fmt.Errorf("%s", freshUsage)
	}
	fs := flag.NewFlagSet("fresh-install "+action, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	out := fs.String("out", "", "new outer installation Plan")
	planPath := fs.String("plan", "", "original installation Plan")
	evidence := fs.String("evidence-dir", "", "private durable independent publication Plan directory")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 || (action == "plan" && (*out == "" || *planPath != "" || *evidence != "")) || (action != "plan" && (*out != "" || *planPath == "")) || (action == "check" && *evidence != "") || ((action == "apply" || action == "resume") && *evidence == "") {
		return fmt.Errorf("%s", freshUsage)
	}
	artifact, err := freshArtifact(cfg)
	if err != nil {
		return err
	}
	postgres, err := db.NewPostgres(cfg.DB_DSN, cfg.DBMaxConnections, cfg.DBMaxIdleConns)
	if err != nil {
		return err
	}
	defer postgres.Close()
	if action == "plan" {
		p, err := planFreshInstall(ctx, postgres.Pool, cfg.RuntimeEnvironment, artifact)
		if err != nil {
			return err
		}
		wire, err := json.Marshal(p)
		if err != nil {
			return err
		}
		if err := writeFreshFile(*out, wire); err != nil {
			return err
		}
		fmt.Printf("fresh_install_plan=%s installation=%s database_unchanged=true\n", p.Digest, p.InstallationID)
		return nil
	}
	wire, err := readFreshFile(*planPath, installstate.MaximumBytes)
	if err != nil {
		return err
	}
	p, err := installstate.DecodePlan(wire)
	if err != nil {
		return err
	}
	if action == "check" {
		s, err := checkFreshInstall(ctx, postgres.Pool, p)
		if err != nil {
			return err
		}
		if (s.Record == nil || s.Record.State == "active") && (!reflect.DeepEqual(artifact, p.Artifact) || cfg.RuntimeEnvironment != p.Target.Environment) {
			return fmt.Errorf("active installation artifact or environment differs")
		}
		state := "unclaimed"
		if s.Record != nil {
			state = s.Record.State
		}
		fmt.Printf("fresh_install_check=PASS state=%s ledger_entries=%d writes=0 publication_plan_files=not_checked\n", state, len(s.Ledger))
		return nil
	}
	if !reflect.DeepEqual(artifact, p.Artifact) || cfg.RuntimeEnvironment != p.Target.Environment {
		return fmt.Errorf("fresh-install requires the original exact image, binary, manifest and migration inventory")
	}
	if err := prepareFreshEvidence(*evidence, p, action == "resume"); err != nil {
		return err
	}
	err = migrate.WithFreshOwner(ctx, postgres.Pool, configuredMigrationsDir(cfg), p, action == "resume", func(owner *migrate.FreshOwner) error {
		return executeFreshInstall(ctx, owner, cfg, p, *evidence)
	})
	if err != nil {
		return err
	}
	fmt.Printf("fresh_install=completed installation=%s plan=%s schema=%s evidence=%s\n", p.InstallationID, p.Digest, p.Artifact.Ledger[len(p.Artifact.Ledger)-1], *evidence)
	return nil
}

func freshArtifact(cfg *config.Config) (installstate.Artifact, error) {
	var a installstate.Artifact
	if cfg == nil || buildVersion == "" || buildCommit == "" || buildTree == "" || cfg.RuntimeVersion != buildVersion || cfg.RuntimeCommit != buildCommit || cfg.RuntimeTree != buildTree {
		return a, fmt.Errorf("fresh-install requires the release build's embedded version, commit and tree")
	}
	identity, err := runtimePublicationArtifact(cfg)
	if err != nil {
		return a, err
	}
	dir := configuredMigrationsDir(cfg)
	ledger, err := schemaauthority.ExpectedLedger(dir)
	if err != nil {
		return a, err
	}
	if err := publication.VerifyArtifactPublicationInventory(dir, ledger); err != nil {
		return a, err
	}
	manifest := runtimegeneration.CurrentManifest(schemaauthority.SupportedSchemaHeads())
	digest, err := manifest.Digest()
	if err != nil {
		return a, err
	}
	a = installstate.Artifact{Version: identity.Version, Commit: identity.Commit, Tree: identity.Tree, ImageDigest: identity.ImageDigest, ExecutableDigest: identity.PublisherExecutableDigest, ManifestDigest: digest, Ledger: ledger, Files: map[string]string{}}
	declarations, err := publication.CompiledDeclarations()
	if err != nil {
		return a, err
	}
	for _, declaration := range declarations {
		a.Publications = append(a.Publications, declaration.PublicationID)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return a, err
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		if !entry.Type().IsRegular() {
			return a, fmt.Errorf("nonregular migration file")
		}
		wire, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return a, err
		}
		a.Files[entry.Name()] = installstate.SHA(wire)
	}
	return a, nil
}

func planFreshInstall(ctx context.Context, pool *pgxpool.Pool, environment string, artifact installstate.Artifact) (installstate.Plan, error) {
	p := installstate.Plan{SchemaVersion: installstate.Schema, Artifact: artifact}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return p, err
	}
	defer tx.Rollback(ctx)
	p.Target, err = installstate.ReadTarget(ctx, tx, environment)
	if err != nil {
		return p, err
	}
	p.FreshShape, err = installstate.FreshShape(ctx, tx)
	if err != nil {
		return p, err
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return p, err
	}
	p.InstallationID = "ufi_" + hex.EncodeToString(id[:])
	p.Digest = p.CalculateDigest()
	return p, p.Validate()
}

func checkFreshInstall(ctx context.Context, pool *pgxpool.Pool, p installstate.Plan) (installstate.Snapshot, error) {
	var s installstate.Snapshot
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return s, err
	}
	defer tx.Rollback(ctx)
	s, err = installstate.Read(ctx, tx)
	if err != nil {
		return s, err
	}
	if s.Record != nil && s.Record.Plan.Digest == p.Digest && s.Record.State == "completed" {
		// A restored complete DB keeps its origin envelope. This read-only
		// check verifies today's state without rebinding or recertifying it.
		return s, verifyFreshCompletion(ctx, tx, p.Artifact.Ledger)
	}
	target, err := installstate.ReadTarget(ctx, tx, p.Target.Environment)
	if err != nil {
		return s, err
	}
	if target != p.Target {
		return s, fmt.Errorf("install Plan target differs")
	}
	if s.Record == nil {
		shape, err := installstate.FreshShape(ctx, tx)
		if err != nil {
			return s, err
		}
		if shape != p.FreshShape {
			return s, fmt.Errorf("fresh database shape differs")
		}
		return s, nil
	}
	if s.Record.Plan.Digest != p.Digest || !freshPrefix(s.Ledger, p.Artifact.Ledger) {
		return s, fmt.Errorf("installation identity or ledger differs")
	}
	if s.Record.State == "completed" {
		err = verifyFreshCompletion(ctx, tx, p.Artifact.Ledger)
	}
	return s, err
}

func executeFreshInstall(ctx context.Context, owner *migrate.FreshOwner, cfg *config.Config, p installstate.Plan, evidence string) error {
	return executeFreshInstallObserved(ctx, owner, cfg, p, evidence, nil)
}

// The unexported observer is used by subprocess integration tests to terminate
// the real owner at durable boundaries. No CLI flag or environment switch can
// enable it in the product binary.
type freshInstallObserver func(phase, version string)

func (o freshInstallObserver) at(phase, version string) {
	if o != nil {
		o(phase, version)
	}
}

func executeFreshInstallObserved(ctx context.Context, owner *migrate.FreshOwner, cfg *config.Config, p installstate.Plan, evidence string, observe freshInstallObserver) error {
	observe.at("claimed", "")
	s, err := owner.Snapshot(ctx)
	if err != nil {
		return err
	}
	declarations, err := publication.CompiledDeclarations()
	if err != nil {
		return err
	}
	inputsByID, err := freshPublicationInputs(cfg, declarations)
	if err != nil {
		return err
	}
	// Validate every adopted file, including superseded ordinary-prefix Plans,
	// before resuming any write. Missing historical files are not synthesized.
	for _, ref := range s.Record.References {
		inputs, ok := inputsByID[ref.Publication]
		if !ok {
			return fmt.Errorf("adopted Plan is not a compiled publication")
		}
		if _, err := loadFreshPublication(evidence, ref, inputs.authority); err != nil {
			return err
		}
	}
	if s.Record.State == "completed" {
		return owner.Read(ctx, func(conn *pgxpool.Conn) error { return verifyFreshCompletion(ctx, conn, p.Artifact.Ledger) })
	}
	if len(s.Ledger) == 0 || s.Ledger[len(s.Ledger)-1] < publication.BaselineMigrationM100 {
		if len(s.Record.References) != 0 {
			return fmt.Errorf("publication references precede baseline")
		}
		if err := owner.Baseline(ctx, publication.BaselineMigrationM100); err != nil {
			return err
		}
		observe.at("baseline_committed", publication.BaselineMigrationM100)
	}
	for _, declaration := range declarations {
		if err := executeFreshPublication(ctx, owner, inputsByID[declaration.PublicationID], p, evidence, declaration.PublicationID, observe); err != nil {
			return err
		}
	}
	for _, version := range []string{schemaauthority.CapabilityExpandHead, schemaauthority.StreamCounterExpandHead, schemaauthority.SourceFactsExpandHead, schemaauthority.ObservationExpandHead, schemaauthority.BudgetSnapshotExpandHead, schemaauthority.ContentCaptureExpandHead, schemaauthority.ContentReclaimExpandHead, schemaauthority.PreDispatchExpandHead, schemaauthority.ScopedContentExpandHead, schemaauthority.XInsightPolicyExpandHead, schemaauthority.XInsightAdoptionExpandHead} {
		s, err := owner.Snapshot(ctx)
		if err != nil {
			return err
		}
		if !slices.Contains(p.Artifact.Ledger, version) || slices.Contains(s.Ledger, version) {
			continue
		}
		var final func(context.Context, pgx.Tx) error
		if version == schemaauthority.BudgetSnapshotExpandHead {
			final = func(ctx context.Context, tx pgx.Tx) error {
				manifest := runtimegeneration.CurrentManifest(schemaauthority.SupportedSchemaHeads())
				if err := runtimegeneration.CertifyBudgetCoverageTx(ctx, tx, manifest, "sha256:"+p.Artifact.Files[version+".up.sql"]); err != nil {
					return err
				}
				return nil
			}
		}
		if version == schemaauthority.ContentCaptureExpandHead || version == schemaauthority.ContentReclaimExpandHead || version == schemaauthority.PreDispatchExpandHead || (version == schemaauthority.ScopedContentExpandHead || (version == schemaauthority.XInsightPolicyExpandHead || version == schemaauthority.XInsightAdoptionExpandHead)) {
			final = func(ctx context.Context, tx pgx.Tx) error {
				if version == p.Artifact.Ledger[len(p.Artifact.Ledger)-1] {
					return verifyFreshCompletion(ctx, tx, p.Artifact.Ledger)
				}
				_, err := runtimegeneration.ReadBudgetCoverage(ctx, tx)
				return err
			}
		}
		if err := owner.Expand(ctx, version, final); err != nil {
			return err
		}
		observe.at("expand_committed", version)
	}
	return owner.Read(ctx, func(conn *pgxpool.Conn) error {
		s, err := installstate.RequireOrdinary(ctx, conn)
		if err != nil {
			return err
		}
		if s.Record == nil || s.Record.State != "completed" {
			return fmt.Errorf("target schema has no atomic completion envelope")
		}
		return verifyFreshCompletion(ctx, conn, p.Artifact.Ledger)
	})
}

// Artifact/executable identity is common to this bounded command. Compute it
// once; retain the original per-publication generated-file verification and
// all original hook-time byte/digest checks.
func freshPublicationInputs(cfg *config.Config, declarations []publication.Declaration) (map[string]publicationCommandInputs, error) {
	if len(declarations) == 0 {
		return nil, fmt.Errorf("missing compiled publications")
	}
	first, err := resolvePublicationCommandInputs(cfg, declarations[0].PublicationID)
	if err != nil {
		return nil, err
	}
	result := map[string]publicationCommandInputs{declarations[0].PublicationID: first}
	for _, d := range declarations[1:] {
		declaration, predecessor, err := publication.CompiledPublication(d.PublicationID)
		if err != nil {
			return nil, err
		}
		if err := publication.VerifyGeneratedMigrationFiles(first.migrationsDir, predecessor, declaration); err != nil {
			return nil, err
		}
		down, verify, err := publication.ReadPublicationAuxiliaryDigests(first.migrationsDir, d.PublicationID)
		if err != nil {
			return nil, err
		}
		next := first
		next.authority.Declaration, next.authority.Predecessor = declaration, predecessor
		next.authority.PublicationDownSHA256, next.authority.PublicationVerifySHA256 = down, verify
		result[d.PublicationID] = next
	}
	return result, nil
}

func executeFreshPublication(ctx context.Context, owner *migrate.FreshOwner, inputs publicationCommandInputs, outer installstate.Plan, evidence, id string, observe freshInstallObserver) error {
	s, err := owner.Snapshot(ctx)
	if err != nil {
		return err
	}
	var latest *installstate.Reference
	for i := range s.Record.References {
		if s.Record.References[i].Publication == id {
			latest = &s.Record.References[i]
		}
	}
	var plan publication.Plan
	if latest != nil {
		plan, err = loadFreshPublication(evidence, *latest, inputs.authority)
		if err != nil {
			return err
		}
	}
	if slices.Contains(s.Ledger, id) {
		if latest == nil || !latest.Committed {
			return fmt.Errorf("committed publication %s has no atomic original Plan reference", id)
		}
		return owner.Read(ctx, func(conn *pgxpool.Conn) error {
			_, err := publication.PostCommitRecoveryCheck(ctx, conn, outer.Target.Environment, &plan, outer.Artifact.Ledger, &inputs.authority)
			return err
		})
	}
	if latest != nil && latest.Committed {
		return fmt.Errorf("publication proof has no matching committed ledger")
	}
	if latest != nil && !slices.Equal(s.Ledger, plan.ExpectedLedger) {
		if err := ordinaryPublicationPrefix(plan, s.Ledger); err != nil {
			return err
		}
	}
	if latest == nil || !slices.Equal(s.Ledger, plan.ExpectedLedger) {
		var next publication.Plan
		err := owner.Read(ctx, func(conn *pgxpool.Conn) error {
			tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
			if err != nil {
				return err
			}
			defer tx.Rollback(ctx)
			state, err := publication.ReadTargetState(ctx, tx, outer.Target.Environment, inputs.authority.Declaration)
			if err != nil {
				return err
			}
			next, err = publication.BuildPlan(inputs.authority, state)
			return err
		})
		if err != nil {
			return err
		}
		if latest == nil {
			// Without an adopted Plan, no ordinary migration between the static
			// predecessor and this publication could have been authorized.
			if len(s.Ledger) == 0 || s.Ledger[len(s.Ledger)-1] != inputs.authority.Declaration.ExpectedStaticPredecessor {
				return fmt.Errorf("unadopted publication has unexpected ledger movement")
			}
		} else if next.PublicationID != plan.PublicationID || next.Target != plan.Target || next.Artifact != plan.Artifact || !slices.Equal(next.ProposedLedger, plan.ProposedLedger) {
			return fmt.Errorf("ordinary-prefix successor Plan changed its final authority")
		}
		if err := publication.ValidatePlanAuthority(inputs.authority, next); err != nil {
			return err
		}
		if next.Validation != publication.ValidationValid {
			return fmt.Errorf("fresh publication Plan has unresolved conflicts")
		}
		wire, err := json.Marshal(publication.NormalizePlan(next))
		if err != nil {
			return err
		}
		ref := installstate.Reference{Publication: id, PlanDigest: next.PlanDigest, FileSHA256: installstate.SHA(wire)}
		if latest != nil {
			ref.Supersedes = latest.PlanDigest
		}
		if err := persistFreshPublication(evidence, ref, wire); err != nil {
			return err
		}
		observe.at("plan_durable", id)
		if err := owner.AdoptReference(ctx, ref); err != nil {
			return err
		}
		observe.at("plan_adopted", id)
		plan = next
	}
	hooks, err := publication.HooksForPlan(outer.Target.Environment, inputs.authority, plan)
	if err != nil {
		return err
	}
	originalApply, originalVerify := hooks.ApplyProjection, hooks.VerifyPostCommit
	hooks.ApplyProjection = func(ctx context.Context, tx pgx.Tx) error {
		observe.at("publication_uncommitted", id)
		return originalApply(ctx, tx)
	}
	hooks.VerifyPostCommit = func(ctx context.Context, conn *pgxpool.Conn) error {
		if err := originalVerify(ctx, conn); err != nil {
			return err
		}
		observe.at("publication_committed", id)
		return nil
	}
	return owner.Publication(ctx, plan.ApplyMigrations, hooks, plan.PlanDigest)
}

// An interrupted Plan may advance only through its explicitly authorized
// ordinary migrations. Its publication is last; crossing it requires the
// atomic committed reference, never a replacement no-op Plan.
func ordinaryPublicationPrefix(p publication.Plan, actual []string) error {
	if !freshPrefix(p.ExpectedLedger, actual) || !freshPrefix(actual, p.ProposedLedger) || len(actual) >= len(p.ProposedLedger) || len(actual) <= len(p.ExpectedLedger) || len(actual)-len(p.ExpectedLedger) > len(p.ApplyMigrations) || !slices.Equal(actual[len(p.ExpectedLedger):], p.ApplyMigrations[:len(actual)-len(p.ExpectedLedger)]) {
		return fmt.Errorf("ledger is not an interrupted ordinary-only prefix of its adopted Plan")
	}
	return nil
}

func freshPrefix(a, b []string) bool { return len(a) <= len(b) && slices.Equal(a, b[:len(a)]) }

func verifyFreshCompletion(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, artifact []string) error {
	actual, err := readRuntimeSchemaLedger(ctx, q)
	if err != nil {
		return err
	}
	if err := schemaauthority.VerifyLedger(artifact, actual); err != nil {
		return err
	}
	if err := publication.VerifyRuntimeStaticProjection(ctx, q, actual); err != nil {
		return err
	}
	_, err = runtimegeneration.ReadBudgetCoverage(ctx, q)
	return err
}

func freshPublicationPath(dir string, ref installstate.Reference) string {
	return filepath.Join(dir, ref.Publication+"-"+ref.PlanDigest+".json")
}

func loadFreshPublication(dir string, ref installstate.Reference, authority publication.PlanAuthority) (publication.Plan, error) {
	wire, err := readFreshFile(freshPublicationPath(dir, ref), maximumPublicationPlanBytes)
	if err != nil {
		return publication.Plan{}, err
	}
	if installstate.SHA(wire) != ref.FileSHA256 {
		return publication.Plan{}, fmt.Errorf("adopted publication Plan bytes changed")
	}
	p, err := publication.DecodePlan(wire)
	if err != nil {
		return p, err
	}
	if p.PlanDigest != ref.PlanDigest || p.PublicationID != ref.Publication {
		return p, fmt.Errorf("adopted publication Plan identity differs")
	}
	return p, publication.ValidatePlanAuthority(authority, p)
}

func persistFreshPublication(dir string, ref installstate.Reference, wire []byte) error {
	path := freshPublicationPath(dir, ref)
	if _, err := os.Lstat(path); err == nil {
		old, err := readFreshFile(path, maximumPublicationPlanBytes)
		if err != nil {
			return err
		}
		if !bytes.Equal(old, wire) {
			return fmt.Errorf("refusing to replace existing publication Plan")
		}
		// A crash may have left an exact unadopted file. Revalidate its bytes
		// above, then make both it and its directory durable before adoption.
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		err = file.Sync()
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		return syncFreshDirectory(dir)
	} else if !os.IsNotExist(err) {
		return err
	}
	return writeFreshFile(path, wire)
}

func prepareFreshEvidence(dir string, p installstate.Plan, resume bool) error {
	if !resume {
		if err := os.Mkdir(dir, 0o700); err != nil {
			return fmt.Errorf("new private evidence directory: %w", err)
		}
		if err := syncFreshDirectory(filepath.Dir(dir)); err != nil {
			return err
		}
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("evidence directory must be private and must not be a symlink")
	}
	wire, err := json.Marshal(p)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "installation-plan.json")
	if resume {
		old, err := readFreshFile(path, installstate.MaximumBytes)
		if os.IsNotExist(err) {
			// Before this file was durable the DB could not have been claimed.
			return writeFreshFile(path, wire)
		}
		if err != nil {
			return err
		}
		if !bytes.Equal(old, wire) {
			return fmt.Errorf("evidence directory belongs to another installation")
		}
		return nil
	}
	return writeFreshFile(path, wire)
}

func readFreshFile(path string, maximum int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() > maximum {
		return nil, fmt.Errorf("Plan must be a bounded private regular file: %s", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	wire, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err == nil && int64(len(wire)) > maximum {
		err = fmt.Errorf("Plan exceeds size limit")
	}
	return wire, err
}

func writeFreshFile(path string, wire []byte) error {
	if err := writeNewPublicationPlanFile(path, wire); err != nil {
		return err
	}
	return syncFreshDirectory(filepath.Dir(path))
}

func syncFreshDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
