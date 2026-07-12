package admin

import "testing"

func TestCreateRPSLimitCompatibility(t *testing.T) {
	rps := 12
	got, err := createRPSLimit(&rps, 12)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 12 {
		t.Fatalf("expected rps 12, got %d", got)
	}

	got, err = createRPSLimit(nil, 0)
	if err != nil {
		t.Fatalf("unexpected default error: %v", err)
	}
	if got != 10 {
		t.Fatalf("expected default rps 10, got %d", got)
	}

	legacy := 10
	if _, err := createRPSLimit(&legacy, 12); err == nil {
		t.Fatal("expected conflict between rps_limit and qps_limit")
	}
}

func TestUpdateRPSLimitCompatibility(t *testing.T) {
	limit := 20
	got, provided, err := updateRPSLimit(&limit, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !provided || got != 20 {
		t.Fatalf("expected provided rps 20, got provided=%v value=%d", provided, got)
	}

	zero := 0
	if _, _, err := updateRPSLimit(&zero, nil); err == nil {
		t.Fatal("expected zero rps_limit to be rejected")
	}
}

func TestAPIKeyQuotaPayload(t *testing.T) {
	payload := apiKeyQuotaPayload(60, 10, 0)
	if payload["rpm_limit"] != 60 || payload["rps_limit"] != 10 || payload["qps_compat_limit"] != 10 {
		t.Fatalf("unexpected request quota payload: %#v", payload)
	}
	if payload["tpm_limit"] != nil || payload["tokens_per_second"] != nil {
		t.Fatalf("expected token limits to be explicitly unavailable, got %#v", payload)
	}
	if payload["tpm_source"] != "not_configured" {
		t.Fatalf("expected not_configured TPM source, got %#v", payload["tpm_source"])
	}
}

func TestNormalizePolicyLabelsMatchesRuntimeAndDatabaseContract(t *testing.T) {
	value, errMessage := normalizePolicyLabels([]string{" production ", "customer-facing", "production"})
	if errMessage != "" || value != "customer-facing,production" {
		t.Fatalf("normalized labels = %q, error = %q", value, errMessage)
	}
	if _, errMessage := normalizePolicyLabels([]string{"生产"}); errMessage == "" {
		t.Fatal("non-ASCII policy label must be rejected before snapshot refresh")
	}
	if _, errMessage := normalizePolicyLabels([]string{"_internal"}); errMessage == "" {
		t.Fatal("policy label must start with an ASCII letter or number")
	}
}

func TestLimitPolicyLimitsRespectDatabaseColumnWidths(t *testing.T) {
	tooLargeRPS := int64(2_147_483_648)
	validTPM := int64(9_000_000_000_000_000)
	if validLimitPolicyLimits(&limitPolicyRecord{RPSLimit: &tooLargeRPS}) {
		t.Fatal("RPS above PostgreSQL INTEGER range must be rejected by the API")
	}
	if !validLimitPolicyLimits(&limitPolicyRecord{TPMLimit: &validTPM}) {
		t.Fatal("valid BIGINT-backed TPM limit was rejected")
	}
}
