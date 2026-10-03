package xinsightconnector

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

const ReceiverModeNewInstall = "new_install"
const ReceiverModeAdoptExisting = "adopt_existing"
const RedeemAdoptionRequestSchema = "xinsight.connector-invitation-redeem.v2"
const RedeemAdoptionResultSchema = "xinsight.connector-invitation-redeem-result.v2"

type ReceiverAdoptionBootstrap struct {
	InstallationID          string `json:"installation_id"`
	OrgID                   int64  `json:"org_id"`
	ProjectID               int64  `json:"project_id"`
	SourceInstance          string `json:"source_instance"`
	SourceBindingSHA256     string `json:"source_binding_sha256"`
	FeedKeyID               string `json:"feed_key_id"`
	AllowedIP               string `json:"allowed_ip"`
	KeyPrefix               string `json:"key_prefix"`
	RegistrationOperationID string `json:"registration_operation_id"`
	VerifierScheme          string `json:"verifier_scheme"`
}
type redeemAdoptionRequest struct {
	SchemaVersion                   string                    `json:"schema_version"`
	InvitationToken                 string                    `json:"invitation_token"`
	ProjectID                       string                    `json:"project_id"`
	FinalEventScope                 []string                  `json:"final_event_scope"`
	ConsentSHA256                   string                    `json:"consent_sha256"`
	ConfirmedAt                     string                    `json:"confirmed_at"`
	ControlToken                    string                    `json:"control_token"`
	ControlTokenExpiresAt           string                    `json:"control_token_expires_at"`
	ReceiverAdoptionBootstrap       ReceiverAdoptionBootstrap `json:"receiver_adoption_bootstrap"`
	ReceiverAdoptionBootstrapSHA256 string                    `json:"receiver_adoption_bootstrap_sha256"`
}

func (r redeemAdoptionRequest) legacy() redeemInvitationRequest {
	return redeemInvitationRequest{InvitationToken: r.InvitationToken, ProjectID: r.ProjectID, FinalEventScope: r.FinalEventScope, ConsentSHA256: r.ConsentSHA256, ConfirmedAt: r.ConfirmedAt, ControlToken: r.ControlToken, ControlTokenExpiresAt: r.ControlTokenExpiresAt}
}
func validateBootstrap(b ReceiverAdoptionBootstrap, i Installation) error {
	expected := SourceBinding{SchemaVersion: SourceBindingSchemaVersion, InstallationID: i.InstallationID, XDeploymentID: i.XDeploymentID, OrgID: b.OrgID, ProjectID: b.ProjectID, SourceInstance: b.SourceInstance, EventScope: EventScopeModelCompleted, FeedSchema: CompletedFeedSchemaVersion}
	if b.InstallationID != i.InstallationID || b.OrgID != i.OrgID || b.ProjectID != i.ProjectID || b.SourceInstance != i.SourceInstance || b.SourceBindingSHA256 != i.SourceBindingSHA256 || PublicCanonicalSHA256(expected) != b.SourceBindingSHA256 || !v3DecimalID.MatchString(b.FeedKeyID) || !validV3Network(b.AllowedIP) || !controlKeyPrefixPattern.MatchString(b.KeyPrefix) || !controlFeedOperationID.MatchString(b.RegistrationOperationID) || b.VerifierScheme != "receiver_sha256_v1" {
		return ErrConflict
	}
	return nil
}
func readReceiverMode(ctx context.Context, tx PhaseProductTx, id string) (string, error) {
	var mode string
	e := tx.QueryRow(ctx, `SELECT COALESCE(to_jsonb(i)->>'receiver_mode','new_install') FROM xinsight_connector_installations i WHERE installation_id=$1`, id).Scan(&mode)
	return mode, e
}
func readAdoptionBootstrap(ctx context.Context, tx PhaseProductTx, i Installation) (ReceiverAdoptionBootstrap, error) {
	mode, e := readReceiverMode(ctx, tx, i.InstallationID)
	if e != nil || mode != ReceiverModeAdoptExisting {
		return ReceiverAdoptionBootstrap{}, ErrConflict
	}
	var status string
	e = tx.QueryRow(ctx, `SELECT status FROM completed_execution_feed_connections WHERE org_id=$1 AND project_id=$2 FOR UPDATE`, i.OrgID, i.ProjectID).Scan(&status)
	if e != nil || status != "active" {
		return ReceiverAdoptionBootstrap{}, ErrConflict
	}
	var active, staged int
	e = tx.QueryRow(ctx, `SELECT COUNT(*) FILTER(WHERE status='feed_active'),COUNT(*) FILTER(WHERE status='feed_staged') FROM api_keys WHERE org_id=$1 AND project_id=$2 AND purpose='xinsight.feed.read'`, i.OrgID, i.ProjectID).Scan(&active, &staged)
	if e != nil || active != 1 || staged != 0 {
		return ReceiverAdoptionBootstrap{}, ErrConflict
	}
	b := ReceiverAdoptionBootstrap{InstallationID: i.InstallationID, OrgID: i.OrgID, ProjectID: i.ProjectID, SourceInstance: i.SourceInstance, SourceBindingSHA256: i.SourceBindingSHA256}
	var key int64
	e = tx.QueryRow(ctx, `SELECT id,allowed_ips,key_prefix,registration_operation_id,verifier_scheme FROM api_keys WHERE org_id=$1 AND project_id=$2 AND purpose='xinsight.feed.read' AND status='feed_active' FOR UPDATE`, i.OrgID, i.ProjectID).Scan(&key, &b.AllowedIP, &b.KeyPrefix, &b.RegistrationOperationID, &b.VerifierScheme)
	if e != nil {
		return b, ErrConflict
	}
	b.FeedKeyID = strconv.FormatInt(key, 10)
	return b, validateBootstrap(b, i)
}

