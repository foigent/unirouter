package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/migrate"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	"github.com/uniroute/unirouter/internal/schemaauthority"
)

const maximumPublicationPlanBytes = 16 << 20

func runCapabilityContractPublicationCLI(ctx context.Context, args []string, cfg *config.Config) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: uniroute capability-contract-publication <install-baseline|plan|apply|guarded-down|post-commit-recovery-check>")
	}
	switch args[0] {
	case "check-bundle", "plan-v2", "apply-v2", "recover-v2":
		return runCapabilityPublicationV2CLI(ctx, args[0], args[1:], cfg)
	case "install-baseline":
		return runCapabilityContractPublicationInstallBaseline(ctx, args[1:], cfg)
	case "plan":
		return runCapabilityContractPublicationPlan(ctx, args[1:], cfg)
	case "apply":
		return runCapabilityContractPublicationApply(ctx, args[1:], cfg)
	case "guarded-down":
		return runCapabilityContractPublicationGuardedDown(ctx, args[1:], cfg)
	case "post-commit-recovery-check":
		return runCapabilityContractPublicationRecoveryCheck(ctx, args[1:], cfg)
	default:
		return fmt.Errorf("unknown capability-contract-publication command %q; supported: install-baseline, plan, apply, guarded-down, post-commit-recovery-check", args[0])
	}
}

func runCapabilityContractPublicationInstallBaseline(ctx context.Context, args []string, cfg *config.Config) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: uniroute capability-contract-publication install-baseline")
	}
	migrationsDir := configuredMigrationsDir(cfg)
	artifactLedger, err := schemaauthority.ExpectedLedger(migrationsDir)
	if err != nil {
		return err
	}
	if err := publication.VerifyArtifactPublicationInventory(migrationsDir, artifactLedger); err != nil {
		return err
	}
	baselineIndex := -1
	for index, version := range artifactLedger {
		if version == publication.BaselineMigrationM100 {
			baselineIndex = index
			break
		}
	}
	if baselineIndex < 0 {
		return fmt.Errorf("artifact does not contain exact Publication V1 baseline %s", publication.BaselineMigrationM100)
	}
	baselineLedger := append([]string(nil), artifactLedger[:baselineIndex+1]...)
	postgres, err := db.NewPostgres(cfg.DB_DSN, cfg.DBMaxConnections, cfg.DBMaxIdleConns)
	if err != nil {
		return fmt.Errorf("connect PostgreSQL: %w", err)
	}
	defer postgres.Close()
	if err := migrate.RunExistingThrough(ctx, postgres.Pool, migrationsDir, publication.BaselineMigrationM100); err != nil {
		return err
	}
	actualLedger, err := readCapabilityPublicationDatabaseLedger(ctx, postgres.Pool)
	if err != nil {
		return err
	}
	if !equalStrings(actualLedger, baselineLedger) {
		return fmt.Errorf("Publication V1 baseline install stopped at unexpected ledger: actual=%v expected=%v", actualLedger, baselineLedger)
	}
	if err := publication.VerifyRuntimeStaticProjection(ctx, postgres.Pool, actualLedger); err != nil {
		return fmt.Errorf("verify installed Publication V1 baseline: %w", err)
	}
	fmt.Printf("publication baseline installed and verified: ceiling=%s\n", publication.BaselineMigrationM100)
	return nil
}

func runCapabilityContractPublicationPlan(ctx context.Context, args []string, cfg *config.Config) error {
	fs := flag.NewFlagSet("capability-contract-publication plan", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	publicationID := fs.String("publication", "", "exact compiled publication identity")
	output := fs.String("output", "", "new local plan artifact path")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return fmt.Errorf("usage: uniroute capability-contract-publication plan --publication <id> --output <new-path>")
	}
	if strings.TrimSpace(*publicationID) == "" || strings.TrimSpace(*output) == "" {
		return fmt.Errorf("--publication and --output are required")
	}
	inputs, err := resolvePublicationCommandInputs(cfg, *publicationID)
	if err != nil {
		return err
	}
	postgres, err := db.NewPostgres(cfg.DB_DSN, cfg.DBMaxConnections, cfg.DBMaxIdleConns)
	if err != nil {
		return fmt.Errorf("connect PostgreSQL: %w", err)
	}
	defer postgres.Close()
	plan, err := publication.PlanTarget(ctx, postgres.Pool, cfg.RuntimeEnvironment, inputs.authority)
	if err != nil {
		return err
	}
	wire, err := json.Marshal(publication.NormalizePlan(plan))
	if err != nil {
		return fmt.Errorf("encode publication plan: %w", err)
	}
	wire = append(wire, '\n')
	if err := writeNewPublicationPlanFile(*output, wire); err != nil {
		return err
	}
	if plan.Validation != publication.ValidationValid {
		return fmt.Errorf("publication plan %s contains conflicts; inspect %s", plan.PlanDigest, *output)
	}
	fmt.Printf("publication plan written: publication=%s digest=%s path=%s migrations=%s\n",
		plan.PublicationID, plan.PlanDigest, *output, inputs.migrationsDir)
	return nil
}

