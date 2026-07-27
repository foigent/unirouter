package eventconnector

import (
	"errors"
	"fmt"
	"regexp"
	"unicode"
	"unicode/utf8"
)

const ConnectorErrorVersion = "foigent.connector-error.v1"

var connectorErrorCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]*$`)

var connectorErrorCategories = map[string]struct{}{
	"configuration":  {},
	"authentication": {},
	"authorization":  {},
	"transport":      {},
	"source":         {},
	"contract":       {},
	"privacy":        {},
	"cursor":         {},
	"conflict":       {},
	"persistence":    {},
	"internal":       {},
}

var connectorErrorPhases = map[string]struct{}{
	"configure":  {},
	"activate":   {},
	"fetch":      {},
	"decode":     {},
	"validate":   {},
	"normalize":  {},
	"commit":     {},
	"checkpoint": {},
	"status":     {},
	"shutdown":   {},
}

var connectorErrorRecoveries = map[string]struct{}{
	"automatic_retry": {},
	"restart":         {},
	"reauthorize":     {},
	"operator_repair": {},
	"explicit_resync": {},
	"none":            {},
}

// ConnectorError is the product-private representation of the Binding
// foigent.connector-error.v1 contract. Cause is deliberately local-only.
type ConnectorError struct {
	ErrorVersion      string `json:"error_version"`
	Category          string `json:"category"`
	Code              string `json:"code"`
	Phase             string `json:"phase"`
	Operation         string `json:"operation"`
	Retryable         bool   `json:"retryable"`
	RetryAfterSeconds *int   `json:"retry_after_seconds,omitempty"`
	SafeMessage       string `json:"safe_message"`
	Recovery          string `json:"recovery"`

	cause error
}

func NewConnectorError(
	category string,
	code string,
	phase string,
	operation string,
	retryable bool,
	safeMessage string,
	recovery string,
	cause error,
) *ConnectorError {
	return &ConnectorError{
		ErrorVersion: ConnectorErrorVersion,
		Category:     category,
		Code:         code,
		Phase:        phase,
		Operation:    operation,
		Retryable:    retryable,
		SafeMessage:  safeMessage,
		Recovery:     recovery,
		cause:        cause,
	}
}

func (e *ConnectorError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.SafeMessage == "" {
		return e.Code
	}
	return e.Code + ": " + e.SafeMessage
}

func (e *ConnectorError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func (e *ConnectorError) WithRetryAfter(seconds int) *ConnectorError {
	if e == nil {
		return nil
	}
	cloned := *e
	cloned.RetryAfterSeconds = &seconds
	return &cloned
}

func (e *ConnectorError) Validate() error {
	if e == nil {
		return errors.New("Connector Error is nil")
	}
	if e.ErrorVersion != ConnectorErrorVersion {
		return fmt.Errorf("error_version must be %q", ConnectorErrorVersion)
	}
	if _, ok := connectorErrorCategories[e.Category]; !ok {
		return errors.New("Connector Error category is unsupported")
	}
	if !connectorErrorCodePattern.MatchString(e.Code) || len(e.Code) > 160 {
		return errors.New("Connector Error code is invalid")
	}
	if _, ok := connectorErrorPhases[e.Phase]; !ok {
		return errors.New("Connector Error phase is unsupported")
	}
	if !connectorErrorCodePattern.MatchString(e.Operation) || len(e.Operation) > 128 {
		return errors.New("Connector Error operation is invalid")
	}
	if !utf8.ValidString(e.SafeMessage) || len(e.SafeMessage) < 1 || len(e.SafeMessage) > 512 {
		return errors.New("Connector Error safe_message is invalid")
	}
	for _, char := range e.SafeMessage {
		if unicode.IsControl(char) {
			return errors.New("Connector Error safe_message contains a control character")
		}
	}
	if _, ok := connectorErrorRecoveries[e.Recovery]; !ok {
		return errors.New("Connector Error recovery is unsupported")
	}
	if e.RetryAfterSeconds != nil {
		if !e.Retryable {
			return errors.New("non-retryable Connector Error must not contain retry_after_seconds")
		}
		if *e.RetryAfterSeconds < 0 || *e.RetryAfterSeconds > 300 {
			return errors.New("Connector Error retry_after_seconds is out of bounds")
		}
	}
	return nil
}

func contractError(code, phase, operation, message string, cause error) *ConnectorError {
	return NewConnectorError(
		"contract",
		code,
		phase,
		operation,
		false,
		message,
		"operator_repair",
		cause,
	)
}