// Adoption intent is read from the same transaction as installation creation;
// this preflight does not pause, rotate, or otherwise mutate the old feed.
func validateAdoptionIntentTx(ctx context.Context, tx pgx.Tx, org, project int64) error {
	var status string
	e := tx.QueryRow(ctx, `SELECT status FROM completed_execution_feed_connections WHERE org_id=$1 AND project_id=$2 FOR UPDATE`, org, project).Scan(&status)
	if e != nil || status != "active" {
		return ErrConflict
	}
	var active, staged int
	e = tx.QueryRow(ctx, `SELECT COUNT(*) FILTER(WHERE status='feed_active'),COUNT(*) FILTER(WHERE status='feed_staged') FROM api_keys WHERE org_id=$1 AND project_id=$2 AND purpose='xinsight.feed.read'`, org, project).Scan(&active, &staged)
	if e != nil || active != 1 || staged != 0 {
		return ErrConflict
	}
	var prefix, operation, ip, scheme string
	e = tx.QueryRow(ctx, `SELECT key_prefix,registration_operation_id,allowed_ips,verifier_scheme FROM api_keys WHERE org_id=$1 AND project_id=$2 AND purpose='xinsight.feed.read' AND status='feed_active' FOR UPDATE`, org, project).Scan(&prefix, &operation, &ip, &scheme)
	if e != nil || !controlKeyPrefixPattern.MatchString(prefix) || !controlFeedOperationID.MatchString(operation) || !validV3Network(ip) || scheme != "receiver_sha256_v1" {
		return ErrConflict
	}
	return nil
}

// Bind requires the exact public identity frozen by the approved invitation
// intake. A v3 candidate cannot turn a legacy installation into adoption.
func ValidateAdoptionBootstrapCandidateTx(ctx context.Context, tx PhaseProductTx, i Installation, c ExistingReceiverCandidate) error {
	mode, err := readReceiverMode(ctx, tx, i.InstallationID)
	if err != nil || mode != ReceiverModeAdoptExisting {
		return ErrConflict
	}
	var raw []byte
	var digest string
	if tx.QueryRow(ctx, `SELECT receiver_adoption_bootstrap_canonical,receiver_adoption_bootstrap_sha256 FROM xinsight_connector_control_bindings WHERE installation_id=$1`, i.InstallationID).Scan(&raw, &digest) != nil {
		return ErrConflict
	}
	b := ReceiverAdoptionBootstrap{}
	if decodeClosedV3(raw, &b) != nil || validateBootstrap(b, i) != nil || PublicCanonicalSHA256(b) != digest || hashBytes(raw) != digest {
		return ErrConflict
	}
	if c.FeedKeyID != b.FeedKeyID || c.AllowedIP != b.AllowedIP || c.ReceiverRegistration.KeyPrefix != b.KeyPrefix || c.ReceiverRegistration.OperationID != b.RegistrationOperationID || c.ReceiverRegistration.VerifierScheme != b.VerifierScheme {
		return ErrConflict
	}
	return nil
}
func buildRedeemRequestForBinding(i Installation, b controlBindingRecord, invitation, project, token string, expiresAt time.Time) (redeemInvitationRequest, []byte, string, error) {
	legacy, wire, digest, e := buildRedeemRequest(i, invitation, project, token, expiresAt)
	if e != nil || len(b.adoptionBootstrapCanonical) == 0 {
		return legacy, wire, digest, e
	}
	var bootstrap ReceiverAdoptionBootstrap
	if decodeClosedV3(b.adoptionBootstrapCanonical, &bootstrap) != nil || validateBootstrap(bootstrap, i) != nil || PublicCanonicalSHA256(bootstrap) != b.adoptionBootstrapSHA256 {
		return legacy, nil, "", ErrConflict
	}
	canon, _ := json.Marshal(bootstrap)
	if !bytes.Equal(canon, b.adoptionBootstrapCanonical) {
		return legacy, nil, "", ErrConflict
	}
	r := redeemAdoptionRequest{SchemaVersion: RedeemAdoptionRequestSchema, InvitationToken: legacy.InvitationToken, ProjectID: legacy.ProjectID, FinalEventScope: legacy.FinalEventScope, ConsentSHA256: legacy.ConsentSHA256, ConfirmedAt: legacy.ConfirmedAt, ControlToken: legacy.ControlToken, ControlTokenExpiresAt: legacy.ControlTokenExpiresAt, ReceiverAdoptionBootstrap: bootstrap, ReceiverAdoptionBootstrapSHA256: b.adoptionBootstrapSHA256}
	wire, e = json.Marshal(r)
	return legacy, wire, hashBytes(wire), e
}
func decodeRedeemRequestForBinding(raw []byte, b controlBindingRecord) (redeemInvitationRequest, error) {
	if len(b.adoptionBootstrapCanonical) == 0 {
		return decodeRedeemRequest(raw)
	}
	var r redeemAdoptionRequest
	if decodeClosedV3(raw, &r) != nil || r.SchemaVersion != RedeemAdoptionRequestSchema || r.ReceiverAdoptionBootstrapSHA256 != b.adoptionBootstrapSHA256 || PublicCanonicalSHA256(r.ReceiverAdoptionBootstrap) != b.adoptionBootstrapSHA256 {
		return redeemInvitationRequest{}, ErrInvalid
	}
	canonical, err := json.Marshal(r)
	if err != nil || !bytes.Equal(canonical, raw) {
		return redeemInvitationRequest{}, ErrInvalid
	}
	return r.legacy(), nil
}

