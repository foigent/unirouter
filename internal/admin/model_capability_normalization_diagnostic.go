package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/uniroute/unirouter/internal/adapters"
)

// This describes observed transport bytes, never accepted usage or qualification.
// Full=false means the hash covers only the captured prefix, including an empty
// prefix when the reader failed before returning any bytes.
type modelCapabilityNormalizationDiagnostic struct {
	Stage      string `json:"stage"`
	Bytes      int    `json:"bytes"`
	Full       bool   `json:"full"`
	BodySHA256 string `json:"body_sha256"`
}

type modelCapabilityNormalizationError struct {
	diagnostic modelCapabilityNormalizationDiagnostic
	cause      error
}

func (e *modelCapabilityNormalizationError) Error() string { return e.cause.Error() }
func (e *modelCapabilityNormalizationError) Unwrap() error { return e.cause }

func capabilityNormalizationError(stage string, body []byte, full bool, cause error) error {
	digest := sha256.Sum256(body)
	return &modelCapabilityNormalizationError{
		diagnostic: modelCapabilityNormalizationDiagnostic{
			Stage: stage, Bytes: len(body), Full: full, BodySHA256: hex.EncodeToString(digest[:]),
		},
		cause: cause,
	}
}

func capabilityReadFailureStage(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "read_canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "read_deadline"
	default:
		return "read_error"
	}
}

func capabilityAdapterFailureStage(err error) string {
	return "adapter_" + adapters.ResponseNormalizationStage(err)
}

func applyCapabilityNormalizationDiagnostic(outcome *modelCapabilityProbeOutcome, err error) {
	var failure *modelCapabilityNormalizationError
	if !errors.As(err, &failure) {
		return
	}
	diagnostic := failure.diagnostic
	switch diagnostic.Stage {
	case "read_canceled", "read_deadline", "read_error", "response_too_large", "duplicate_json", "invalid_json", "profile_unavailable",
		"adapter_identity", "adapter_json", "adapter_usage", "adapter_tool_normalization", "adapter_builtin_metering", "adapter_provider_envelope", "adapter_cache_projection", "adapter_unknown":
		outcome.NormalizationDiagnostic = &diagnostic
		outcome.ResponseBytes = diagnostic.Bytes
	}
}
