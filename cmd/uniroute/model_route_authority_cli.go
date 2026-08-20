package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/modelrouteauthority"
	"github.com/uniroute/unirouter/internal/modelrouting"
)

func runModelRouteAuthorityCLI(ctx context.Context, args []string, cfg *config.Config) error {
	if cfg == nil || len(args) == 0 {
		return modelRouteAuthorityUsageError()
	}
	action := args[0]
	if action != "check" && action != "apply" {
		return modelRouteAuthorityUsageError()
	}
	fs := flag.NewFlagSet("model-route-authority "+action, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	customerOrgID := fs.Int64("customer-org-id", 0, "sole active root customer organization id")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 || *customerOrgID <= 0 {
		return modelRouteAuthorityUsageError()
	}
	mode := modelrouting.Mode(cfg.ModelRouteAuthority)
	if action == "apply" && mode != modelrouting.ModeVerify {
		return modelrouteauthority.ErrApplyMode
	}
	postgres, err := db.NewPostgres(cfg.DB_DSN, cfg.DBMaxConnections, cfg.DBMaxIdleConns)
	if err != nil {
		return fmt.Errorf("connect PostgreSQL: %w", err)
	}
	defer postgres.Close()
	service, err := modelrouteauthority.New(postgres.Pool, cfg.RoutingPolicyConfig())
	if err != nil {
		return err
	}
	if action == "check" {
		report, checkErr := service.Check(ctx, modelrouting.AuthorityCheckRequest{
			Mode: mode, CustomerOrgID: *customerOrgID,
		})
		if err := writeModelRouteAuthorityJSON(report); err != nil {
			return err
		}
		return checkErr
	}
	report, applyErr := service.Apply(ctx, modelrouteauthority.ApplyRequest{
		Mode: mode, CustomerOrgID: *customerOrgID,
	})
	if err := writeModelRouteAuthorityJSON(report); err != nil {
		return err
	}
	return applyErr
}

func writeModelRouteAuthorityJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(true)
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("write model route authority report: %w", err)
	}
	return nil
}

func modelRouteAuthorityUsageError() error {
	return errors.New("usage: uniroute model-route-authority <check|apply> --customer-org-id <positive-id>")
}
