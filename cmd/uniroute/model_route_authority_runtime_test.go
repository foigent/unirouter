package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/modelrouteauthority"
	"github.com/uniroute/unirouter/internal/modelrouting"
)

func TestLegacyStartupDoesNotRequireAuthorityDatabase(t *testing.T) {
	cfg := &config.Config{ModelRouteAuthority: modelrouting.ModeLegacy}
	report, err := enforceModelRouteAuthorityStartup(context.Background(), cfg, nil, 0)
	if err != nil || !report.Ready || report.Mode != modelrouting.ModeLegacy {
		t.Fatalf("legacy startup changed compatibility: report=%#v err=%v", report, err)
	}
}

func TestCanonicalStartupRejectsMissingCustomerBaselineBeforeDatabaseAccess(t *testing.T) {
	cfg := &config.Config{ModelRouteAuthority: modelrouting.ModeCanonical}
	report, err := enforceModelRouteAuthorityStartup(context.Background(), cfg, nil, 0)
	if err == nil || !strings.Contains(err.Error(), "requires a ready customer baseline") {
		t.Fatalf("fresh canonical startup did not fail closed: report=%#v err=%v", report, err)
	}
}

func TestModelRouteAuthorityCLIRequiresExplicitCustomer(t *testing.T) {
	cfg := &config.Config{}
	for _, args := range [][]string{
		nil,
		{"check"},
		{"apply", "--customer-org-id", "0"},
		{"unknown", "--customer-org-id", "1"},
	} {
		if err := runModelRouteAuthorityCLI(context.Background(), args, cfg); err == nil {
			t.Fatalf("runModelRouteAuthorityCLI(%v) accepted incomplete authority", args)
		}
	}
}

func TestModelRouteAuthorityCanonicalApplyIsRejectedBeforeDatabaseAccess(t *testing.T) {
	cfg := &config.Config{
		ModelRouteAuthority: modelrouting.ModeCanonical,
		DB_DSN:              "postgres://must-not-be-opened.invalid/unirouter",
	}
	err := runModelRouteAuthorityCLI(context.Background(), []string{
		"apply", "--customer-org-id", "41",
	}, cfg)
	if !errors.Is(err, modelrouteauthority.ErrApplyMode) {
		t.Fatalf("canonical apply error = %v, want %v", err, modelrouteauthority.ErrApplyMode)
	}
}
