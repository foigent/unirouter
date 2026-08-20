package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/migrate"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
)

type adminPasswordInput struct {
	stdin        io.Reader
	promptWriter io.Writer
	stdinFD      int
	isTerminal   func(int) bool
	readHidden   func(int) ([]byte, error)
}

func systemAdminPasswordInput() adminPasswordInput {
	return adminPasswordInput{
		stdin:        os.Stdin,
		promptWriter: os.Stderr,
		stdinFD:      int(os.Stdin.Fd()),
		isTerminal:   term.IsTerminal,
		readHidden:   term.ReadPassword,
	}
}

func runCLI(ctx context.Context, args []string, cfg *config.Config) (bool, error) {
	return runCLIWithAdminPasswordInput(ctx, args, cfg, systemAdminPasswordInput())
}

func runCLIWithAdminPasswordInput(ctx context.Context, args []string, cfg *config.Config, input adminPasswordInput) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	if containsPasswordArgument(args) {
		return true, passwordArgumentError()
	}

	switch args[0] {
	case "admin":
		return true, runAdminCLIWithPasswordInput(ctx, args[1:], cfg, input)
	case "migrate":
		return true, runMigrateCLI(ctx, args[1:], cfg)
	case "model-route-authority":
		return true, runModelRouteAuthorityCLI(ctx, args[1:], cfg)
	case "reset":
		return true, runResetCLI(ctx, args[1:], cfg)
	case "upgrade":
		return true, runUpgradeCLI(ctx, args[1:], cfg)
	default:
		return false, nil
	}
}

func runAdminCLI(ctx context.Context, args []string, cfg *config.Config) error {
	return runAdminCLIWithPasswordInput(ctx, args, cfg, systemAdminPasswordInput())
}

func runAdminCLIWithPasswordInput(ctx context.Context, args []string, cfg *config.Config, input adminPasswordInput) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: uniroute admin <create|set-password>")
	}
	if containsPasswordArgument(args) {
		return passwordArgumentError()
	}

	switch args[0] {
	case "create":
		fs := flag.NewFlagSet("admin create", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		username := fs.String("username", "", "admin username")
		passwordStdin := fs.Bool("password-stdin", false, "read the admin password from non-terminal standard input")
		role := fs.String("role", "admin", "admin role")
		orgID := fs.Int64("org-id", 1, "organization id")
		if err := fs.Parse(args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return fmt.Errorf("usage: uniroute admin create --username <name> [--role <role>] [--org-id <id>] [--password-stdin]")
			}
			return fmt.Errorf("invalid admin create arguments; supported flags: --username, --role, --org-id, --password-stdin")
		}
		if fs.NArg() != 0 {
			return fmt.Errorf("unexpected positional argument(s); passwords must use --password-stdin or the hidden prompt")
		}
		normalizedUsername, err := auth.NormalizeLocalUsername(*username)
		if err != nil {
			return fmt.Errorf("--username is required: %w", err)
		}
		organizationRole, compatibilityRole, err := normalizeAdminCreateRoles(*role)
		if err != nil {
			return err
		}
		password, err := readAdminPassword(input, *passwordStdin)
		if err != nil {
			return err
		}
		if isPublicDefaultAdminPassword(password) {
			return fmt.Errorf("refusing to create admin with the public default password")
		}
		if err := auth.ValidateLocalPassword(password); err != nil {
			return err
		}
		return createAdmin(ctx, cfg, normalizedUsername, password, organizationRole, compatibilityRole, *orgID)

	case "set-password":
		fs := flag.NewFlagSet("admin set-password", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		username := fs.String("username", "", "admin username")
		passwordStdin := fs.Bool("password-stdin", false, "read the new password from non-terminal standard input")
		orgID := fs.Int64("org-id", 1, "organization id")
		if err := fs.Parse(args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return fmt.Errorf("usage: uniroute admin set-password --username <name> [--org-id <id>] [--password-stdin]")
			}
			return fmt.Errorf("invalid admin set-password arguments; supported flags: --username, --org-id, --password-stdin")
		}
		if fs.NArg() != 0 {
			return fmt.Errorf("unexpected positional argument(s); passwords must use --password-stdin or the hidden prompt")
		}
		normalizedUsername, err := auth.NormalizeLocalUsername(*username)
		if err != nil {
			return fmt.Errorf("--username is required: %w", err)
		}
		password, err := readAdminPassword(input, *passwordStdin)
		if err != nil {
			return err
		}
		if isPublicDefaultAdminPassword(password) {
			return fmt.Errorf("refusing to set the public default admin password")
		}
		if err := auth.ValidateLocalPassword(password); err != nil {
			return err
		}
		return setAdminPassword(ctx, cfg, normalizedUsername, password, *orgID)

	default:
		return fmt.Errorf("unknown admin command; supported: create, set-password")
	}
}

func passwordArgumentError() error {
	return fmt.Errorf("--password is not supported because password values must not be passed in process arguments; use --password-stdin or omit it for a hidden prompt")
}

