package main

import (
	"context"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/config"
)

func TestResetCLIExposesPreviewOnlyAndValidatesBeforeDatabaseAccess(t *testing.T) {
	cfg := &config.Config{}
	for _, args := range [][]string{
		nil,
		{"apply"},
		{"preview"},
		{"preview", "--next-source-instance", "source-next", "unexpected"},
	} {
		err := runResetCLI(context.Background(), args, cfg)
		if err == nil {
			t.Fatalf("args %v unexpectedly accepted", args)
		}
		if args != nil && len(args) > 0 && args[0] == "apply" && !strings.Contains(err.Error(), "preview") {
			t.Fatalf("apply rejection did not preserve preview-only boundary: %v", err)
		}
	}
}
