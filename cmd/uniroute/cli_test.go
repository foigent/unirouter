package main

import (
	"context"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/config"
)

func TestRunCLI_IgnoresServerMode(t *testing.T) {
	handled, err := runCLI(context.Background(), nil, &config.Config{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if handled {
		t.Fatal("expected no CLI handling without args")
	}
}

func TestRunCLI_AdminSetPasswordRequiresPassword(t *testing.T) {
	handled, err := runCLI(context.Background(), []string{"admin", "set-password", "--username", "admin"}, &config.Config{})
	if !handled {
		t.Fatal("expected admin CLI to be handled")
	}
	if err == nil || !strings.Contains(err.Error(), "--username and --password are required") {
		t.Fatalf("expected missing password error, got %v", err)
	}
}

func TestRunCLI_AdminRefusesDefaultPassword(t *testing.T) {
	handled, err := runCLI(context.Background(), []string{"admin", "set-password", "--username", "admin", "--password", "admin123456"}, &config.Config{})
	if !handled {
		t.Fatal("expected admin CLI to be handled")
	}
	if err == nil || !strings.Contains(err.Error(), "public default admin password") {
		t.Fatalf("expected default password refusal, got %v", err)
	}
}
