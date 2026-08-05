package main

import (
	"context"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/config"
)

func TestResetCLIRequiresExplicitSubcommandAndCompletePreviewIdentity(t *testing.T) {
	cfg := &config.Config{}
	for _, args := range [][]string{
		nil,
		{"unknown"},
		{"preview"},
		{"preview", "--next-source-instance", "source-next", "--target-tree", strings.Repeat("a", 40)},
		{"preview", "--next-source-instance", "source-next", "--target-tree", strings.Repeat("a", 40), "--preserve-local-login", "other"},
	} {
		if err := runResetCLI(context.Background(), args, cfg); err == nil {
			t.Fatalf("args %v unexpectedly accepted", args)
		}
	}
}

func TestResetApplyRejectsIncompleteAuthorizationBeforeRuntimeAccess(t *testing.T) {
	cfg := &config.Config{}
	for _, args := range [][]string{
		{"apply"},
		{"apply", "--confirm", resetApplyConfirmation},
		{"apply", "--confirm", "wrong", "--authorization-id", "baton-1234", "--authorized-at", "2026-08-05T00:00:00Z"},
	} {
		err := runResetCLI(context.Background(), args, cfg)
		if err == nil {
			t.Fatalf("args %v unexpectedly accepted", args)
		}
		if strings.Contains(err.Error(), "connect PostgreSQL") || strings.Contains(err.Error(), "connect Redis") {
			t.Fatalf("incomplete authorization reached runtime access: %v", err)
		}
	}
}
