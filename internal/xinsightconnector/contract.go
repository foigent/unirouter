package xinsightconnector

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	SourceBindingSchemaVersion = "unirouter.xinsight.connector-source-scope.v1"
	InstallationSchemaVersion  = "unirouter.xinsight.connector-installation.v1"
	OperationSchemaVersion     = "unirouter.xinsight.connector-operation.v1"
	EventScopeModelCompleted   = "model.request.completed"
	CompletedFeedSchemaVersion = "xinsight.completed-execution-feed.v1"

	DesiredActive       = "active"
	DesiredPaused       = "paused"
	DesiredDisconnected = "disconnected"

	ObservedProvisioning = "provisioning"
	ObservedConnected    = "connected"
	ObservedPaused       = "paused"
	ObservedAbnormal     = "abnormal"
	ObservedDisconnected = "disconnected"

	ProductNotConnected = "not_connected"
	ProductConnecting   = "connecting"
	ProductConnected    = "connected"
	ProductPaused       = "paused"
	ProductAbnormal     = "abnormal"

	ActionConnect = "connect"
	ActionPause   = "pause"
	ActionResume  = "resume"
	ActionRotate  = "rotate"
	ActionReplay  = "replay"

	OperationQueued    = "queued"
	OperationClaimed   = "claimed"
	OperationRetryWait = "retry_wait"
	OperationSucceeded = "succeeded"
	OperationFailed    = "failed"
	OperationCancelled = "cancelled"

	HealthBacklogCap = int64(10_000)
	MaxAttempts      = 12
)

var (
	ErrInvalid      = errors.New("x-insight connector input is invalid")
	ErrNotFound     = errors.New("x-insight connector installation or operation was not found")
	ErrConflict     = errors.New("x-insight connector state conflicts with the requested operation")
	ErrUnauthorized = errors.New("x-insight connector authority changed")
	ErrUnavailable  = errors.New("x-insight connector state is unavailable")

	localIdentityPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._:-]{0,127}$`)
	remoteInstallationID = regexp.MustCompile(`^xci_[A-Za-z0-9_-]{22}$`)
	remoteDeploymentID   = regexp.MustCompile(`^xid_[A-Za-z0-9_-]{22}$`)
	remoteOperationID    = regexp.MustCompile(`^xco_[A-Za-z0-9_-]{22}$`)
	hashPattern          = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	tokenPattern         = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
)

type SourceBinding struct {
	SchemaVersion  string `json:"schema_version"`
	InstallationID string `json:"installation_id"`
	XDeploymentID  string `json:"x_deployment_id"`
	OrgID          int64  `json:"org_id"`
	ProjectID      int64  `json:"project_id"`
	SourceInstance string `json:"source_instance"`
	EventScope     string `json:"event_scope"`
	FeedSchema     string `json:"feed_schema"`
}

type Consent struct {
	InstallationID    string
	XDeploymentID     string
	InvitationSHA256  string
	OrgID             int64
	ProjectID         int64
	EventScope        string
	IdempotencyKey    string
	ConsentedByUserID int64
}

type Installation struct {
	SchemaVersion          string     `json:"schema_version"`
	InstallationID         string     `json:"installation_id"`
	XDeploymentID          string     `json:"x_deployment_id"`
	InvitationSHA256       string     `json:"invitation_sha256"`
	OrgID                  int64      `json:"org_id"`
	ProjectID              int64      `json:"project_id"`
	EventScope             string     `json:"event_scope"`
	SourceInstance         string     `json:"source_instance"`
	SourceBindingSHA256    string     `json:"source_binding_sha256"`
	DesiredState           string     `json:"desired_state"`
	ObservedState          string     `json:"observed_state"`
	StateRevision          int64      `json:"state_revision"`
	ControlFence           int64      `json:"control_fence"`
	BootstrapReceiptSHA256 string     `json:"bootstrap_receipt_sha256,omitempty"`
	LastSuccessfulSyncAt   *time.Time `json:"last_successful_sync_at,omitempty"`
	LastErrorCode          string     `json:"last_error_code,omitempty"`
	LastErrorAt            *time.Time `json:"last_error_at,omitempty"`
	ConsentedAt            time.Time  `json:"consented_at"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

