package config

import "testing"

func TestLoadDBPoolLimitsPreservesExistingSemantics(t *testing.T) {
	for _, tc := range []struct {
		name, max, min   string
		wantMax, wantMin int
	}{
		{"defaults", "", "", 25, 5},
		{"explicit_small_pool", "4", "0", 4, 0},
		{"independent_override", "8", "", 8, 5},
		{"malformed_fallback", "bad", "bad", 25, 5},
		{"whitespace_not_trimmed", " 4", "0 ", 25, 5},
		{"signed_integers", "+4", "+0", 4, 0},
		{"zero_negative_not_newly_validated", "0", "-1", 0, -1},
		{"overflow_fallback", "999999999999999999999999", "999999999999999999999999", 25, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DB_MAX_CONNECTIONS", tc.max)
			t.Setenv("DB_MAX_IDLE_CONNECTIONS", tc.min)
			max, min := LoadDBPoolLimits()
			if max != tc.wantMax || min != tc.wantMin {
				t.Fatalf("pool limits = %d/%d; want %d/%d", max, min, tc.wantMax, tc.wantMin)
			}
			// Prove the real config entry point consumes the shared values.
			// These are synthetic local validation inputs; Load opens no services.
			t.Setenv("JWT_SECRET", "synthetic-pool-test")
			t.Setenv("API_KEY_HASH_SECRET", "synthetic-pool-test")
			t.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.DBMaxConnections != max || cfg.DBMaxIdleConns != min {
				t.Fatal("Load and shared pool limits differ")
			}
		})
	}
}
