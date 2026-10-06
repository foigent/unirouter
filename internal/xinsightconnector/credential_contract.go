package xinsightconnector

import (
	"bytes"
	"encoding/json"
	"regexp"
	"time"
)

const (
	CredentialRequestSchema  = "xinsight.connector-control-credential-request.v1"
	CredentialReceiptSchema  = "xinsight.connector-control-credential-receipt.v1"
	CredentialRecoverySchema = "xinsight.connector-control-recovery.v1"
)

var credentialOperationPattern = regexp.MustCompile(`^xcr_[A-Za-z0-9_-]{22}$`)
var credentialRecoveryPattern = regexp.MustCompile(`^xrr_[A-Za-z0-9_-]{22}$`)
var credentialTicketPattern = regexp.MustCompile(`^xirt_[A-Za-z0-9_-]{43}$`)
var credentialIdentityPattern = regexp.MustCompile(`^xsi_[A-Za-z0-9_-]{22}$`)

func IsCredentialRecoveryTicket(ticket string) bool {
	return credentialTicketPattern.MatchString(ticket)
}

// Field order is the shared v1 wire contract. Recovery-only fields are
// pointers so its two empty supersession fields remain present on the wire.
type CredentialRequest struct {
	SchemaVersion               string   `json:"schema_version"`
	Kind                        string   `json:"kind"`
	InstallationID              string   `json:"installation_id"`
	DeploymentID                string   `json:"deployment_id"`
	ProjectID                   string   `json:"project_id"`
	FinalEventScope             []string `json:"final_event_scope"`
	SourceBindingSHA256         string   `json:"source_binding_sha256"`
	XInsightOrigin              string   `json:"xinsight_origin"`
	UniRouterOrigin             string   `json:"unirouter_origin"`
	OperationID                 string   `json:"operation_id"`
	PreviousTokenSHA256         string   `json:"previous_token_sha256"`
	ReplacementToken            string   `json:"replacement_token"`
	RecoveryID                  *string  `json:"recovery_id,omitempty"`
	RecoveryTicket              *string  `json:"recovery_ticket,omitempty"`
	PreviousToken               *string  `json:"previous_token,omitempty"`
	SupersededOperationID       *string  `json:"superseded_operation_id,omitempty"`
	SupersededReplacementSHA256 *string  `json:"superseded_replacement_token_sha256,omitempty"`
}

type CredentialConfirm struct {
	SchemaVersion string `json:"schema_version"`
	OperationID   string `json:"operation_id"`
	RequestSHA256 string `json:"request_sha256"`
}

type CredentialReceipt struct {
	SchemaVersion          string   `json:"schema_version"`
	InstallationID         string   `json:"installation_id"`
	DeploymentID           string   `json:"deployment_id"`
	ProjectID              string   `json:"project_id"`
	FinalEventScope        []string `json:"final_event_scope"`
	SourceBindingSHA256    string   `json:"source_binding_sha256"`
	XInsightOrigin         string   `json:"xinsight_origin"`
	UniRouterOrigin        string   `json:"unirouter_origin"`
	OperationID            string   `json:"operation_id"`
	RequestSHA256          string   `json:"request_sha256"`
	ControlIdentityID      string   `json:"control_identity_id"`
	PreviousTokenSHA256    string   `json:"previous_token_sha256"`
	ReplacementTokenSHA256 string   `json:"replacement_token_sha256"`
	ReplacementExpiresAt   string   `json:"replacement_expires_at"`
	State                  string   `json:"state"`
}

// CredentialRecoveryResolution is XI's non-secret read of a human-approved
// ticket. The ticket itself is supplied separately and never echoed here.
type CredentialRecoveryResolution struct {
	SchemaVersion               string   `json:"schema_version"`
	InstallationID              string   `json:"installation_id"`
	DeploymentID                string   `json:"deployment_id"`
	ProjectID                   string   `json:"project_id"`
	FinalEventScope             []string `json:"final_event_scope"`
	SourceBindingSHA256         string   `json:"source_binding_sha256"`
	XInsightOrigin              string   `json:"xinsight_origin"`
	UniRouterOrigin             string   `json:"unirouter_origin"`
	RecoveryID                  string   `json:"recovery_id"`
	PreviousTokenSHA256         string   `json:"previous_token_sha256"`
	SupersededOperationID       string   `json:"superseded_operation_id"`
	SupersededReplacementSHA256 string   `json:"superseded_replacement_token_sha256"`
	ExpiresAt                   string   `json:"expires_at"`
}

func (r CredentialRecoveryResolution) Validate(now time.Time) error {
	if r.SchemaVersion != CredentialRecoverySchema || !validExactExternalIdentity(r.InstallationID) ||
		!validExactDeploymentIdentity(r.DeploymentID) || !validControlText(r.ProjectID, 128) ||
		len(r.FinalEventScope) != 1 || r.FinalEventScope[0] != EventScopeModelCompleted ||
		!hashPattern.MatchString(r.SourceBindingSHA256) || !validFrozenHTTPSOrigin(r.XInsightOrigin) ||
		!validFrozenHTTPSOrigin(r.UniRouterOrigin) || !credentialRecoveryPattern.MatchString(r.RecoveryID) ||
		!hashPattern.MatchString(r.PreviousTokenSHA256) ||
		(r.SupersededOperationID == "") != (r.SupersededReplacementSHA256 == "") {
		return ErrInvalid
	}
	if r.SupersededOperationID != "" && (!credentialOperationPattern.MatchString(r.SupersededOperationID) ||
		!hashPattern.MatchString(r.SupersededReplacementSHA256)) {
		return ErrInvalid
	}
	expires, err := time.Parse(time.RFC3339Nano, r.ExpiresAt)
	if err != nil || expires.Nanosecond()%1000 != 0 || expires.UTC().Format(time.RFC3339Nano) != r.ExpiresAt || !expires.After(now.UTC()) {
		return ErrConflict
	}
	return nil
}

