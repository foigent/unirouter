package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/redisx"
	"github.com/uniroute/unirouter/internal/resetexec"
	"github.com/uniroute/unirouter/internal/resetplan"
	"github.com/uniroute/unirouter/internal/schemaauthority"
)

const (
	resetPlanMaximumBytes     = 8 << 20
	resetEvidenceMaximumBytes = 4 << 20
	resetAuthorizationWindow  = 24 * time.Hour
	resetApplyConfirmation    = "DELETE_PRODUCT_DATA_KEEP_EXACT_ADMIN"
)

var resetAuthorizationIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,159}$`)

func runResetCLI(ctx context.Context, args []string, cfg *config.Config) error {
	if len(args) == 0 {
		return resetUsageError()
	}
	switch args[0] {
	case "preview":
		return runResetPreviewCLI(ctx, args[1:], cfg)
	case "apply":
		return runResetApplyCLI(ctx, args[1:], cfg)
	default:
		return resetUsageError()
	}
}

func runResetPreviewCLI(ctx context.Context, args []string, cfg *config.Config) error {
	fs := flag.NewFlagSet("reset preview", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	nextSourceInstance := fs.String("next-source-instance", "", "new completed-execution Source Instance for the post-reset epoch")
	targetTree := fs.String("target-tree", "", "exact 40-character source tree of the running artifact")
	preservedLogin := fs.String("preserve-local-login", "", "exact normalized local account to preserve")
	output := fs.String("output", "-", "write JSON to a new 0600 file, or - for standard output")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return fmt.Errorf("invalid reset preview arguments")
	}
	if strings.TrimSpace(*nextSourceInstance) == "" || strings.TrimSpace(*targetTree) == "" {
		return fmt.Errorf("--next-source-instance and --target-tree are required")
	}
	if *preservedLogin != resetplan.PreservedLogin {
		return fmt.Errorf("--preserve-local-login must be exactly %q", resetplan.PreservedLogin)
	}
	if strings.TrimSpace(*output) == "" {
		return fmt.Errorf("--output must be a new file path or -")
	}
	expectedSchemaLedger, err := resetExpectedSchemaLedger(cfg)
	if err != nil {
		return err
	}

	postgres, redisClient, closeRuntime, err := openResetRuntime(cfg)
	if err != nil {
		return err
	}
	defer closeRuntime()
	plan, err := resetexec.Preview(
		ctx,
		postgres.Pool,
		redisClient.Client,
		expectedSchemaLedger,
		resetplan.TargetIdentity{
			RuntimeVersion: cfg.RuntimeVersion,
			RuntimeCommit:  cfg.RuntimeCommit,
			SourceTree:     strings.TrimSpace(*targetTree),
		},
		cfg.CompletedExecutionFeedSourceInstance,
		*nextSourceInstance,
		time.Now(),
	)
	if err != nil {
		return err
	}
	encoded, err := resetplan.Marshal(plan)
	if err != nil {
		return fmt.Errorf("encode reset preview: %w", err)
	}
	encoded = append(encoded, '\n')
	if *output == "-" {
		_, err = os.Stdout.Write(encoded)
		return err
	}
	if err := writeNewPrivateFile(*output, encoded); err != nil {
		return err
	}
	fmt.Printf("reset preview written; artifact_sha256=%s scope_sha256=%s mutation_performed=false\n", resetplan.ArtifactSHA256(encoded), plan.ScopeSHA256)
	return nil
}

func runResetApplyCLI(ctx context.Context, args []string, cfg *config.Config) error {
	fs := flag.NewFlagSet("reset apply", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	planFile := fs.String("plan-file", "", "final reviewed reset preview artifact")
	planSHA := fs.String("plan-sha256", "", "exact SHA-256 of final plan artifact")
	scopeSHA := fs.String("scope-sha256", "", "stable semantic reset-scope SHA-256")
	targetTree := fs.String("target-tree", "", "exact 40-character source tree of the running artifact")
	backupFile := fs.String("backup-file", "", "fresh pre-reset rollback backup")
	backupSHA := fs.String("backup-sha256", "", "exact SHA-256 of fresh rollback backup")
	restoreEvidenceFile := fs.String("isolated-restore-evidence-file", "", "reviewed isolated restore verification artifact")
	restoreEvidenceSHA := fs.String("isolated-restore-evidence-sha256", "", "exact SHA-256 of restore verification artifact")
	targetGateEvidenceFile := fs.String("target-gate-evidence-file", "", "reviewed exact-main CI, TLS, License, and capacity gate artifact")
	targetGateEvidenceSHA := fs.String("target-gate-evidence-sha256", "", "exact SHA-256 of target gate artifact")
	quiesceEvidenceFile := fs.String("quiesce-evidence-file", "", "reviewed quiescence evidence artifact")
	quiesceEvidenceSHA := fs.String("quiesce-evidence-sha256", "", "exact SHA-256 of quiescence evidence artifact")
	epochEvidenceFile := fs.String("consumer-epoch-evidence-file", "", "reviewed consumer old-cursor and next-source non-use evidence")
	epochEvidenceSHA := fs.String("consumer-epoch-evidence-sha256", "", "exact SHA-256 of consumer epoch evidence")
	authorizedAt := fs.String("authorized-at", "", "RFC3339 user confirmation time")
	authorizationID := fs.String("authorization-id", "", "coordinator-issued DATA_RESET delete-baton identifier")
	confirm := fs.String("confirm", "", "literal destructive-action confirmation")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return fmt.Errorf("invalid reset apply arguments")
	}
	if *confirm != resetApplyConfirmation {
		return fmt.Errorf("--confirm must be exactly %q", resetApplyConfirmation)
	}
	if !resetAuthorizationIDPattern.MatchString(*authorizationID) {
		return fmt.Errorf("--authorization-id is missing or invalid")
	}
	confirmationTime, err := time.Parse(time.RFC3339, *authorizedAt)
	if err != nil {
		return fmt.Errorf("--authorized-at must be RFC3339")
	}
	now := time.Now()
	if confirmationTime.After(now.Add(5*time.Minute)) || now.Sub(confirmationTime) > resetAuthorizationWindow {
		return fmt.Errorf("DATA_RESET authorization timestamp is outside the supported window")
	}
	expectedSchemaLedger, err := resetExpectedSchemaLedger(cfg)
	if err != nil {
		return err
	}

	planPayload, err := resetexec.VerifyFileSHA256(*planFile, *planSHA, resetPlanMaximumBytes)
	if err != nil {
		return fmt.Errorf("verify reset plan: %w", err)
	}
	plan, err := resetplan.Unmarshal(planPayload)
	if err != nil {
		return err
	}
	if plan.ScopeSHA256 != *scopeSHA {
		return fmt.Errorf("RESET_PLAN_DRIFT_DETECTED: supplied scope digest does not match final plan")
	}
	for _, artifact := range []struct {
		label    string
		path     string
		digest   string
		maxBytes int64
	}{
		{label: "fresh backup", path: *backupFile, digest: *backupSHA},
		{label: "isolated restore evidence", path: *restoreEvidenceFile, digest: *restoreEvidenceSHA, maxBytes: resetEvidenceMaximumBytes},
		{label: "target release gate evidence", path: *targetGateEvidenceFile, digest: *targetGateEvidenceSHA, maxBytes: resetEvidenceMaximumBytes},
		{label: "quiescence evidence", path: *quiesceEvidenceFile, digest: *quiesceEvidenceSHA, maxBytes: resetEvidenceMaximumBytes},
		{label: "consumer epoch evidence", path: *epochEvidenceFile, digest: *epochEvidenceSHA, maxBytes: resetEvidenceMaximumBytes},
	} {
		if _, err := resetexec.VerifyFileSHA256(artifact.path, artifact.digest, artifact.maxBytes); err != nil {
			return fmt.Errorf("verify %s: %w", artifact.label, err)
		}
	}

	effectiveTarget := resetplan.TargetIdentity{
		RuntimeVersion: cfg.RuntimeVersion,
		RuntimeCommit:  cfg.RuntimeCommit,
		SourceTree:     strings.TrimSpace(*targetTree),
	}
	postgres, redisClient, closeRuntime, err := openResetRuntime(cfg)
	if err != nil {
		return err
	}
	defer closeRuntime()
	result, err := resetexec.Apply(
		ctx,
		postgres.Pool,
		redisClient.Client,
		expectedSchemaLedger,
		plan,
		effectiveTarget,
		cfg.CompletedExecutionFeedSourceInstance,
	)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("encode reset result: %w", err)
	}
	fmt.Println(string(encoded))
	return nil
}

func resetExpectedSchemaLedger(cfg *config.Config) ([]string, error) {
	if cfg == nil {
		return nil, fmt.Errorf("reset runtime configuration is required")
	}
	migrationsDir := strings.TrimSpace(cfg.MigrationsDir)
	if migrationsDir == "" {
		migrationsDir = findMigrationsDir()
	}
	expected, err := schemaauthority.ExpectedLedger(migrationsDir)
	if err != nil {
		return nil, fmt.Errorf("load reset schema authority: %w", err)
	}
	return expected, nil
}

func openResetRuntime(cfg *config.Config) (*db.Postgres, *redisx.Redis, func(), error) {
	if cfg == nil {
		return nil, nil, func() {}, fmt.Errorf("reset runtime configuration is required")
	}
	postgres, err := db.NewPostgres(cfg.DB_DSN, cfg.DBMaxConnections, cfg.DBMaxIdleConns)
	if err != nil {
		return nil, nil, func() {}, fmt.Errorf("connect PostgreSQL: %w", err)
	}
	redisClient, err := redisx.NewRedis(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB, cfg.RedisMaxConns)
	if err != nil {
		postgres.Close()
		return nil, nil, func() {}, fmt.Errorf("connect Redis: %w", err)
	}
	closeRuntime := func() {
		_ = redisClient.Close()
		postgres.Close()
	}
	return postgres, redisClient, closeRuntime, nil
}

func writeNewPrivateFile(path string, payload []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create reset preview without overwrite: %w", err)
	}
	if _, err = file.Write(payload); err != nil {
		_ = file.Close()
		return fmt.Errorf("write reset preview: %w", err)
	}
	if err = file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync reset preview: %w", err)
	}
	if err = file.Close(); err != nil {
		return fmt.Errorf("close reset preview: %w", err)
	}
	return nil
}

func resetUsageError() error {
	return fmt.Errorf("usage: uniroute reset <preview|apply>; apply requires a separately issued DATA_RESET delete baton")
}
