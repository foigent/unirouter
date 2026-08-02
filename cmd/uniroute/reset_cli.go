package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/resetplan"
)

func runResetCLI(ctx context.Context, args []string, cfg *config.Config) error {
	if len(args) == 0 || args[0] != "preview" {
		return fmt.Errorf("usage: uniroute reset preview --next-source-instance <stable-id> [--output <new-file-or-dash>]")
	}
	fs := flag.NewFlagSet("reset preview", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	nextSourceInstance := fs.String("next-source-instance", "", "new completed-execution Source Instance for the post-reset epoch")
	output := fs.String("output", "-", "write JSON to a new 0600 file, or - for standard output")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
		return fmt.Errorf("invalid reset preview arguments; supported flags: --next-source-instance, --output")
	}
	if strings.TrimSpace(*nextSourceInstance) == "" {
		return fmt.Errorf("--next-source-instance is required")
	}
	if strings.TrimSpace(*output) == "" {
		return fmt.Errorf("--output must be a new file path or -")
	}

	postgres, err := db.NewPostgres(cfg.DB_DSN, cfg.DBMaxConnections, cfg.DBMaxIdleConns)
	if err != nil {
		return fmt.Errorf("connect PostgreSQL: %w", err)
	}
	defer postgres.Close()

	inventory, err := inspectResetInventory(ctx, postgres.Pool)
	if err != nil {
		return err
	}
	plan, err := resetplan.Build(
		inventory,
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
	file, err := os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create reset preview without overwrite: %w", err)
	}
	if _, err = file.Write(encoded); err != nil {
		_ = file.Close()
		return fmt.Errorf("write reset preview: %w", err)
	}
	if err = file.Close(); err != nil {
		return fmt.Errorf("close reset preview: %w", err)
	}
	fmt.Printf("reset preview written to %s (%s); no mutation performed\n", *output, plan.PlanSHA256)
	return nil
}

type resetInventoryPool interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

func inspectResetInventory(ctx context.Context, pool resetInventoryPool) ([]resetplan.TableInventory, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("begin reset preview snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
		SELECT tablename
		  FROM pg_catalog.pg_tables
		 WHERE schemaname = 'public'
		 ORDER BY tablename`)
	if err != nil {
		return nil, fmt.Errorf("inventory public tables: %w", err)
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return nil, fmt.Errorf("read public table inventory: %w", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("read public table inventory: %w", err)
	}
	rows.Close()

	inventory := make([]resetplan.TableInventory, 0, len(names))
	for _, name := range names {
		query := fmt.Sprintf("SELECT COUNT(*) FROM %s", pgx.Identifier{"public", name}.Sanitize())
		var count int64
		if err := tx.QueryRow(ctx, query).Scan(&count); err != nil {
			return nil, fmt.Errorf("count table %s: %w", name, err)
		}
		inventory = append(inventory, resetplan.TableInventory{Name: name, RowCount: count})
	}
	if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		return nil, fmt.Errorf("close reset preview snapshot: %w", err)
	}
	return inventory, nil
}
