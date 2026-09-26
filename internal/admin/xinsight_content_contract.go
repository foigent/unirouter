package admin

import (
	"bytes"
	"encoding/json"
	"regexp"
	"slices"
	"strconv"

	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/sourcefacts"
)

// This decoder grants no authority and performs no database operations.
// C requests are separate from the existing legacy source control DTO.
type contentControlRequest struct {
	OperationID          string                    `json:"operation_id"`
	ExpectedStateVersion string                    `json:"expected_state_version"`
	CredentialID         string                    `json:"credential_id,omitempty"`
	RegistrationHash     string                    `json:"registration_hash,omitempty"`
	Registration         *sourcefacts.Registration `json:"registration,omitempty"`
	AllowedIP            string                    `json:"allowed_ip,omitempty"`
}

var contentOperationPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
var contentHashPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func contentControlNumber(raw string, zero bool) (int64, bool) {
	n, err := strconv.ParseInt(raw, 10, 64)
	return n, err == nil && n >= 0 && (zero || n > 0) && strconv.FormatInt(n, 10) == raw
}

func decodeContentControl(wire []byte, action string) (contentControlRequest, error) {
	var result contentControlRequest
	invalid := errXInsightFeedInvalidScope
	if !slices.Contains([]string{"prepare", "rotate", "activate", "cancel", "pause", "resume", "revoke"}, action) {
		return result, invalid
	}
	if err := eventconnector.ValidateStrictJSON(wire, eventconnector.StrictJSONOptions{RejectNull: true}); err != nil {
		return result, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(wire, &fields); err != nil || fields == nil {
		return result, invalid
	}
	for name := range fields {
		if !slices.Contains([]string{"operation_id", "expected_state_version", "credential_id", "registration_hash", "registration", "allowed_ip"}, name) {
			return result, invalid
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(wire))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return result, err
	}
	if !contentOperationPattern.MatchString(result.OperationID) {
		return result, invalid
	}
	if _, ok := contentControlNumber(result.ExpectedStateVersion, true); !ok {
		return result, invalid
	}
	register := action == "prepare" || action == "rotate"
	if register {
		if result.Registration == nil || sourcefacts.ValidateContentRegistration(*result.Registration) != nil || result.OperationID != result.Registration.OperationID {
			return result, invalid
		}
		registrationOnly, _ := json.Marshal(map[string]json.RawMessage{"registration": fields["registration"]})
		if !exactSourceControlFields(registrationOnly) {
			return result, invalid
		}
	} else if _, exists := fields["registration"]; exists {
		return result, invalid
	}
	if action == "prepare" {
		if !exactHostNetworkBinding(result.AllowedIP) {
			return result, invalid
		}
		if _, exists := fields["credential_id"]; exists {
			return result, invalid
		}
		if _, exists := fields["registration_hash"]; exists {
			return result, invalid
		}
	} else {
		if _, exists := fields["allowed_ip"]; exists {
			return result, invalid
		}
		_, hasID := fields["credential_id"]
		_, hasHash := fields["registration_hash"]
		if hasID != hasHash {
			return result, invalid
		}
		if hasID {
			if _, ok := contentControlNumber(result.CredentialID, false); !ok || !contentHashPattern.MatchString(result.RegistrationHash) {
				return result, invalid
			}
		}
		if (action == "rotate" || action == "activate" || action == "cancel") && !hasID {
			return result, invalid
		}
	}
	return result, nil
}
