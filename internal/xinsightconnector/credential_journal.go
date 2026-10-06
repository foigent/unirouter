package xinsightconnector

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const credentialRotationWindow = 6 * time.Hour
const credentialMigrationHead = "000125_xinsight_control_credential_lifecycle"

type CredentialLocalStatus struct {
	State                    string    `json:"state"`
	CurrentTokenSHA256       string    `json:"current_token_sha256"`
	CurrentExpiresAt         time.Time `json:"current_expires_at"`
	PendingOperationID       string    `json:"pending_operation_id,omitempty"`
	PendingState             string    `json:"pending_state,omitempty"`
	PendingReplacementSHA256 string    `json:"pending_replacement_sha256,omitempty"`
}

type preparedCredentialOperation struct {
	request       CredentialRequest
	canonical     []byte
	requestSHA256 string
	replacement   string
	state         string
	receipt       *CredentialReceipt
}

type credentialSetupBinding struct {
	XIOrigin       string `json:"xi_origin"`
	UROrigin       string `json:"ur_origin"`
	InstallationID string `json:"installation_id"`
	DeploymentID   string `json:"deployment_id"`
}

// PrepareRecoveryTx is called only inside the original server-side human
// management transaction, after reauthorization of the current UR actor.
// Ticket resolution alone never invokes this writer.
func (journal *PostgresPhaseJournal) PrepareRecoveryTx(ctx context.Context, tx pgx.Tx, id, ticket string, resolution CredentialRecoveryResolution) (string, error) {
	if journal == nil || journal.cipher == nil || journal.controlBearer == nil || tx == nil ||
		!validExactExternalIdentity(id) || !credentialTicketPattern.MatchString(ticket) ||
		resolution.InstallationID != id || resolution.Validate(journal.now().UTC()) != nil {
		return "", ErrInvalid
	}
	installation, err := readInstallationByIdentity(ctx, tx, id, true)
	if err != nil || installation.DesiredState != DesiredActive || installation.ObservedState == ObservedDisconnected {
		return "", ErrConflict
	}
	binding, err := readControlBinding(ctx, tx, id, true)
	if err != nil {
		return "", err
	}
	scope, err := readCredentialScopeTx(ctx, tx, installation, binding)
	if err != nil || binding.invitationTokenEnc != "" || !hashPattern.MatchString(binding.redeemResultSHA256) ||
		resolution.DeploymentID != installation.XDeploymentID || resolution.ProjectID != binding.controlProjectID ||
		resolution.FinalEventScope[0] != installation.EventScope || resolution.SourceBindingSHA256 != installation.SourceBindingSHA256 ||
		resolution.XInsightOrigin != scope.XIOrigin || resolution.UniRouterOrigin != scope.UROrigin {
		return "", ErrConflict
	}
	if binding.controlTokenExpiresAt.Valid && binding.controlTokenExpiresAt.Time.After(journal.now().UTC()) && resolution.PreviousTokenSHA256 == binding.controlTokenSHA256 {
		return "", ErrConflict
	}
	var oldPending *preparedCredentialOperation
	if pending, e := journal.readPendingCredential(ctx, tx, id); e == nil {
		if pending.request.Kind == "recover" && pending.request.RecoveryID != nil && *pending.request.RecoveryID == resolution.RecoveryID &&
			pending.request.RecoveryTicket != nil && *pending.request.RecoveryTicket == ticket &&
			pending.request.PreviousTokenSHA256 == resolution.PreviousTokenSHA256 {
			return pending.request.OperationID, nil
		}
		oldPending = &pending
	} else if e != pgx.ErrNoRows {
		return "", e
	}
	previous := ""
	if resolution.PreviousTokenSHA256 == binding.controlTokenSHA256 {
		previous, err = journal.cipher.open(connectorControlSecret, id, binding.controlTokenEnc)
		if err != nil || hashBytes([]byte(previous)) != resolution.PreviousTokenSHA256 {
			return "", ErrConflict
		}
	} else if oldPending != nil && resolution.PreviousTokenSHA256 == hashBytes([]byte(oldPending.replacement)) {
		previous = oldPending.replacement
		// XI's ticket identifies its authoritative expired current. UR may
		// have lost the prepared or committed response entirely.
		if oldPending.receipt != nil {
			expires, _ := time.Parse(time.RFC3339Nano, oldPending.receipt.ReplacementExpiresAt)
			if expires.After(journal.now().UTC()) {
				return "", ErrConflict
			}
		}
	} else {
		return "", ErrConflict
	}
	var supersededID any
	if oldPending != nil {
		if resolution.SupersededOperationID != "" {
			if oldPending.request.OperationID != resolution.SupersededOperationID ||
				hashBytes([]byte(oldPending.replacement)) != resolution.SupersededReplacementSHA256 ||
				oldPending.state != "pending" {
				return "", ErrConflict
			}
			if oldPending.receipt != nil {
				expires, _ := time.Parse(time.RFC3339Nano, oldPending.receipt.ReplacementExpiresAt)
				if expires.After(journal.now().UTC()) {
					return "", ErrConflict
				}
			}
		} else if resolution.PreviousTokenSHA256 != hashBytes([]byte(oldPending.replacement)) {
			return "", ErrConflict
		}
		supersededID = oldPending.request.OperationID
		if tag, e := tx.Exec(ctx, `UPDATE xinsight_connector_credential_operations SET state='superseded',updated_at=$2 WHERE operation_id=$1 AND state IN ('pending','committed')`, oldPending.request.OperationID, journal.now().UTC()); e != nil || tag.RowsAffected() != 1 {
			return "", ErrConflict
		}
	} else if resolution.SupersededOperationID != "" {
		return "", ErrConflict
	}
	replacement, err := journal.controlBearer()
	if err != nil || !validConnectorControlBearer(replacement) || replacement == previous {
		return "", ErrUnavailable
	}
	opID, err := randomCredentialID("xcr_")
	if err != nil {
		return "", err
	}
	superID, superSHA := resolution.SupersededOperationID, resolution.SupersededReplacementSHA256
	recoveryID := resolution.RecoveryID
	request := CredentialRequest{SchemaVersion: CredentialRequestSchema, Kind: "recover", InstallationID: id,
		DeploymentID: installation.XDeploymentID, ProjectID: binding.controlProjectID, FinalEventScope: []string{installation.EventScope},
		SourceBindingSHA256: installation.SourceBindingSHA256, XInsightOrigin: scope.XIOrigin, UniRouterOrigin: scope.UROrigin,
		OperationID: opID, PreviousTokenSHA256: resolution.PreviousTokenSHA256, ReplacementToken: replacement,
		RecoveryID: &recoveryID, RecoveryTicket: &ticket, PreviousToken: &previous,
		SupersededOperationID: &superID, SupersededReplacementSHA256: &superSHA}
	canonical, err := request.CanonicalJSON()
	if err != nil {
		return "", err
	}
	requestEnc, err := journal.cipher.seal(connectorCredentialRequestSecret, id, string(canonical))
	if err != nil {
		return "", err
	}
	replacementEnc, err := journal.cipher.seal(connectorControlSecret, id, replacement)
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx, `INSERT INTO xinsight_connector_credential_operations
(operation_id,installation_id,org_id,project_id,kind,previous_token_sha256,replacement_token_enc,replacement_token_sha256,request_enc,request_sha256,superseded_operation_id)
VALUES($1,$2,$3,$4,'recover',$5,$6,$7,$8,$9,$10)`, opID, id, installation.OrgID, installation.ProjectID,
		request.PreviousTokenSHA256, replacementEnc, hashBytes([]byte(replacement)), requestEnc, hashBytes(canonical), supersededID)
	if err != nil {
		return "", ErrConflict
	}
	return opID, nil
}

