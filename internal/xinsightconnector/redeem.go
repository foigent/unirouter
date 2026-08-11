package xinsightconnector

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const connectorControlBearerLifetime = 24 * time.Hour

// PreparedInvitationRedeem is the exact request identity durably frozen by
// PrepareRedeem. CanonicalRequest contains the invitation and newly generated
// control bearer, so callers must treat it as secret and must never log it.
type PreparedInvitationRedeem struct {
	InstallationID        string
	XDeploymentID         string
	ControlProjectID      string
	CanonicalRequest      []byte
	RequestSHA256         string
	ControlTokenExpiresAt time.Time
}

type redeemInvitationRequest struct {
	InvitationToken       string `json:"invitation_token"`
	ProjectID             string `json:"project_id"`
	FinalEventScope       string `json:"final_event_scope"`
	ConsentSHA256         string `json:"consent_sha256"`
	ConfirmedAt           string `json:"confirmed_at"`
	ControlToken          string `json:"control_token"`
	ControlTokenExpiresAt string `json:"control_token_expires_at"`
}

type registrationResultWire struct {
	Installation json.RawMessage `json:"installation"`
	Operation    json.RawMessage `json:"operation"`
}

// PrepareRedeem transitions invitation_staged to redeem_pending before any
// network send. Every later call reconstructs and verifies the byte-identical
// request from durable product facts; it never regenerates the bearer.
func (journal *PostgresPhaseJournal) PrepareRedeem(
	ctx context.Context,
	installationID string,
) (PreparedInvitationRedeem, error) {
	if journal == nil || journal.db == nil || journal.cipher == nil || journal.now == nil ||
		journal.controlBearer == nil || !validExactExternalIdentity(installationID) {
		return PreparedInvitationRedeem{}, ErrInvalid
	}
	tx, err := journal.db.Begin(ctx)
	if err != nil {
		return PreparedInvitationRedeem{}, fmt.Errorf("%w: begin invitation redeem preparation: %v", ErrUnavailable, err)
	}
	defer tx.Rollback(ctx)
	scopeInstallation, err := readInstallationByIdentity(ctx, tx, installationID, false)
	if err != nil {
		return PreparedInvitationRedeem{}, err
	}
	if err := lockPhaseProductScope(ctx, tx, scopeInstallation); err != nil {
		return PreparedInvitationRedeem{}, err
	}
	installation, err := readInstallationByIdentity(ctx, tx, installationID, true)
	if err != nil {
		return PreparedInvitationRedeem{}, err
	}
	if installation.OrgID != scopeInstallation.OrgID || installation.ProjectID != scopeInstallation.ProjectID {
		return PreparedInvitationRedeem{}, ErrConflict
	}
	if installation.DesiredState != DesiredActive || installation.ObservedState == ObservedDisconnected ||
		installation.EventScope != EventScopeModelCompleted ||
		!hashPattern.MatchString(installation.SourceBindingSHA256) {
		return PreparedInvitationRedeem{}, ErrConflict
	}
	binding, err := readControlBinding(ctx, tx, installationID, true)
	if err != nil {
		return PreparedInvitationRedeem{}, err
	}
	if binding.invitationTokenEnc == "" {
		return PreparedInvitationRedeem{}, ErrConflict
	}

	invitation, err := journal.cipher.open(
		connectorInvitationSecret, installationID, binding.invitationTokenEnc,
	)
	if err != nil || installation.InvitationSHA256 != hashBytes([]byte(invitation)) {
		return PreparedInvitationRedeem{}, ErrConflict
	}

	now := journal.now().UTC().Truncate(time.Microsecond)
	if binding.controlTokenEnc == "" {
		controlToken, err := journal.controlBearer()
		if err != nil || !validConnectorControlBearer(controlToken) || exactSecretEqual(invitation, controlToken) {
			return PreparedInvitationRedeem{}, fmt.Errorf("%w: generate Connector control bearer", ErrUnavailable)
		}
		sealedControl, err := journal.cipher.seal(connectorControlSecret, installationID, controlToken)
		if err != nil {
			return PreparedInvitationRedeem{}, err
		}
		controlProjectID, err := readConnectorControlProjectID(ctx, tx, installation)
		if err != nil {
			return PreparedInvitationRedeem{}, err
		}
		expiresAt := now.Add(connectorControlBearerLifetime)
		request, canonical, requestSHA, err := buildRedeemRequest(
			installation, invitation, controlProjectID, controlToken, expiresAt,
		)
		if err != nil || request.ControlToken != controlToken {
			return PreparedInvitationRedeem{}, ErrInvalid
		}
		tag, err := tx.Exec(ctx, `
UPDATE xinsight_connector_control_bindings
SET control_project_id=$2,control_token_enc=$3,control_token_sha256=$4,
    control_token_expires_at=$5,redeem_request_sha256=$6,updated_at=$7
WHERE installation_id=$1
  AND invitation_token_enc IS NOT NULL
  AND control_token_enc IS NULL
  AND control_token_sha256 IS NULL
  AND control_token_expires_at IS NULL
  AND redeem_request_sha256 IS NULL
  AND redeem_result_sha256 IS NULL`,
			installationID, controlProjectID, sealedControl, hashBytes([]byte(controlToken)),
			expiresAt, requestSHA, now,
		)
		if err != nil || tag.RowsAffected() != 1 {
			return PreparedInvitationRedeem{}, fmt.Errorf("%w: persist pending invitation redeem", ErrConflict)
		}
		if err := commitConnectorTx(ctx, tx, "prepare invitation redeem"); err != nil {
			return PreparedInvitationRedeem{}, err
		}
		return PreparedInvitationRedeem{
			InstallationID: installation.InstallationID, XDeploymentID: installation.XDeploymentID,
			ControlProjectID: controlProjectID, CanonicalRequest: bytes.Clone(canonical),
			RequestSHA256: requestSHA, ControlTokenExpiresAt: expiresAt,
		}, nil
	}

	prepared, err := journal.reconstructPendingRedeem(installation, binding, invitation, now)
	if err != nil {
		return PreparedInvitationRedeem{}, err
	}
	if err := commitConnectorTx(ctx, tx, "replay pending invitation redeem"); err != nil {
		return PreparedInvitationRedeem{}, err
	}
	return prepared, nil
}

