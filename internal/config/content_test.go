package config

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestContentStorageConfigurationCombinations(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		content, full         bool
		root, identity, token string
		valid                 bool
	}{
		{"off", false, false, "", "", "", true},
		{"metadata", true, false, "", "", "", true},
		{"full_without_C", false, true, "/data/content", "synthetic", "/data/binding", false},
		{"full_without_store", true, true, "", "", "", false},
		{"partial", true, false, "/data/content", "", "", false},
		{"relative", true, true, "relative", "synthetic", "/data/binding", false},
		{"root", true, true, "/", "synthetic", "/data/binding", false},
		{"relative_token", true, true, "/data/content", "synthetic", "binding", false},
		{"full", true, true, "/data/content", "synthetic", "/data/binding", true},
		{"read_historical", true, false, "/data/content", "synthetic", "/data/binding", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := Config{SourceContentEnabled: tc.content, SourceContentFullEnabled: tc.full, SourceContentStorageRoot: tc.root, SourceContentStorageIdentity: tc.identity, SourceContentStorageTokenFile: tc.token}
			err := c.validateContentStorage()
			require.Equal(t, tc.valid, err == nil)
		})
	}
}

func TestContentFullConfigurationRequiresStrictBoolean(t *testing.T) {
	t.Setenv("SOURCE_CONTENT_FULL_ENABLED", "TRUE")
	_, err := Load()
	require.ErrorContains(t, err, "SOURCE_CONTENT_FULL_ENABLED")
}

func TestLoadContentDefaultOffAndRequiresSourceFacts(t *testing.T) {
	for _, tc := range []struct {
		value, source  string
		valid, enabled bool
	}{
		{"", "false", true, false}, {"false", "false", true, false},
		{"true", "false", false, false}, {"true", "true", true, true},
		{"yes", "true", false, false}, {"TRUE", "true", false, false},
	} {
		t.Run(tc.value+"/"+tc.source, func(t *testing.T) {
			t.Setenv("JWT_SECRET", "synthetic-jwt-secret")
			t.Setenv("API_KEY_HASH_SECRET", "synthetic-api-key-secret")
			t.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
			t.Setenv("SOURCE_FACTS_ENABLED", tc.source)
			t.Setenv("SOURCE_CONTENT_ENABLED", tc.value)
			t.Setenv("SOURCE_OBSERVATIONS_ENABLED", "false")
			t.Setenv("SOURCE_BUDGET_SNAPSHOTS_ENABLED", "false")
			t.Setenv("COMPLETED_EXECUTION_FEED_SOURCE_INSTANCE", "synthetic-C-config")
			cfg, err := Load()
			if !tc.valid {
				if err == nil {
					t.Fatal("invalid content enablement accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.SourceContentEnabled != tc.enabled {
				t.Fatal("content enablement mismatch")
			}
		})
	}
}