func randomCredentialID(prefix string) (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", ErrUnavailable
	}
	return prefix + base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

// readCredentialScopeTx runs only after the installation lock. Its original
// saved setup and completed redeem origin are immutable consent, distinct from
// the source-binding digest on the installation row.
func readCredentialScopeTx(ctx context.Context, tx pgx.Tx, installation Installation, binding controlBindingRecord) (credentialSetupBinding, error) {
	var raw []byte
	var sha string
	var fence int64
	if err := tx.QueryRow(ctx, `SELECT setup_consent_canonical,setup_consent_sha256,control_fence
FROM xinsight_connector_installations WHERE installation_id=$1 AND setup_request_id IS NOT NULL`, installation.InstallationID).Scan(&raw, &sha, &fence); err != nil {
		return credentialSetupBinding{}, ErrConflict
	}
	if hashBytes(raw) != sha || fence != installation.ControlFence || !binding.expanded || !validFrozenHTTPSOrigin(binding.redeemedOrigin) {
		return credentialSetupBinding{}, ErrConflict
	}
	var saved struct {
		ControlFence int64                  `json:"control_fence"`
		XIConsent    credentialSetupBinding `json:"xi_consent"`
	}
	if json.Unmarshal(raw, &saved) != nil || saved.ControlFence != installation.ControlFence ||
		saved.XIConsent.InstallationID != installation.InstallationID || saved.XIConsent.DeploymentID != installation.XDeploymentID ||
		saved.XIConsent.XIOrigin != binding.redeemedOrigin || !validFrozenHTTPSOrigin(saved.XIConsent.UROrigin) {
		return credentialSetupBinding{}, ErrConflict
	}
	return saved.XIConsent, nil
}