func readConnectorControlProjectID(
	ctx context.Context,
	tx PhaseProductTx,
	installation Installation,
) (string, error) {
	var slug string
	if err := tx.QueryRow(ctx, `
SELECT slug FROM projects WHERE id=$1 AND org_id=$2 AND status='active'`,
		installation.ProjectID, installation.OrgID,
	).Scan(&slug); err != nil {
		return "", ErrConflict
	}
	identity := "project." + slug
	if !validControlText(identity, 128) {
		return "", ErrConflict
	}
	return identity, nil
}

// FinalizeRedeem validates the exact registration result and only then retires
// the invitation. A response-loss retry with the same canonical result is a
// no-op; any identity, project, scope, operation or digest drift fails closed.
func (journal *PostgresPhaseJournal) FinalizeRedeem(
	ctx context.Context,
	prepared PreparedInvitationRedeem,
	canonicalResult []byte,
) error {
	if journal == nil || journal.db == nil || journal.cipher == nil || journal.now == nil ||
		!validExactExternalIdentity(prepared.InstallationID) ||
		!validExactDeploymentIdentity(prepared.XDeploymentID) ||
		!validControlText(prepared.ControlProjectID, 128) ||
		prepared.RequestSHA256 != hashBytes(prepared.CanonicalRequest) ||
		len(canonicalResult) == 0 || len(canonicalResult) > 64*1024 {
		return ErrInvalid
	}
	tx, err := journal.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("%w: begin invitation redeem finalization: %v", ErrUnavailable, err)
	}
	defer tx.Rollback(ctx)
	installation, err := readInstallationByIdentity(ctx, tx, prepared.InstallationID, true)
	if err != nil {
		return err
	}
	binding, err := readControlBinding(ctx, tx, prepared.InstallationID, true)
	if err != nil {
		return err
	}
	resultSHA := hashBytes(canonicalResult)
	if binding.invitationTokenEnc == "" {
		if binding.redeemRequestSHA256 != prepared.RequestSHA256 ||
			binding.redeemResultSHA256 != resultSHA {
			return ErrConflict
		}
		return commitConnectorTx(ctx, tx, "invitation redeem finalization replay")
	}
	invitation, err := journal.cipher.open(
		connectorInvitationSecret, installation.InstallationID, binding.invitationTokenEnc,
	)
	if err != nil {
		return ErrConflict
	}
	reconstructed, err := journal.reconstructPendingRedeem(
		installation, binding, invitation, journal.now().UTC().Truncate(time.Microsecond),
	)
	if err != nil || reconstructed.RequestSHA256 != prepared.RequestSHA256 ||
		!bytes.Equal(reconstructed.CanonicalRequest, prepared.CanonicalRequest) ||
		reconstructed.XDeploymentID != prepared.XDeploymentID ||
		reconstructed.ControlProjectID != prepared.ControlProjectID ||
		!reconstructed.ControlTokenExpiresAt.Equal(prepared.ControlTokenExpiresAt) {
		return ErrConflict
	}
	request, err := decodeRedeemRequest(prepared.CanonicalRequest)
	if err != nil {
		return err
	}
	if bytes.Contains(canonicalResult, []byte(request.InvitationToken)) ||
		bytes.Contains(canonicalResult, []byte(request.ControlToken)) {
		return ErrConflict
	}
	if err := validateRegistrationResult(canonicalResult, installation, binding.controlProjectID); err != nil {
		return err
	}
	now := journal.now().UTC().Truncate(time.Microsecond)
	tag, err := tx.Exec(ctx, `
UPDATE xinsight_connector_control_bindings
SET invitation_token_enc=NULL,redeem_result_sha256=$2,updated_at=$3
WHERE installation_id=$1
  AND invitation_token_enc IS NOT NULL
  AND control_token_enc IS NOT NULL
  AND redeem_request_sha256=$4
  AND redeem_result_sha256 IS NULL`,
		installation.InstallationID, resultSHA, now, prepared.RequestSHA256,
	)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("%w: finalize invitation redeem", ErrConflict)
	}
	return commitConnectorTx(ctx, tx, "finalize invitation redeem")
}

