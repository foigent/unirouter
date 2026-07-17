package main

import (
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/releasetrust"
)

func TestValidateReleaseModeConfigKeepsDevelopmentCompatibleAndFailsReleaseClosed(t *testing.T) {
	cfg := &config.Config{
		JWTSecret:        "development-secret",
		APIKeyHashSecret: "development-api-secret",
	}
	if err := validateReleaseModeConfig(cfg, releasetrust.ModeDevelopment); err != nil {
		t.Fatalf("development mode must retain non-empty legacy secret compatibility: %v", err)
	}
	if err := validateReleaseModeConfig(cfg, releasetrust.ModeRelease); err == nil {
		t.Fatal("release mode must reject secrets shorter than 32 bytes")
	}

	cfg.JWTSecret = strings.Repeat("j", 32)
	cfg.APIKeyHashSecret = strings.Repeat("a", 32)
	if err := validateReleaseModeConfig(cfg, releasetrust.ModeRelease); err == nil {
		t.Fatal("release mode must require a deployment-wide notification Webhook signing secret")
	}
	cfg.NotificationWebhookSigningSecret = strings.Repeat("w", 32)
	if err := validateReleaseModeConfig(cfg, releasetrust.ModeRelease); err != nil {
		t.Fatalf("release mode rejected exact 32-byte secrets: %v", err)
	}
}
