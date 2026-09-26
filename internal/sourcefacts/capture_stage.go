package sourcefacts

import "errors"

// captureStageError preserves the original cause for errors.Is/As. Only finite
// execution stages are exported; request/source data and SQL are never labels.
type captureStageError struct {
	stage string
	cause error
}

func (e *captureStageError) Error() string {
	return "source capture " + e.stage + ": " + e.cause.Error()
}
func (e *captureStageError) Unwrap() error { return e.cause }
func captureError(stage string, err error) error {
	if err == nil {
		return nil
	}
	return &captureStageError{stage: stage, cause: err}
}

// CaptureErrorStage returns a bounded diagnostic label, not a root-cause claim.
func CaptureErrorStage(err error) string {
	var staged *captureStageError
	if !errors.As(err, &staged) {
		return "unknown"
	}
	switch staged.stage {
	case "begin", "transaction_setup", "admission_lookup", "attempt_lookup", "destination_lookup", "commit",
		"stream_insert", "stream_lock", "replay_lookup", "publication_clock", "replay_predecessor_read", "sequence_and_fact_write", "batch_close":
		return staged.stage
	default:
		return "unknown"
	}
}
