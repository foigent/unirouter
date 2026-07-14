package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/migrate"
	"golang.org/x/crypto/bcrypt"
)

func runCLI(ctx context.Context, args []string, cfg *config.Config) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}

	switch args[0] {
	case "admin":
		return true, runAdminCLI(ctx, args[1:], cfg)
	case "migrate":
		return true, runMigrateCLI(ctx, args[1:], cfg)
	case "upgrade":
		return true, runUpgradeCLI(ctx, args[1:], cfg)
	default:
		return false, nil
	}
}

func runAdminCLI(ctx context.Context, args []string, cfg *config.Config) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: uniroute admin <create|set-password>")
	}

	switch args[0] {
	case "create":
		fs := flag.NewFlagSet("admin create", flag.ContinueOnError)
		username := fs.String("username", "", "admin username")
		password := fs.String("password", "", "admin password")
		role := fs.String("role", "admin", "admin role")
		orgID := fs.Int64("org-id", 1, "organization id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *username == "" || *password == "" {
			return fmt.Errorf("--username and --password are required")
		}
		if isDefaultAdminPassword(*username, *password) {
			return fmt.Errorf("refusing to create admin with the public default password")
		}
		return createAdmin(ctx, cfg, *username, *password, *role, *orgID)

	case "set-password":
		fs := flag.NewFlagSet("admin set-password", flag.ContinueOnError)
		username := fs.String("username", "", "admin username")
		password := fs.String("password", "", "new password")
		orgID := fs.Int64("org-id", 1, "organization id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *username == "" || *password == "" {
			return fmt.Errorf("--username and --password are required")
		}
		if isDefaultAdminPassword(*username, *password) {
			return fmt.Errorf("refusing to set the public default admin password")
		}
		return setAdminPassword(ctx, cfg, *username, *password, *orgID)

	default:
		return fmt.Errorf("unknown admin command %q", args[0])
	}
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

func createAdmin(ctx context.Context, cfg *config.Config, username, password, role string, orgID int64) error {
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
		`INSERT INTO users (org_id, username, password_hash, role, status, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, 'active', NOW(), NOW())`,
		orgID, username, string(hash), role,
	)
	if err != nil {
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

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	tag, err := postgres.Pool.Exec(ctx,
		`UPDATE users SET password_hash = $1, status = 'active', updated_at = NOW()
		 WHERE org_id = $2 AND username = $3`,
		string(hash), orgID, username,
	)
	if err != nil {
		return fmt.Errorf("set admin password: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("admin user %q not found in org %d", username, orgID)
	}
	fmt.Printf("password updated for admin user %q in org %d\n", username, orgID)
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

func isDefaultAdminPassword(username, password string) bool {
	return strings.EqualFold(username, "admin") && password == "admin123456"
}