func containsPasswordArgument(args []string) bool {
	for _, arg := range args {
		if arg == "--password" || arg == "-password" || strings.HasPrefix(arg, "--password=") || strings.HasPrefix(arg, "-password=") {
			return true
		}
	}
	return false
}

func readAdminPassword(input adminPasswordInput, passwordStdin bool) (string, error) {
	if passwordStdin {
		if input.isTerminal != nil && input.isTerminal(input.stdinFD) {
			return "", fmt.Errorf("--password-stdin requires non-terminal standard input; omit it to use the hidden prompt")
		}
		return readAdminPasswordLine(input.stdin)
	}
	if input.isTerminal == nil || !input.isTerminal(input.stdinFD) {
		return "", fmt.Errorf("standard input is not a terminal; use --password-stdin for non-interactive input")
	}
	if input.promptWriter == nil || input.readHidden == nil {
		return "", fmt.Errorf("hidden password input is unavailable")
	}

	first, err := readHiddenAdminPassword(input, "New password: ")
	if err != nil {
		return "", err
	}
	second, err := readHiddenAdminPassword(input, "Confirm new password: ")
	if err != nil {
		return "", err
	}
	if first != second {
		return "", fmt.Errorf("password confirmation does not match")
	}
	return first, nil
}

func readHiddenAdminPassword(input adminPasswordInput, prompt string) (string, error) {
	if _, err := fmt.Fprint(input.promptWriter, prompt); err != nil {
		return "", fmt.Errorf("write password prompt: %w", err)
	}
	raw, readErr := input.readHidden(input.stdinFD)
	_, newlineErr := fmt.Fprintln(input.promptWriter)
	if readErr != nil {
		return "", fmt.Errorf("read hidden password: %w", readErr)
	}
	if newlineErr != nil {
		return "", fmt.Errorf("write password prompt newline: %w", newlineErr)
	}
	if len(raw) > auth.MaxLocalPasswordBytes {
		clear(raw)
		return "", fmt.Errorf("password input exceeds %d bytes", auth.MaxLocalPasswordBytes)
	}
	password := string(raw)
	clear(raw)
	return password, nil
}

func readAdminPasswordLine(input io.Reader) (string, error) {
	if input == nil {
		return "", fmt.Errorf("password standard input is unavailable")
	}
	reader := bufio.NewReaderSize(input, auth.MaxLocalPasswordBytes+2)
	line, err := reader.ReadSlice('\n')
	defer clear(line)
	if errors.Is(err, bufio.ErrBufferFull) {
		return "", fmt.Errorf("password input exceeds %d bytes", auth.MaxLocalPasswordBytes)
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read password from standard input: %w", err)
	}
	if len(line) > 0 && line[len(line)-1] == '\n' {
		line = line[:len(line)-1]
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
		if _, extraErr := reader.ReadByte(); extraErr == nil {
			return "", fmt.Errorf("password standard input must contain exactly one line")
		} else if !errors.Is(extraErr, io.EOF) {
			return "", fmt.Errorf("read password from standard input: %w", extraErr)
		}
	}
	if len(line) > auth.MaxLocalPasswordBytes {
		return "", fmt.Errorf("password input exceeds %d bytes", auth.MaxLocalPasswordBytes)
	}
	return string(line), nil
}

func runMigrateCLI(ctx context.Context, args []string, cfg *config.Config) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: uniroute migrate <up|status>")
	}

	postgres, err := db.NewPostgres(cfg.DB_DSN, cfg.DBMaxConnections, cfg.DBMaxIdleConns)
	if err != nil {
		return fmt.Errorf("connect PostgreSQL: %w", err)
	}
	defer postgres.Close()

	migrationsDir := cfg.MigrationsDir
	if migrationsDir == "" {
		migrationsDir = findMigrationsDir()
	}

	switch args[0] {
	case "up":
		return migrate.Run(ctx, postgres.Pool, migrationsDir)
	case "status":
		return printMigrationStatus(ctx, cfg, migrationsDir)
	default:
		return fmt.Errorf("unknown migrate command %q; supported: up, status", args[0])
	}
}

func normalizeAdminCreateRoles(role string) (organizationRole, compatibilityRole string, err error) {
	if !auth.IsOrganizationRole(role) {
		return "", "", fmt.Errorf("invalid --role: expected platform_admin, org_admin, billing_admin, org_viewer, or member (admin is a compatibility alias for platform_admin)")
	}
	organizationRole, ok := auth.NormalizeOrganizationRole(role)
	if !ok {
		return "", "", fmt.Errorf("invalid --role")
	}
	compatibilityRole = organizationRole
	if role == auth.RoleLegacyAdmin {
		compatibilityRole = auth.RoleLegacyAdmin
	}
	return organizationRole, compatibilityRole, nil
}