// ValidateCredentialOriginTx proves the fixed XI destination from original
// durable consent and redeem evidence before any secret-bearing HTTP request.
// It deliberately works after bearer expiry; ordinary Poll/ACK do not use it.
func (journal *PostgresPhaseJournal) ValidateCredentialOriginTx(ctx context.Context, tx pgx.Tx, id, origin string) error {
	if journal == nil || tx == nil || !validExactExternalIdentity(id) || !validFrozenHTTPSOrigin(origin) {
		return ErrInvalid
	}
	installation, err := readInstallationByIdentity(ctx, tx, id, true)
	if err != nil || installation.DesiredState != DesiredActive || installation.ObservedState == ObservedDisconnected {
		return ErrConflict
	}
	binding, err := readControlBinding(ctx, tx, id, true)
	if err != nil {
		return err
	}
	scope, err := readCredentialScopeTx(ctx, tx, installation, binding)
	if err != nil || scope.XIOrigin != origin || binding.invitationTokenEnc != "" ||
		!hashPattern.MatchString(binding.redeemResultSHA256) {
		return ErrConflict
	}
	return nil
}

func (journal *PostgresPhaseJournal) ValidateCredentialOrigin(ctx context.Context, id, origin string) error {
	if journal == nil || journal.db == nil {
		return ErrInvalid
	}
	tx, err := journal.db.Begin(ctx)
	if err != nil {
		return ErrUnavailable
	}
	defer tx.Rollback(ctx)
	if err := journal.ValidateCredentialOriginTx(ctx, tx, id, origin); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (journal *PostgresPhaseJournal) ListCredentialDueInstallations(ctx context.Context, limit int) ([]RuntimeInstallationDescriptor, error) {
	if journal == nil || journal.db == nil || journal.now == nil || limit < 1 || limit > maxRuntimeInstallationBatch {
		return nil, ErrInvalid
	}
	ready, err := journal.credentialSchemaReady(ctx)
	if err != nil {
		return nil, err
	}
	if !ready {
		return []RuntimeInstallationDescriptor{}, nil
	}
	now := journal.now().UTC()
	rows, err := journal.db.Query(ctx, `SELECT i.installation_id,i.x_deployment_id,b.control_project_id
FROM xinsight_connector_installations i JOIN xinsight_connector_control_bindings b USING(installation_id)
WHERE i.desired_state='active' AND i.observed_state<>'disconnected' AND i.setup_request_id IS NOT NULL
 AND b.invitation_token_enc IS NULL AND b.redeem_result_sha256 IS NOT NULL AND b.redeemed_origin IS NOT NULL
 AND ((b.control_token_expires_at>$2 AND b.control_token_expires_at<=$3)
   OR EXISTS(SELECT 1 FROM xinsight_connector_credential_operations o WHERE o.installation_id=i.installation_id AND o.state IN ('pending','committed')))
ORDER BY i.installation_id COLLATE "C" LIMIT $1`, limit, now, now.Add(credentialRotationWindow))
	if err != nil {
		return nil, fmt.Errorf("%w: discover credential due", ErrUnavailable)
	}
	defer rows.Close()
	out := make([]RuntimeInstallationDescriptor, 0, limit)
	for rows.Next() {
		var d RuntimeInstallationDescriptor
		if err := rows.Scan(&d.InstallationID, &d.XDeploymentID, &d.ControlProjectID); err != nil {
			return nil, ErrUnavailable
		}
		if !validExactExternalIdentity(d.InstallationID) || !validExactDeploymentIdentity(d.XDeploymentID) || !validControlText(d.ControlProjectID, 128) {
			return nil, ErrConflict
		}
		out = append(out, d)
	}
	if rows.Err() != nil {
		return nil, ErrUnavailable
	}
	return out, nil
}

func (journal *PostgresPhaseJournal) credentialSchemaReady(ctx context.Context) (bool, error) {
	var present bool
	if err := journal.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, credentialMigrationHead).Scan(&present); err != nil {
		return false, fmt.Errorf("%w: read credential lifecycle schema", ErrUnavailable)
	}
	return present, nil
}

