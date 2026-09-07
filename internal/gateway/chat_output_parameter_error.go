package gateway

import (
	"errors"
	"fmt"
)

// Only the existing exact protocol validator constructs this rejection. The
// internal diagnostic remains private; public projection accepts finite fields.
type unsupportedOutputParameterError struct {
	parameter string
	accepted  []string
	profile   string
}

func (e *unsupportedOutputParameterError) Error() string {
	return fmt.Sprintf("%s is not supported by protocol profile %s", e.parameter, e.profile)
}

type chatOutputParameterError struct {
	Code               string   `json:"code"`
	Parameter          string   `json:"parameter"`
	AcceptedParameters []string `json:"accepted_parameters"`
	Message            string   `json:"message"`
}

func publicOutputParameterError(err error) *chatOutputParameterError {
	if !errors.Is(err, errModelExecutionProfileRequestInvalid) {
		return nil
	}
	var cause *unsupportedOutputParameterError
	if !errors.As(err, &cause) || cause == nil || len(cause.accepted) != 1 {
		return nil
	}
	accepted := cause.accepted[0]
	if !((cause.parameter == "max_tokens" && accepted == "max_completion_tokens") ||
		(cause.parameter == "max_completion_tokens" && accepted == "max_tokens")) {
		return nil
	}
	return &chatOutputParameterError{
		Code: "unsupported_output_parameter", Parameter: cause.parameter,
		AcceptedParameters: []string{accepted},
		Message:            fmt.Sprintf("Remove %s and use %s for this model; do not send both. Check the selected model's output limit before migrating the value.", cause.parameter, accepted),
	}
}