// A closed public projection of XI's existing installation v1. Hidden Source
// and control service identities are deliberately absent from this wire.
type adoptionRegistrationInstallation struct {
	SchemaVersion                     string   `json:"schema_version"`
	InstallationID                    string   `json:"installation_id"`
	DeploymentID                      string   `json:"deployment_id"`
	ConnectorType                     string   `json:"connector_type"`
	RequestedEventScope               []string `json:"requested_event_scope"`
	FinalEventScope                   []string `json:"final_event_scope,omitempty"`
	ProjectID                         string   `json:"project_id,omitempty"`
	SourceInstanceSHA256              string   `json:"source_instance_sha256,omitempty"`
	InstallationAcknowledgementSHA256 string   `json:"installation_acknowledgement_sha256,omitempty"`
	Status                            string   `json:"status"`
	DesiredState                      string   `json:"desired_state"`
	ObservedState                     string   `json:"observed_state"`
	StatusReasonCode                  string   `json:"status_reason_code,omitempty"`
	LastAgentSeenAt                   string   `json:"last_agent_seen_at,omitempty"`
	CreatedAt                         string   `json:"created_at"`
	UpdatedAt                         string   `json:"updated_at"`
	Version                           int64    `json:"version"`
}
type adoptionRegistrationResult struct {
	SchemaVersion                   string                           `json:"schema_version"`
	Installation                    adoptionRegistrationInstallation `json:"installation"`
	ReceiverAdoptionBootstrapSHA256 string                           `json:"receiver_adoption_bootstrap_sha256"`
}

func canonicalizeAdoptionRedeemResult(raw []byte, i Installation, b controlBindingRecord) ([]byte, error) {
	var r adoptionRegistrationResult
	if decodeClosedV3(raw, &r) != nil || r.SchemaVersion != RedeemAdoptionResultSchema {
		return nil, ErrInvalid
	}
	v := r.Installation
	ack := struct {
		SchemaVersion   string    `json:"schema_version"`
		InstallationID  string    `json:"installation_id"`
		DeploymentID    string    `json:"deployment_id"`
		ProjectID       string    `json:"project_id"`
		FinalEventScope []string  `json:"final_event_scope"`
		ConsentSHA256   string    `json:"consent_sha256"`
		ConfirmedAt     time.Time `json:"confirmed_at"`
	}{"xinsight.connector-installation-acknowledgement.v1", i.InstallationID, i.XDeploymentID, b.controlProjectID, []string{i.EventScope}, i.SourceBindingSHA256, i.ConsentedAt.UTC()}
	if r.ReceiverAdoptionBootstrapSHA256 != b.adoptionBootstrapSHA256 || !hashPattern.MatchString(b.adoptionBootstrapSHA256) ||
		v.SchemaVersion != "xinsight.connector-installation.v1" || v.InstallationID != i.InstallationID || v.DeploymentID != i.XDeploymentID || v.ConnectorType != "unirouter" || v.ProjectID != b.controlProjectID ||
		len(v.RequestedEventScope) != 1 || v.RequestedEventScope[0] != i.EventScope || len(v.FinalEventScope) != 1 || v.FinalEventScope[0] != i.EventScope ||
		v.InstallationAcknowledgementSHA256 != PublicCanonicalSHA256(ack) || (v.SourceInstanceSHA256 != "" && v.SourceInstanceSHA256 != hashBytes([]byte(i.SourceInstance))) ||
		v.DesiredState != "active" || v.Status != "pending" || v.ObservedState != "agent_registered" || v.Version < 1 ||
		!validControlObservedAt(v.CreatedAt) || !validControlObservedAt(v.UpdatedAt) || (v.LastAgentSeenAt != "" && !validControlObservedAt(v.LastAgentSeenAt)) {
		return nil, ErrConflict
	}
	created, _ := time.Parse(time.RFC3339Nano, v.CreatedAt)
	updated, _ := time.Parse(time.RFC3339Nano, v.UpdatedAt)
	if updated.Before(created) {
		return nil, ErrConflict
	}
	return json.Marshal(r)
}