type Operation struct {
	SchemaVersion     string     `json:"schema_version"`
	OperationRef      string     `json:"operation_ref"`
	InstallationID    string     `json:"installation_id"`
	OrgID             int64      `json:"org_id"`
	ProjectID         int64      `json:"project_id"`
	Action            string     `json:"action"`
	IdempotencyKey    string     `json:"idempotency_key"`
	DesiredState      string     `json:"desired_state"`
	State             string     `json:"state"`
	RequestedRevision int64      `json:"requested_revision"`
	ControlFence      int64      `json:"control_fence"`
	AttemptCount      int        `json:"attempt_count"`
	NextAttemptAt     time.Time  `json:"next_attempt_at"`
	LeaseOwner        string     `json:"lease_owner,omitempty"`
	LeaseToken        string     `json:"lease_token,omitempty"`
	LeaseExpiresAt    *time.Time `json:"lease_expires_at,omitempty"`
	ResultCode        string     `json:"result_code,omitempty"`
	ResultSHA256      string     `json:"result_sha256,omitempty"`
	ErrorCode         string     `json:"error_code,omitempty"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type ProjectHealth struct {
	ProductStatus           string     `json:"product_status"`
	FeedConnectionStatus    string     `json:"feed_connection_status"`
	LastAuthenticatedPullAt *time.Time `json:"last_authenticated_pull_at,omitempty"`
	LastSuccessfulSyncAt    *time.Time `json:"last_successful_sync_at,omitempty"`
	OldestPendingAt         *time.Time `json:"oldest_pending_at,omitempty"`
	OldestPendingAgeSeconds int64      `json:"oldest_pending_age_seconds"`
	PendingItemCount        int64      `json:"pending_item_count"`
	PendingItemCountCapped  bool       `json:"pending_item_count_capped"`
	CursorOutsideRetention  bool       `json:"cursor_outside_retention"`
	ActiveCredentialCount   int        `json:"active_credential_count"`
	StagedCredentialCount   int        `json:"staged_credential_count"`
}

type ProjectState struct {
	Installation         *Installation `json:"installation,omitempty"`
	Health               ProjectHealth `json:"health"`
	LatestOperation      *Operation    `json:"latest_operation,omitempty"`
	AcknowledgedSequence int64         `json:"-"`
	HighWatermark        int64         `json:"-"`
	RetainedAfter        int64         `json:"-"`
}

type OperationRequest struct {
	InstallationID    string
	OrgID             int64
	ProjectID         int64
	Action            string
	IdempotencyKey    string
	RequestedByUserID int64
}

type OperationOutcome struct {
	ObservedState          string
	ResultCode             string
	ResultSHA256           string
	BootstrapReceiptSHA256 string
	SuccessfulSyncAt       *time.Time
}

func normalizeConsent(input Consent, sourceInstance string) (Consent, SourceBinding, string, error) {
	input.InvitationSHA256 = strings.TrimSpace(strings.ToLower(input.InvitationSHA256))
	input.EventScope = strings.TrimSpace(input.EventScope)
	input.IdempotencyKey = strings.TrimSpace(strings.ToLower(input.IdempotencyKey))
	if !validExactExternalIdentity(input.InstallationID) ||
		!validExactDeploymentIdentity(input.XDeploymentID) ||
		!hashPattern.MatchString(input.InvitationSHA256) ||
		input.OrgID <= 0 || input.ProjectID <= 0 || input.ConsentedByUserID <= 0 ||
		input.EventScope != EventScopeModelCompleted ||
		!validSourceInstance(sourceInstance) ||
		!localIdentityPattern.MatchString(input.IdempotencyKey) {
		return Consent{}, SourceBinding{}, "", ErrInvalid
	}
	binding := SourceBinding{
		SchemaVersion:  SourceBindingSchemaVersion,
		InstallationID: input.InstallationID,
		XDeploymentID:  input.XDeploymentID,
		OrgID:          input.OrgID,
		ProjectID:      input.ProjectID,
		SourceInstance: sourceInstance,
		EventScope:     input.EventScope,
		FeedSchema:     CompletedFeedSchemaVersion,
	}
	body, err := json.Marshal(binding)
	if err != nil {
		return Consent{}, SourceBinding{}, "", fmt.Errorf("%w: source binding", ErrUnavailable)
	}
	return input, binding, hashBytes(body), nil
}

func validSourceInstance(value string) bool {
	if value == "" || len(value) > 128 || !utf8.ValidString(value) {
		return false
	}
	for _, char := range value {
		if unicode.IsControl(char) {
			return false
		}
	}
	return true
}

func validExactExternalIdentity(value string) bool {
	return remoteInstallationID.MatchString(value)
}

func validExactDeploymentIdentity(value string) bool {
	return remoteDeploymentID.MatchString(value)
}

func validateOperationRequest(input OperationRequest) (OperationRequest, string, error) {
	input.Action = strings.TrimSpace(strings.ToLower(input.Action))
	input.IdempotencyKey = strings.TrimSpace(strings.ToLower(input.IdempotencyKey))
	if !validExactExternalIdentity(input.InstallationID) || input.OrgID <= 0 || input.ProjectID <= 0 ||
		input.RequestedByUserID <= 0 || !localIdentityPattern.MatchString(input.IdempotencyKey) {
		return OperationRequest{}, "", ErrInvalid
	}
	var desired string
	switch input.Action {
	case ActionPause:
		desired = DesiredPaused
	case ActionResume:
		desired = DesiredActive
	case ActionRotate, ActionReplay:
		// These actions preserve the installation's desired state. The manager
		// replaces this marker after locking the installation.
		desired = ""
	default:
		return OperationRequest{}, "", ErrInvalid
	}
	return input, desired, nil
}

func validateOutcome(action string, outcome OperationOutcome) (OperationOutcome, error) {
	outcome.ObservedState = strings.TrimSpace(strings.ToLower(outcome.ObservedState))
	outcome.ResultCode = strings.TrimSpace(strings.ToLower(outcome.ResultCode))
	outcome.ResultSHA256 = strings.TrimSpace(strings.ToLower(outcome.ResultSHA256))
	outcome.BootstrapReceiptSHA256 = strings.TrimSpace(strings.ToLower(outcome.BootstrapReceiptSHA256))
	allowed := false
	switch action {
	case ActionConnect:
		allowed = outcome.ObservedState == ObservedConnected || outcome.ObservedState == ObservedPaused
	case ActionPause:
		allowed = outcome.ObservedState == ObservedPaused
	case ActionResume:
		allowed = outcome.ObservedState == ObservedConnected
	case ActionRotate, ActionReplay:
		allowed = outcome.ObservedState == ObservedConnected || outcome.ObservedState == ObservedPaused
	}
	if !allowed || !tokenPattern.MatchString(outcome.ResultCode) ||
		(outcome.ResultSHA256 != "" && !hashPattern.MatchString(outcome.ResultSHA256)) ||
		(outcome.BootstrapReceiptSHA256 != "" && !hashPattern.MatchString(outcome.BootstrapReceiptSHA256)) {
		return OperationOutcome{}, ErrInvalid
	}
	if outcome.SuccessfulSyncAt != nil {
		value := outcome.SuccessfulSyncAt.UTC().Truncate(time.Microsecond)
		outcome.SuccessfulSyncAt = &value
	}
	return outcome, nil
}

func validMachineToken(value string) bool {
	return tokenPattern.MatchString(strings.TrimSpace(strings.ToLower(value)))
}

func operationRef(installationID, idempotencyKey string) string {
	sum := sha256.Sum256([]byte("unirouter.xinsight.connector-operation.v1\x00" + installationID + "\x00" + idempotencyKey))
	return "xcop_" + hex.EncodeToString(sum[:16])
}

func hashBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(sum[:])
}