func (journal *PostgresPhaseJournal) reconstructPendingRedeem(
	installation Installation,
	binding controlBindingRecord,
	invitation string,
	now time.Time,
) (PreparedInvitationRedeem, error) {
	if binding.invitationTokenEnc == "" || binding.controlTokenEnc == "" ||
		!hashPattern.MatchString(binding.controlTokenSHA256) ||
		!binding.controlTokenExpiresAt.Valid ||
		!binding.controlTokenExpiresAt.Time.After(now) ||
		!hashPattern.MatchString(binding.redeemRequestSHA256) ||
		binding.redeemResultSHA256 != "" ||
		!validControlText(binding.controlProjectID, 128) {
		return PreparedInvitationRedeem{}, ErrConflict
	}
	controlToken, err := journal.cipher.open(
		connectorControlSecret, installation.InstallationID, binding.controlTokenEnc,
	)
	if err != nil || !validConnectorControlBearer(controlToken) ||
		binding.controlTokenSHA256 != hashBytes([]byte(controlToken)) {
		return PreparedInvitationRedeem{}, ErrConflict
	}
	_, canonical, requestSHA, err := buildRedeemRequest(
		installation, invitation, binding.controlProjectID, controlToken,
		binding.controlTokenExpiresAt.Time,
	)
	if err != nil || requestSHA != binding.redeemRequestSHA256 {
		return PreparedInvitationRedeem{}, ErrConflict
	}
	return PreparedInvitationRedeem{
		InstallationID: installation.InstallationID, XDeploymentID: installation.XDeploymentID,
		ControlProjectID: binding.controlProjectID, CanonicalRequest: bytes.Clone(canonical),
		RequestSHA256:         requestSHA,
		ControlTokenExpiresAt: binding.controlTokenExpiresAt.Time.UTC(),
	}, nil
}

func buildRedeemRequest(
	installation Installation,
	invitation, controlProjectID, controlToken string,
	expiresAt time.Time,
) (redeemInvitationRequest, []byte, string, error) {
	request := redeemInvitationRequest{
		InvitationToken:       invitation,
		ProjectID:             controlProjectID,
		FinalEventScope:       installation.EventScope,
		ConsentSHA256:         installation.SourceBindingSHA256,
		ConfirmedAt:           installation.ConsentedAt.UTC().Format(time.RFC3339Nano),
		ControlToken:          controlToken,
		ControlTokenExpiresAt: expiresAt.UTC().Format(time.RFC3339Nano),
	}
	if !validConnectorSecret(request.InvitationToken) ||
		!validControlText(request.ProjectID, 128) ||
		request.FinalEventScope != EventScopeModelCompleted ||
		!hashPattern.MatchString(request.ConsentSHA256) ||
		!validControlObservedAt(request.ConfirmedAt) ||
		!validConnectorControlBearer(request.ControlToken) ||
		!validControlObservedAt(request.ControlTokenExpiresAt) {
		return redeemInvitationRequest{}, nil, "", ErrInvalid
	}
	canonical, err := json.Marshal(request)
	if err != nil {
		return redeemInvitationRequest{}, nil, "", fmt.Errorf("%w: canonicalize invitation redeem", ErrUnavailable)
	}
	return request, canonical, hashBytes(canonical), nil
}