func (journal *PostgresPhaseJournal) CredentialStatus(ctx context.Context, id string) (CredentialLocalStatus, error) {
	if journal == nil || journal.db == nil || journal.now == nil || !validExactExternalIdentity(id) {
		return CredentialLocalStatus{}, ErrInvalid
	}
	var state CredentialLocalStatus
	var expires, replacementExpires sql.NullTime
	var desired, observed string
	var opID, opState, replacement sql.NullString
	ready, err := journal.credentialSchemaReady(ctx)
	if err != nil {
		return state, err
	}
	query := `SELECT i.desired_state,i.observed_state,COALESCE(b.control_token_sha256,''),b.control_token_expires_at,
COALESCE(o.operation_id,''),COALESCE(o.state,''),COALESCE(o.replacement_token_sha256,''),o.replacement_expires_at
FROM xinsight_connector_installations i JOIN xinsight_connector_control_bindings b USING(installation_id)
LEFT JOIN xinsight_connector_credential_operations o ON o.installation_id=i.installation_id AND o.state IN ('pending','committed')
WHERE i.installation_id=$1`
	if !ready {
		query = `SELECT i.desired_state,i.observed_state,COALESCE(b.control_token_sha256,''),b.control_token_expires_at,
''::text,''::text,''::text,NULL::timestamptz FROM xinsight_connector_installations i JOIN xinsight_connector_control_bindings b USING(installation_id)
WHERE i.installation_id=$1`
	}
	err = journal.db.QueryRow(ctx, query, id).Scan(&desired, &observed, &state.CurrentTokenSHA256, &expires, &opID, &opState, &replacement, &replacementExpires)
	if err != nil {
		return state, ErrConflict
	}
	state.PendingOperationID, state.PendingState, state.PendingReplacementSHA256 = opID.String, opState.String, replacement.String
	if expires.Valid {
		state.CurrentExpiresAt = expires.Time.UTC()
	}
	switch {
	case desired == DesiredDisconnected || observed == ObservedDisconnected:
		state.State = "revoked"
	case (state.PendingState == "pending" || state.PendingState == "committed") &&
		replacementExpires.Valid && !replacementExpires.Time.After(journal.now().UTC()):
		// A saved receipt proves the replacement's deadline. Without a
		// receipt, the peer outcome is unknown and remains pending instead.
		state.State = "expired_recovery_required"
	case state.PendingState == "committed":
		state.State = "committed_unpromoted"
	case state.PendingState == "pending":
		state.State = "rotation_pending"
	case !expires.Valid:
		state.State = "credential_unavailable"
	case !expires.Time.After(journal.now().UTC()):
		state.State = "expired_recovery_required"
	case expires.Time.Sub(journal.now().UTC()) <= credentialRotationWindow:
		state.State = "rotation_due"
	default:
		state.State = "healthy"
	}
	return state, nil
}

func readPendingCredentialTx(ctx context.Context, tx pgx.Tx, id string, lock bool) (string, string, string, string, []byte, string, []byte, string, error) {
	query := `SELECT operation_id,kind,request_enc,request_sha256,receipt_canonical,state,replacement_token_enc,replacement_token_sha256
FROM xinsight_connector_credential_operations WHERE installation_id=$1 AND state IN ('pending','committed')`
	if lock {
		query += " FOR UPDATE"
	}
	var opID, kind, enc, sha, state, replacementEnc, replacementSHA string
	var receipt []byte
	err := tx.QueryRow(ctx, query, id).Scan(&opID, &kind, &enc, &sha, &receipt, &state, &replacementEnc, &replacementSHA)
	return opID, kind, enc, sha, receipt, state, []byte(replacementEnc), replacementSHA, err
}

