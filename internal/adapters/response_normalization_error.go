package adapters

import "errors"

// responseNormalizationError preserves the original error for existing callers.
// Only ResponseNormalizationStage may be used in persisted safe diagnostics.
type responseNormalizationError struct {
	stage string
	cause error
}

func (e *responseNormalizationError) Error() string { return e.cause.Error() }
func (e *responseNormalizationError) Unwrap() error { return e.cause }

func normalizationError(stage string, cause error) error {
	return &responseNormalizationError{stage: stage, cause: cause}
}

// ResponseNormalizationStage returns a closed code, never provider text or a
// field name taken from an untrusted document. It grants no usage authority.
func ResponseNormalizationStage(err error) string {
	var failure *responseNormalizationError
	if errors.As(err, &failure) {
		switch failure.stage {
		case "identity", "json", "usage", "tool_normalization", "builtin_metering", "provider_envelope", "cache_projection":
			return failure.stage
		}
	}
	return "unknown"
}