func createAdmin(ctx context.Context, cfg *config.Config, username, password, organizationRole, compatibilityRole string, orgID int64) error {
	postgres, err := db.NewPostgres(cfg.DB_DSN, cfg.DBMaxConnections, cfg.DBMaxIdleConns)
	if err != nil {
		return fmt.Errorf("connect PostgreSQL: %w", err)
	}
	defer postgres.Close()

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	_, err = postgres.Pool.Exec(ctx,
		`INSERT INTO users (org_id, username, password_hash, organization_role, role, status, auth_provider, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, 'active', 'local', NOW(), NOW())`,
		orgID, username, string(hash), organizationRole, compatibilityRole,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" &&
			(pgErr.ConstraintName == "users_local_login_id_unique" || pgErr.ConstraintName == "users_org_username_unique") {
			return fmt.Errorf("local login ID is unavailable")
		}
		return fmt.Errorf("create admin user: %w", err)
	}
	fmt.Printf("admin user %q created for org %d\n", username, orgID)
	return nil
}

func setAdminPassword(ctx context.Context, cfg *config.Config, username, password string, orgID int64) error {
	postgres, err := db.NewPostgres(cfg.DB_DSN, cfg.DBMaxConnections, cfg.DBMaxIdleConns)
	if err != nil {
		return fmt.Errorf("connect PostgreSQL: %w", err)
	}
	defer postgres.Close()

	if err := setAdminPasswordWithDB(ctx, postgres.Pool, username, password, orgID); err != nil {
		return err
	}
	fmt.Printf("password updated for admin user %q in org %d\n", username, orgID)
	return nil
}

func setAdminPasswordWithDB(ctx context.Context, q db.DBQuerier, username, password string, orgID int64) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	var changed, audited bool
	err = q.QueryRow(ctx,
		`WITH candidates AS MATERIALIZED (
		    SELECT id, org_id
		      FROM users
		     WHERE org_id = $2
		       AND COALESCE(NULLIF(auth_provider, ''), 'local') = 'local'
		       AND public.normalize_local_login_id(username) = public.normalize_local_login_id($3)
		     ORDER BY id
		     LIMIT 2
		), singleton AS (
		    SELECT MIN(id) AS id, MIN(org_id) AS org_id
		      FROM candidates
		    HAVING COUNT(*) = 1
		), changed AS (
		    UPDATE users AS target
		       SET password_hash = $1,
		           auth_epoch = auth_epoch + 1,
		           status = 'active',
		           updated_at = NOW()
		      FROM singleton
		     WHERE target.id = singleton.id
		 RETURNING target.id, target.org_id
		), audited AS (
		    INSERT INTO audit_logs (
		        org_id, user_id, action, resource, resource_id, detail, source,
		        before_state, after_state, created_at
		    )
		    SELECT org_id, id, 'user.password_reset', 'user', id::text,
		           '{"reason":"recovery_cli","sessions_revoked":"all"}'::jsonb,
		           'system', '{}'::jsonb,
		           '{"password_reset":true,"sessions_revoked":"all"}'::jsonb,
		           NOW()
		      FROM changed
		 RETURNING 1
		)
		SELECT EXISTS (SELECT 1 FROM changed), EXISTS (SELECT 1 FROM audited)`,
		string(hash), orgID, username,
	).Scan(&changed, &audited)
	if err != nil {
		return fmt.Errorf("set admin password: %w", err)
	}
	if !changed {
		return fmt.Errorf("admin user %q not found in org %d", username, orgID)
	}
	if !audited {
		return fmt.Errorf("password reset audit missing for admin user %q in org %d", username, orgID)
	}
	return nil
}

func printMigrationStatus(ctx context.Context, cfg *config.Config, migrationsDir string) error {
	postgres, err := db.NewPostgres(cfg.DB_DSN, cfg.DBMaxConnections, cfg.DBMaxIdleConns)
	if err != nil {
		return fmt.Errorf("connect PostgreSQL: %w", err)
	}
	defer postgres.Close()

	applied := make(map[string]bool)
	rows, err := postgres.Pool.Query(ctx, `SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		if strings.Contains(err.Error(), "schema_migrations") {
			fmt.Println("schema_migrations table does not exist; no migrations recorded")
			return nil
		}
		return fmt.Errorf("query schema_migrations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return fmt.Errorf("scan migration version: %w", err)
		}
		applied[version] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read migration versions: %w", err)
	}

	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("read migrations dir %s: %w", migrationsDir, err)
	}
	var versions []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasSuffix(name, ".up.sql") {
			versions = append(versions, strings.TrimSuffix(name, ".up.sql"))
		}
	}
	sort.Strings(versions)
	for _, version := range versions {
		state := "pending"
		if applied[version] {
			state = "applied"
		}
		fmt.Printf("%s\t%s\t%s\n", state, version, filepath.Join(migrationsDir, version+".up.sql"))
	}
	return nil
}

func isPublicDefaultAdminPassword(password string) bool {
	return password == "admin123456"
}