func (journal *PostgresPhaseJournal) readPendingCredential(ctx context.Context, tx pgx.Tx, id string) (preparedCredentialOperation, error) {
	opID, kind, enc, sha, receiptRaw, state, replacementEnc, replacementSHA, err := readPendingCredentialTx(ctx, tx, id, true)
	if err != nil {
		return preparedCredentialOperation{}, err
	}
	canonicalString, err := journal.cipher.open(connectorCredentialRequestSecret, id, enc)
	if err != nil || hashBytes([]byte(canonicalString)) != sha {
		return preparedCredentialOperation{}, ErrConflict
	}
	request, err := DecodeCredentialRequest([]byte(canonicalString))
	if err != nil || request.InstallationID != id || request.OperationID != opID || request.Kind != kind || request.ReplacementToken == "" {
		return preparedCredentialOperation{}, ErrConflict
	}
	replacement, err := journal.cipher.open(connectorControlSecret, id, string(replacementEnc))
	if err != nil || replacement != request.ReplacementToken || replacementSHA != hashBytes([]byte(replacement)) {
		return preparedCredentialOperation{}, ErrConflict
	}
	prepared := preparedCredentialOperation{request: request, canonical: []byte(canonicalString), requestSHA256: sha, replacement: replacement, state: state}
	if len(receiptRaw) != 0 {
		receipt, err := DecodeCredentialReceipt(receiptRaw)
		if err != nil || validateCredentialReceipt(prepared, receipt) != nil {
			return preparedCredentialOperation{}, ErrConflict
		}
		prepared.receipt = &receipt
	}
	return prepared, nil
}

func (journal *PostgresPhaseJournal) PendingCredential(ctx context.Context, id string) (preparedCredentialOperation, error) {
	tx, err := journal.db.Begin(ctx)
	if err != nil {
		return preparedCredentialOperation{}, ErrUnavailable
	}
	defer tx.Rollback(ctx)
	return journal.readPendingCredential(ctx, tx, id)
}