func (r CredentialRequest) CanonicalJSON() ([]byte, error) {
	if r.SchemaVersion != CredentialRequestSchema || (r.Kind != "rotate" && r.Kind != "recover") ||
		!validExactExternalIdentity(r.InstallationID) || !validExactDeploymentIdentity(r.DeploymentID) ||
		!validControlText(r.ProjectID, 128) || len(r.FinalEventScope) != 1 || r.FinalEventScope[0] != EventScopeModelCompleted ||
		!hashPattern.MatchString(r.SourceBindingSHA256) || !validFrozenHTTPSOrigin(r.XInsightOrigin) ||
		!validFrozenHTTPSOrigin(r.UniRouterOrigin) || !credentialOperationPattern.MatchString(r.OperationID) ||
		!hashPattern.MatchString(r.PreviousTokenSHA256) || !validConnectorControlBearer(r.ReplacementToken) {
		return nil, ErrInvalid
	}
	if r.Kind == "rotate" {
		if r.RecoveryID != nil || r.RecoveryTicket != nil || r.PreviousToken != nil ||
			r.SupersededOperationID != nil || r.SupersededReplacementSHA256 != nil {
			return nil, ErrInvalid
		}
	} else {
		if r.RecoveryID == nil || !credentialRecoveryPattern.MatchString(*r.RecoveryID) ||
			r.RecoveryTicket == nil || !credentialTicketPattern.MatchString(*r.RecoveryTicket) ||
			r.PreviousToken == nil || !validConnectorControlBearer(*r.PreviousToken) ||
			hashBytes([]byte(*r.PreviousToken)) != r.PreviousTokenSHA256 ||
			r.SupersededOperationID == nil || r.SupersededReplacementSHA256 == nil ||
			(*r.SupersededOperationID == "") != (*r.SupersededReplacementSHA256 == "") {
			return nil, ErrInvalid
		}
		if *r.SupersededOperationID != "" && (!credentialOperationPattern.MatchString(*r.SupersededOperationID) ||
			!hashPattern.MatchString(*r.SupersededReplacementSHA256)) {
			return nil, ErrInvalid
		}
	}
	return json.Marshal(r)
}

func DecodeCredentialRequest(raw []byte) (CredentialRequest, error) {
	var r CredentialRequest
	if len(raw) == 0 || len(raw) > 8192 || rejectDuplicateControlV2Fields(raw) != nil || decodeStrictControlV2(raw, &r) != nil {
		return r, ErrInvalid
	}
	canonical, err := r.CanonicalJSON()
	if err != nil || !bytes.Equal(canonical, raw) {
		return CredentialRequest{}, ErrInvalid
	}
	return r, nil
}

func (c CredentialConfirm) CanonicalJSON() ([]byte, error) {
	if c.SchemaVersion != CredentialRequestSchema || !credentialOperationPattern.MatchString(c.OperationID) || !hashPattern.MatchString(c.RequestSHA256) {
		return nil, ErrInvalid
	}
	return json.Marshal(c)
}

func (r CredentialReceipt) CanonicalJSON() ([]byte, error) {
	if r.SchemaVersion != CredentialReceiptSchema || !validExactExternalIdentity(r.InstallationID) || !validExactDeploymentIdentity(r.DeploymentID) ||
		!validControlText(r.ProjectID, 128) || len(r.FinalEventScope) != 1 || r.FinalEventScope[0] != EventScopeModelCompleted ||
		!hashPattern.MatchString(r.SourceBindingSHA256) || !validFrozenHTTPSOrigin(r.XInsightOrigin) || !validFrozenHTTPSOrigin(r.UniRouterOrigin) ||
		!credentialOperationPattern.MatchString(r.OperationID) || !hashPattern.MatchString(r.RequestSHA256) ||
		!credentialIdentityPattern.MatchString(r.ControlIdentityID) || !hashPattern.MatchString(r.PreviousTokenSHA256) ||
		!hashPattern.MatchString(r.ReplacementTokenSHA256) || (r.State != "prepared" && r.State != "committed") {
		return nil, ErrInvalid
	}
	expires, err := time.Parse(time.RFC3339Nano, r.ReplacementExpiresAt)
	if err != nil || expires.Nanosecond()%1000 != 0 || expires.UTC().Format(time.RFC3339Nano) != r.ReplacementExpiresAt {
		return nil, ErrInvalid
	}
	return json.Marshal(r)
}

func DecodeCredentialReceipt(raw []byte) (CredentialReceipt, error) {
	var r CredentialReceipt
	if len(raw) == 0 || len(raw) > 8192 || rejectDuplicateControlV2Fields(raw) != nil || decodeStrictControlV2(raw, &r) != nil {
		return r, ErrInvalid
	}
	canonical, err := r.CanonicalJSON()
	if err != nil {
		return CredentialReceipt{}, ErrInvalid
	}
	// XI uses json.Encoder, so a framing newline is allowed, but the decoded
	// strict DTO must round-trip to the shared canonical field order.
	if !bytes.Equal(bytes.TrimSuffix(raw, []byte("\n")), canonical) {
		return CredentialReceipt{}, ErrInvalid
	}
	return r, nil
}