func runCapabilityContractPublicationApply(ctx context.Context, args []string, cfg *config.Config) error {
	fs := flag.NewFlagSet("capability-contract-publication apply", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	planPath := fs.String("plan", "", "exact validated local plan artifact")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || strings.TrimSpace(*planPath) == "" {
		return fmt.Errorf("usage: uniroute capability-contract-publication apply --plan <path>")
	}
	plan, err := readPublicationPlan(*planPath)
	if err != nil {
		return err
	}
	inputs, err := resolvePublicationCommandInputs(cfg, plan.PublicationID)
	if err != nil {
		return err
	}
	if err := publication.ValidatePlanAuthority(inputs.authority, plan); err != nil {
		return err
	}
	if err := publication.VerifyPlanArtifactFiles(inputs.migrationsDir, plan); err != nil {
		return err
	}
	hooks, err := publication.HooksForPlan(cfg.RuntimeEnvironment, inputs.authority, plan)
	if err != nil {
		return err
	}
	postgres, err := db.NewPostgres(cfg.DB_DSN, cfg.DBMaxConnections, cfg.DBMaxIdleConns)
	if err != nil {
		return fmt.Errorf("connect PostgreSQL: %w", err)
	}
	defer postgres.Close()
	if err := migrate.RunPublication(ctx, postgres.Pool, inputs.migrationsDir, plan.ApplyMigrations, hooks); err != nil {
		return err
	}
	fmt.Printf("publication applied and verified: publication=%s digest=%s\n", plan.PublicationID, plan.PlanDigest)
	return nil
}

func runCapabilityContractPublicationGuardedDown(ctx context.Context, args []string, cfg *config.Config) error {
	fs := flag.NewFlagSet("capability-contract-publication guarded-down", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	planPath := fs.String("plan", "", "original exact validated local Plan artifact")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || strings.TrimSpace(*planPath) == "" {
		return fmt.Errorf("usage: uniroute capability-contract-publication guarded-down --plan <path>")
	}
	plan, err := readPublicationPlan(*planPath)
	if err != nil {
		return err
	}
	inputs, err := resolvePublicationCommandInputs(cfg, plan.PublicationID)
	if err != nil {
		return err
	}
	if err := publication.ValidatePlanAuthority(inputs.authority, plan); err != nil {
		return err
	}
	if err := publication.VerifyPlanArtifactFiles(inputs.migrationsDir, plan); err != nil {
		return err
	}
	hooks, err := publication.DownHooksForPlan(cfg.RuntimeEnvironment, inputs.authority, plan)
	if err != nil {
		return err
	}
	postgres, err := db.NewPostgres(cfg.DB_DSN, cfg.DBMaxConnections, cfg.DBMaxIdleConns)
	if err != nil {
		return fmt.Errorf("connect PostgreSQL: %w", err)
	}
	defer postgres.Close()
	if err := migrate.RunPublicationDown(ctx, postgres.Pool, inputs.migrationsDir, hooks); err != nil {
		return err
	}
	fmt.Printf("publication guarded down applied and verified: publication=%s digest=%s\n", plan.PublicationID, plan.PlanDigest)
	return nil
}

func runCapabilityContractPublicationRecoveryCheck(ctx context.Context, args []string, cfg *config.Config) error {
	fs := flag.NewFlagSet("capability-contract-publication post-commit-recovery-check", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	planPath := fs.String("plan", "", "original local plan artifact when available")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return fmt.Errorf("usage: uniroute capability-contract-publication post-commit-recovery-check [--plan <path>]")
	}
	migrationsDir := configuredMigrationsDir(cfg)
	ledger, err := schemaauthority.ExpectedLedger(migrationsDir)
	if err != nil {
		return err
	}
	if err := publication.VerifyArtifactPublicationInventory(migrationsDir, ledger); err != nil {
		return err
	}
	var plan *publication.Plan
	var planAuthority *publication.PlanAuthority
	var planMigrationsDir string
	if strings.TrimSpace(*planPath) != "" {
		decoded, err := readPublicationPlan(*planPath)
		if err != nil {
			return err
		}
		plan = &decoded
		inputs, err := resolvePublicationCommandInputs(cfg, decoded.PublicationID)
		if err != nil {
			return err
		}
		if err := publication.ValidatePlanAuthority(inputs.authority, decoded); err != nil {
			return err
		}
		planAuthority = &inputs.authority
		planMigrationsDir = inputs.migrationsDir
	}
	postgres, err := db.NewPostgres(cfg.DB_DSN, cfg.DBMaxConnections, cfg.DBMaxIdleConns)
	if err != nil {
		return fmt.Errorf("connect PostgreSQL: %w", err)
	}
	defer postgres.Close()
	var result publication.RecoveryCheckResult
	var checkErr error
	if err := migrate.WithSessionLock(ctx, postgres.Pool, func(conn *pgxpool.Conn) error {
		if plan != nil {
			if err := publication.VerifyPlanArtifactFiles(planMigrationsDir, *plan); err != nil {
				return err
			}
		}
		result, checkErr = publication.PostCommitRecoveryCheck(ctx, conn, cfg.RuntimeEnvironment, plan, ledger, planAuthority)
		return nil
	}); err != nil {
		return err
	}
	wire, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("encode recovery check: %w", err)
	}
	fmt.Println(string(wire))
	return checkErr
}

type publicationCommandInputs struct {
	migrationsDir string
	authority     publication.PlanAuthority
}

func resolvePublicationCommandInputs(cfg *config.Config, publicationID string) (publicationCommandInputs, error) {
	if cfg == nil {
		return publicationCommandInputs{}, fmt.Errorf("runtime config is required")
	}
	migrationsDir := configuredMigrationsDir(cfg)
	ledger, err := schemaauthority.ExpectedLedger(migrationsDir)
	if err != nil {
		return publicationCommandInputs{}, err
	}
	if err := publication.VerifyArtifactPublicationInventory(migrationsDir, ledger); err != nil {
		return publicationCommandInputs{}, err
	}
	declaration, predecessor, err := publication.CompiledPublication(publicationID)
	if err != nil {
		return publicationCommandInputs{}, err
	}
	if err := publication.VerifyGeneratedMigrationFiles(migrationsDir, predecessor, declaration); err != nil {
		return publicationCommandInputs{}, err
	}
	artifact, err := runtimePublicationArtifact(cfg)
	if err != nil {
		return publicationCommandInputs{}, err
	}
	files, err := publication.ReadArtifactMigrationFiles(migrationsDir, ledger)
	if err != nil {
		return publicationCommandInputs{}, err
	}
	downDigest, verifyDigest, err := publication.ReadPublicationAuxiliaryDigests(migrationsDir, publicationID)
	if err != nil {
		return publicationCommandInputs{}, err
	}
	return publicationCommandInputs{migrationsDir: migrationsDir, authority: publication.PlanAuthority{
		Artifact: artifact, ArtifactMigrationLedger: ledger, ArtifactMigrationFiles: files,
		PublicationDownSHA256: downDigest, PublicationVerifySHA256: verifyDigest,
		Predecessor: predecessor, Declaration: declaration,
	}}, nil
}

func runtimePublicationArtifact(cfg *config.Config) (publication.ArtifactIdentity, error) {
	digest, err := currentExecutableSHA256()
	if err != nil {
		return publication.ArtifactIdentity{}, err
	}
	artifact := publication.ArtifactIdentity{
		Kind: publication.ArtifactKindSource, Commit: strings.TrimSpace(cfg.RuntimeCommit),
		Tree: strings.TrimSpace(cfg.RuntimeTree), Version: strings.TrimSpace(cfg.RuntimeVersion),
		PublisherExecutableDigest: digest,
	}
	if imageDigest := strings.TrimSpace(cfg.RuntimeImageDigest); imageDigest != "" {
		artifact.Kind = publication.ArtifactKindImage
		artifact.ImageDigest = imageDigest
	}
	return artifact, nil
}

func currentExecutableSHA256() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve publisher executable: %w", err)
	}
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open publisher executable: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash publisher executable: %w", err)
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func readPublicationPlan(path string) (publication.Plan, error) {
	file, err := os.Open(path)
	if err != nil {
		return publication.Plan{}, fmt.Errorf("open publication plan: %w", err)
	}
	defer file.Close()
	wire, err := io.ReadAll(io.LimitReader(file, maximumPublicationPlanBytes+1))
	if err != nil {
		return publication.Plan{}, fmt.Errorf("read publication plan: %w", err)
	}
	if len(wire) > maximumPublicationPlanBytes {
		return publication.Plan{}, fmt.Errorf("publication plan exceeds %d bytes", maximumPublicationPlanBytes)
	}
	plan, err := publication.DecodePlan(wire)
	if err != nil {
		return publication.Plan{}, err
	}
	return plan, nil
}

func writeNewPublicationPlanFile(path string, wire []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("refusing to overwrite existing plan artifact %s", path)
		}
		return fmt.Errorf("create publication plan: %w", err)
	}
	wrote := false
	defer func() {
		_ = file.Close()
		if !wrote {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(wire); err != nil {
		return fmt.Errorf("write publication plan: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync publication plan: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close publication plan: %w", err)
	}
	wrote = true
	return nil
}

func configuredMigrationsDir(cfg *config.Config) string {
	if strings.TrimSpace(cfg.MigrationsDir) != "" {
		return cfg.MigrationsDir
	}
	return findMigrationsDir()
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func readCapabilityPublicationDatabaseLedger(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	rows, err := pool.Query(ctx, `SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		return nil, fmt.Errorf("query publication database ledger: %w", err)
	}
	defer rows.Close()
	var ledger []string
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("scan publication database ledger: %w", err)
		}
		ledger = append(ledger, version)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read publication database ledger: %w", err)
	}
	return ledger, nil
}