// PrepareRotate freezes a new secret and complete request before the first
// network send. A previous pending operation is returned byte-for-byte.
func (journal *PostgresPhaseJournal) PrepareRotate(ctx context.Context, id string) (preparedCredentialOperation, error) {
	if journal == nil || journal.db == nil || journal.cipher == nil || journal.controlBearer == nil || journal.now == nil || !validExactExternalIdentity(id) {
		return preparedCredentialOperation{}, ErrInvalid
	}
	tx, err := journal.db.Begin(ctx)
	if err != nil {
		return preparedCredentialOperation{}, ErrUnavailable
	}
	defer tx.Rollback(ctx)
	installation, err := readInstallationByIdentity(ctx, tx, id, true)
	if err != nil {
		return preparedCredentialOperation{}, err
	}
	if installation.DesiredState != DesiredActive || installation.ObservedState == ObservedDisconnected {
		return preparedCredentialOperation{}, ErrConflict
	}
	binding, err := readControlBinding(ctx, tx, id, true)
	if err != nil {
		return preparedCredentialOperation{}, err
	}
	if binding.invitationTokenEnc != "" || binding.redeemResultSHA256 == "" {
		return preparedCredentialOperation{}, ErrConflict
	}
	scope, err := readCredentialScopeTx(ctx, tx, installation, binding)
	if err != nil {
		return preparedCredentialOperation{}, err
	}
	if pending, err := journal.readPendingCredential(ctx, tx, id); err == nil {
		return pending, nil
	} else if err != pgx.ErrNoRows {
		return preparedCredentialOperation{}, err
	}
	now := journal.now().UTC().Truncate(time.Microsecond)
	if !binding.controlTokenExpiresAt.Valid || !binding.controlTokenExpiresAt.Time.After(now) || binding.controlTokenExpiresAt.Time.Sub(now) > credentialRotationWindow {
		return preparedCredentialOperation{}, ErrConflict
	}
	old, err := journal.cipher.open(connectorControlSecret, id, binding.controlTokenEnc)
	if err != nil || binding.controlTokenSHA256 != hashBytes([]byte(old)) {
		return preparedCredentialOperation{}, ErrConflict
	}
	replacement, err := journal.controlBearer()
	if err != nil || !validConnectorControlBearer(replacement) || replacement == old {
		return preparedCredentialOperation{}, ErrUnavailable
	}
	opID, err := randomCredentialID("xcr_")
	if err != nil {
		return preparedCredentialOperation{}, err
	}
	request := CredentialRequest{SchemaVersion: CredentialRequestSchema, Kind: "rotate", InstallationID: id, DeploymentID: installation.XDeploymentID, ProjectID: binding.controlProjectID, FinalEventScope: []string{installation.EventScope}, SourceBindingSHA256: installation.SourceBindingSHA256, XInsightOrigin: scope.XIOrigin, UniRouterOrigin: scope.UROrigin, OperationID: opID, PreviousTokenSHA256: binding.controlTokenSHA256, ReplacementToken: replacement}
	canonical, err := request.CanonicalJSON()
	if err != nil {
		return preparedCredentialOperation{}, err
	}
	requestEnc, err := journal.cipher.seal(connectorCredentialRequestSecret, id, string(canonical))
	if err != nil {
		return preparedCredentialOperation{}, err
	}
	replacementEnc, err := journal.cipher.seal(connectorControlSecret, id, replacement)
	if err != nil {
		return preparedCredentialOperation{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO xinsight_connector_credential_operations
(operation_id,installation_id,org_id,project_id,kind,previous_token_sha256,replacement_token_enc,replacement_token_sha256,request_enc,request_sha256)
VALUES($1,$2,$3,$4,'rotate',$5,$6,$7,$8,$9)`, opID, id, installation.OrgID, installation.ProjectID, request.PreviousTokenSHA256, replacementEnc, hashBytes([]byte(replacement)), requestEnc, hashBytes(canonical))
	if err != nil {
		return preparedCredentialOperation{}, ErrConflict
	}
	if err := commitConnectorTx(ctx, tx, "prepare control credential rotation"); err != nil {
		return preparedCredentialOperation{}, err
	}
	return preparedCredentialOperation{request: request, canonical: canonical, requestSHA256: hashBytes(canonical), replacement: replacement, state: "pending"}, nil
}

func validateCredentialReceipt(op preparedCredentialOperation, receipt CredentialReceipt) error {
	r := op.request
	if receipt.InstallationID != r.InstallationID || receipt.DeploymentID != r.DeploymentID || receipt.ProjectID != r.ProjectID ||
		len(receipt.FinalEventScope) != 1 || receipt.FinalEventScope[0] != r.FinalEventScope[0] || receipt.SourceBindingSHA256 != r.SourceBindingSHA256 ||
		receipt.XInsightOrigin != r.XInsightOrigin || receipt.UniRouterOrigin != r.UniRouterOrigin || receipt.OperationID != r.OperationID ||
		receipt.RequestSHA256 != op.requestSHA256 || receipt.PreviousTokenSHA256 != r.PreviousTokenSHA256 ||
		receipt.ReplacementTokenSHA256 != hashBytes([]byte(op.replacement)) {
		return ErrConflict
	}
	return nil
}

func (journal *PostgresPhaseJournal) RecordCredentialReceipt(ctx context.Context, op preparedCredentialOperation, receipt CredentialReceipt) error {
	if validateCredentialReceipt(op, receipt) != nil {
		return ErrConflict
	}
	canonical, err := receipt.CanonicalJSON()
	if err != nil {
		return err
	}
	expires, _ := time.Parse(time.RFC3339Nano, receipt.ReplacementExpiresAt)
	tx, err := journal.db.Begin(ctx)
	if err != nil {
		return ErrUnavailable
	}
	defer tx.Rollback(ctx)
	installation, err := readInstallationByIdentity(ctx, tx, op.request.InstallationID, true)
	if err != nil || installation.DesiredState != DesiredActive || installation.ObservedState == ObservedDisconnected {
		return ErrConflict
	}
	binding, err := readControlBinding(ctx, tx, installation.InstallationID, true)
	if err != nil {
		return err
	}
	scope, err := readCredentialScopeTx(ctx, tx, installation, binding)
	if err != nil || scope.XIOrigin != op.request.XInsightOrigin || scope.UROrigin != op.request.UniRouterOrigin ||
		installation.SourceBindingSHA256 != op.request.SourceBindingSHA256 {
		return ErrConflict
	}
	stored, err := journal.readPendingCredential(ctx, tx, installation.InstallationID)
	if err != nil || stored.request.OperationID != op.request.OperationID || stored.requestSHA256 != op.requestSHA256 ||
		stored.request.PreviousTokenSHA256 != op.request.PreviousTokenSHA256 {
		return ErrConflict
	}
	if stored.receipt != nil && (stored.receipt.ControlIdentityID != receipt.ControlIdentityID || stored.receipt.ReplacementExpiresAt != receipt.ReplacementExpiresAt || stored.receipt.State == "committed" && receipt.State != "committed") {
		return ErrConflict
	}
	state := "pending"
	if receipt.State == "committed" {
		state = "committed"
	}
	tag, err := tx.Exec(ctx, `UPDATE xinsight_connector_credential_operations SET receipt_canonical=$2,receipt_sha256=$3,
 replacement_expires_at=$4,control_identity_id=$5,state=$6,updated_at=$7 WHERE operation_id=$1 AND state IN ('pending','committed')`,
		receipt.OperationID, canonical, hashBytes(canonical), expires, receipt.ControlIdentityID, state, journal.now().UTC())
	if err != nil || tag.RowsAffected() != 1 {
		return ErrConflict
	}
	return commitConnectorTx(ctx, tx, "record control credential receipt")
}

func (journal *PostgresPhaseJournal) PromoteCredential(ctx context.Context, id, operationID string) error {
	if journal == nil || !credentialOperationPattern.MatchString(operationID) {
		return ErrInvalid
	}
	tx, err := journal.db.Begin(ctx)
	if err != nil {
		return ErrUnavailable
	}
	defer tx.Rollback(ctx)
	installation, err := readInstallationByIdentity(ctx, tx, id, true)
	if err != nil || installation.DesiredState != DesiredActive || installation.ObservedState == ObservedDisconnected {
		return ErrConflict
	}
	binding, err := readControlBinding(ctx, tx, id, true)
	if err != nil {
		return err
	}
	if _, err = readCredentialScopeTx(ctx, tx, installation, binding); err != nil {
		return err
	}
	op, err := journal.readPendingCredential(ctx, tx, id)
	if err != nil || op.request.OperationID != operationID || op.state != "committed" || op.receipt == nil || op.receipt.State != "committed" {
		return ErrConflict
	}
	expires, _ := time.Parse(time.RFC3339Nano, op.receipt.ReplacementExpiresAt)
	if !expires.After(journal.now().UTC()) {
		return ErrConflict
	}
	localPredecessor := op.request.PreviousTokenSHA256
	if binding.controlTokenSHA256 != localPredecessor && op.request.Kind == "recover" {
		// A second offline recovery can follow a prior unpromoted recovery.
		// Trace only the exact durable supersession chain back to this local
		// current digest; no missing generation may be invented or skipped.
		childID := operationID
		for depth := 0; binding.controlTokenSHA256 != localPredecessor; depth++ {
			if depth >= 64 {
				return ErrConflict
			}
			var oldID, oldPrevious, oldReplacement, oldState string
			if err := tx.QueryRow(ctx, `SELECT o.operation_id,o.previous_token_sha256,o.replacement_token_sha256,o.state
FROM xinsight_connector_credential_operations n JOIN xinsight_connector_credential_operations o
 ON o.operation_id=n.superseded_operation_id
WHERE n.operation_id=$1 AND n.installation_id=$2 AND o.installation_id=$2 FOR UPDATE OF o`,
				childID, id).Scan(&oldID, &oldPrevious, &oldReplacement, &oldState); err != nil ||
				oldState != "superseded" || oldReplacement != localPredecessor || oldID == childID {
				return ErrConflict
			}
			childID, localPredecessor = oldID, oldPrevious
		}
	}
	if binding.controlTokenSHA256 != localPredecessor {
		return ErrConflict
	}
	sealed, err := journal.cipher.seal(connectorControlSecret, id, op.replacement)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE xinsight_connector_control_bindings SET control_token_enc=$2,
 control_token_sha256=$3,control_token_expires_at=$4,updated_at=$5
 WHERE installation_id=$1 AND control_token_sha256=$6 AND redeemed_origin=$7`,
		id, sealed, hashBytes([]byte(op.replacement)), expires, journal.now().UTC(), localPredecessor, op.request.XInsightOrigin)
	if err != nil || tag.RowsAffected() != 1 {
		return ErrConflict
	}
	if tag, err = tx.Exec(ctx, `UPDATE xinsight_connector_credential_operations SET state='promoted',updated_at=$2 WHERE operation_id=$1 AND state='committed'`, operationID, journal.now().UTC()); err != nil || tag.RowsAffected() != 1 {
		return ErrConflict
	}
	return commitConnectorTx(ctx, tx, "promote control credential")
}