func decodeRedeemRequest(raw []byte) (redeemInvitationRequest, error) {
	if err := rejectDuplicateControlV2Fields(raw); err != nil {
		return redeemInvitationRequest{}, err
	}
	var request redeemInvitationRequest
	if err := decodeStrictControlV2(raw, &request); err != nil {
		return redeemInvitationRequest{}, err
	}
	canonical, err := json.Marshal(request)
	if err != nil || !bytes.Equal(canonical, raw) {
		return redeemInvitationRequest{}, ErrInvalid
	}
	return request, nil
}

func validateRegistrationResult(
	raw []byte,
	installation Installation,
	controlProjectID string,
) error {
	if err := rejectDuplicateControlV2Fields(raw); err != nil {
		return err
	}
	var result registrationResultWire
	if err := decodeStrictControlV2(raw, &result); err != nil {
		return err
	}
	if len(result.Installation) == 0 || len(result.Operation) == 0 {
		return ErrInvalid
	}
	identity, err := decodeRegistrationInstallationIdentity(result.Installation)
	if err != nil {
		return err
	}
	if identity.InstallationID != installation.InstallationID ||
		identity.XDeploymentID != installation.XDeploymentID ||
		identity.ProjectID != controlProjectID ||
		len(identity.EventScope) != 1 || identity.EventScope[0] != installation.EventScope {
		return ErrConflict
	}
	operation, err := DecodeControlOperationV2(result.Operation)
	if err != nil {
		return err
	}
	canonicalOperation, err := operation.CanonicalJSON()
	if err != nil || !bytes.Equal(canonicalOperation, result.Operation) ||
		operation.InstallationID != installation.InstallationID ||
		operation.ProjectID != controlProjectID ||
		operation.Phase != ControlPhasePreparePaused ||
		len(operation.EventScope) != 1 || operation.EventScope[0] != installation.EventScope {
		return ErrConflict
	}
	canonical, err := json.Marshal(result)
	if err != nil || !bytes.Equal(canonical, raw) {
		return ErrInvalid
	}
	return nil
}

type registrationInstallationIdentity struct {
	InstallationID string   `json:"installation_id"`
	XDeploymentID  string   `json:"x_deployment_id"`
	ProjectID      string   `json:"project_id"`
	EventScope     []string `json:"event_scope"`
}

func decodeRegistrationInstallationIdentity(raw []byte) (registrationInstallationIdentity, error) {
	if err := rejectDuplicateControlV2Fields(raw); err != nil {
		return registrationInstallationIdentity{}, err
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return registrationInstallationIdentity{}, ErrInvalid
	}
	for field := range object {
		lower := strings.ToLower(field)
		if strings.Contains(lower, "token") || strings.Contains(lower, "bearer") ||
			strings.Contains(lower, "secret") || strings.Contains(lower, "credential") {
			return registrationInstallationIdentity{}, ErrConflict
		}
	}
	var identity registrationInstallationIdentity
	if err := unmarshalRequiredRegistrationField(object, "installation_id", &identity.InstallationID); err != nil {
		return registrationInstallationIdentity{}, err
	}
	if err := unmarshalRequiredRegistrationField(object, "x_deployment_id", &identity.XDeploymentID); err != nil {
		return registrationInstallationIdentity{}, err
	}
	if err := unmarshalRequiredRegistrationField(object, "project_id", &identity.ProjectID); err != nil {
		return registrationInstallationIdentity{}, err
	}
	if err := unmarshalRequiredRegistrationField(object, "event_scope", &identity.EventScope); err != nil {
		return registrationInstallationIdentity{}, err
	}
	canonical, err := json.Marshal(object)
	if err != nil || !bytes.Equal(canonical, raw) ||
		!validExactExternalIdentity(identity.InstallationID) ||
		!validExactDeploymentIdentity(identity.XDeploymentID) ||
		!validControlText(identity.ProjectID, 128) {
		return registrationInstallationIdentity{}, ErrInvalid
	}
	return identity, nil
}

func unmarshalRequiredRegistrationField(
	object map[string]json.RawMessage,
	field string,
	destination any,
) error {
	raw, ok := object[field]
	if !ok || len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ErrInvalid
	}
	if err := json.Unmarshal(raw, destination); err != nil {
		return ErrInvalid
	}
	return nil
}

func randomConnectorControlBearer() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "xib_" + base64.RawURLEncoding.EncodeToString(raw), nil
}

func validConnectorControlBearer(value string) bool {
	if !strings.HasPrefix(value, "xib_") || len(value) != 47 || !validConnectorSecret(value) {
		return false
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(strings.TrimPrefix(value, "xib_"))
	return err == nil && len(decoded) == 32
}
