package xinsightconnector

import (
	"bytes"
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/db"
)

// PostgresPhaseJournal stores X-owned operation/result bytes opaquely. The
// typed v2 consumer validates phase semantics before any durable mutation;
// this store adds tenant binding, installation-first serialization and exact
// terminal replay without publishing another wire contract.
type PostgresPhaseJournal struct {
	db       db.DBQuerier
	cipher   *connectorSecretCipher
	now      func() time.Time
	token    func() (string, error)
	claimTTL time.Duration
}

func NewPostgresPhaseJournal(database db.DBQuerier, encryptionRoot []byte) (*PostgresPhaseJournal, error) {
	if database == nil {
		return nil, fmt.Errorf("%w: phase journal database is required", ErrInvalid)
	}
	cipher, err := newConnectorSecretCipher(encryptionRoot)
	if err != nil {
		return nil, err
	}
	return &PostgresPhaseJournal{
		db: database, cipher: cipher, now: time.Now,
		token: randomLeaseToken, claimTTL: 2 * time.Minute,
	}, nil
}

// StageInvitation persists only a purpose-encrypted invitation after proving
// it matches the local consent digest. Exact replay is a no-op.
func (journal *PostgresPhaseJournal) StageInvitation(
	ctx context.Context,
	installationID, invitationToken string,
) error {
	if journal == nil || journal.db == nil || journal.cipher == nil ||
		!validExactExternalIdentity(installationID) || !validConnectorSecret(invitationToken) {
		return ErrInvalid
	}
	sealed, err := journal.cipher.seal(connectorInvitationSecret, installationID, invitationToken)
	if err != nil {
		return err
	}
	tx, err := journal.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("%w: begin invitation binding: %v", ErrUnavailable, err)
	}
	defer tx.Rollback(ctx)
	installation, err := readInstallationByIdentity(ctx, tx, installationID, true)
	if err != nil {
		return err
	}
	if installation.InvitationSHA256 != hashBytes([]byte(invitationToken)) ||
		installation.ObservedState == ObservedDisconnected {
		return ErrConflict
	}
	existing, err := readControlBinding(ctx, tx, installationID, true)
	if err == nil {
		if existing.controlTokenEnc != "" || existing.invitationTokenEnc == "" {
			return ErrConflict
		}
		plaintext, openErr := journal.cipher.open(
			connectorInvitationSecret, installationID, existing.invitationTokenEnc,
		)
		if openErr != nil || !exactSecretEqual(plaintext, invitationToken) {
			return ErrConflict
		}
		return commitConnectorTx(ctx, tx, "invitation replay")
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	now := journal.now().UTC().Truncate(time.Microsecond)
	_, err = tx.Exec(ctx, `
INSERT INTO xinsight_connector_control_bindings (
    installation_id,org_id,project_id,invitation_token_enc,created_at,updated_at)
VALUES ($1,$2,$3,$4,$5,$5)`,
		installation.InstallationID, installation.OrgID, installation.ProjectID, sealed, now,
	)
	if err != nil {
		return classifyWriteError(err)
	}
	return commitConnectorTx(ctx, tx, "stage invitation")
}

// PromoteControlToken atomically clears the invitation ciphertext and stores a
// separately-derived control ciphertext. Exact lost-response replay succeeds;
// a receiver bearer is not accepted by this API.
func (journal *PostgresPhaseJournal) PromoteControlToken(
	ctx context.Context,
	installationID, invitationToken, controlToken, controlProjectID string,
) error {
	if journal == nil || journal.db == nil || journal.cipher == nil ||
		!validExactExternalIdentity(installationID) || !validConnectorSecret(invitationToken) ||
		!validConnectorSecret(controlToken) || !validControlText(controlProjectID, 128) ||
		exactSecretEqual(invitationToken, controlToken) {
		return ErrInvalid
	}
	sealedControl, err := journal.cipher.seal(connectorControlSecret, installationID, controlToken)
	if err != nil {
		return err
	}
	tx, err := journal.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("%w: begin control-token promotion: %v", ErrUnavailable, err)
	}
	defer tx.Rollback(ctx)
	installation, err := readInstallationByIdentity(ctx, tx, installationID, true)
	if err != nil {
		return err
	}
	if installation.InvitationSHA256 != hashBytes([]byte(invitationToken)) ||
		installation.ObservedState == ObservedDisconnected {
		return ErrConflict
	}
	binding, err := readControlBinding(ctx, tx, installationID, true)
	if err != nil {
		return err
	}
	if binding.controlTokenEnc != "" {
		if binding.controlProjectID != controlProjectID {
			return ErrConflict
		}
		plaintext, openErr := journal.cipher.open(
			connectorControlSecret, installationID, binding.controlTokenEnc,
		)
		if openErr != nil || !exactSecretEqual(plaintext, controlToken) ||
			binding.controlTokenSHA256 != hashBytes([]byte(plaintext)) {
			return ErrConflict
		}
		return commitConnectorTx(ctx, tx, "control-token replay")
	}
	plaintext, err := journal.cipher.open(
		connectorInvitationSecret, installationID, binding.invitationTokenEnc,
	)
	if err != nil || !exactSecretEqual(plaintext, invitationToken) {
		return ErrConflict
	}
	now := journal.now().UTC().Truncate(time.Microsecond)
	tag, err := tx.Exec(ctx, `
UPDATE xinsight_connector_control_bindings
SET invitation_token_enc=NULL, control_token_enc=$2, control_token_sha256=$3,
    control_project_id=$4, updated_at=$5
WHERE installation_id=$1 AND invitation_token_enc IS NOT NULL AND control_token_enc IS NULL`,
		installationID, sealedControl, hashBytes([]byte(controlToken)), controlProjectID, now,
	)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("%w: promote control token", ErrConflict)
	}
	return commitConnectorTx(ctx, tx, "promote control token")
}

// controlToken is package-private so plaintext authority can only be wired by
// the future fixed-origin, installation-bound counterpart transport.
func (journal *PostgresPhaseJournal) controlToken(ctx context.Context, installationID string) (string, error) {
	if journal == nil || journal.db == nil || journal.cipher == nil || !validExactExternalIdentity(installationID) {
		return "", ErrInvalid
	}
	binding, err := readControlBinding(ctx, journal.db, installationID, false)
	if err != nil {
		return "", err
	}
	if binding.controlTokenEnc == "" {
		return "", ErrConflict
	}
	plaintext, err := journal.cipher.open(
		connectorControlSecret, installationID, binding.controlTokenEnc,
	)
	if err != nil || binding.controlTokenSHA256 != hashBytes([]byte(plaintext)) {
		return "", ErrConflict
	}
	return plaintext, nil
}

func (journal *PostgresPhaseJournal) Acquire(
	ctx context.Context,
	operation ControlOperationV2,
	requestSHA256 string,
) (PhaseJournalAcquisition, error) {
	if journal == nil || journal.db == nil || journal.cipher == nil || journal.now == nil ||
		journal.token == nil || journal.claimTTL < time.Second || journal.claimTTL > 10*time.Minute {
		return PhaseJournalAcquisition{}, ErrInvalid
	}
	canonical, err := operation.CanonicalJSON()
	if err != nil || requestSHA256 != hashBytes(canonical) {
		return PhaseJournalAcquisition{}, ErrInvalid
	}
	tx, err := journal.db.Begin(ctx)
	if err != nil {
		return PhaseJournalAcquisition{}, fmt.Errorf("%w: begin phase acquisition: %v", ErrUnavailable, err)
	}
	defer tx.Rollback(ctx)
	installation, err := readInstallationByIdentity(ctx, tx, operation.InstallationID, true)
	if err != nil {
		return PhaseJournalAcquisition{}, err
	}

	existing, err := readPhaseRecord(ctx, tx, operation.OperationID, true)
	if err == nil {
		if existing.installationID != operation.InstallationID || existing.requestSHA256 != requestSHA256 ||
			!bytes.Equal(existing.requestCanonical, canonical) {
			return PhaseJournalAcquisition{}, ErrConflict
		}
		if len(existing.resultCanonical) != 0 {
			if existing.resultSHA256 != hashBytes(existing.resultCanonical) {
				return PhaseJournalAcquisition{}, fmt.Errorf("%w: stored terminal result digest mismatch", ErrUnavailable)
			}
			predecessor, err := journal.readAndValidatePredecessor(ctx, tx, operation, existing.controlFence)
			if err != nil {
				return PhaseJournalAcquisition{}, err
			}
			return PhaseJournalAcquisition{
				PredecessorCanonicalResult: predecessor,
				TerminalCanonicalResult:    bytes.Clone(existing.resultCanonical),
			}, commitConnectorTx(ctx, tx, "terminal phase replay")
		}
		binding, err := readControlBinding(ctx, tx, operation.InstallationID, true)
		if err != nil {
			return PhaseJournalAcquisition{}, err
		}
		if err := journal.validatePhaseExecutionAuthority(installation, binding, operation); err != nil ||
			existing.controlFence != installation.ControlFence {
			return PhaseJournalAcquisition{}, ErrConflict
		}
		predecessor, err := journal.readAndValidatePredecessor(ctx, tx, operation, installation.ControlFence)
		if err != nil {
			return PhaseJournalAcquisition{}, err
		}
		now := journal.now().UTC().Truncate(time.Microsecond)
		if existing.claimExpiresAt.Valid && existing.claimExpiresAt.Time.After(now) {
			return PhaseJournalAcquisition{}, ErrConflict
		}
		claimToken, err := journal.token()
		if err != nil || !validPhaseClaimToken(claimToken) {
			return PhaseJournalAcquisition{}, fmt.Errorf("%w: create phase claim", ErrUnavailable)
		}
		claimExpiresAt := now.Add(journal.claimTTL)
		tag, err := tx.Exec(ctx, `
UPDATE xinsight_connector_phase_operations
SET claim_fence=claim_fence+1, claim_token=$2, claim_expires_at=$3, updated_at=$4
WHERE operation_id=$1 AND result_sha256 IS NULL AND result_canonical IS NULL`,
			operation.OperationID, claimToken, claimExpiresAt, now,
		)
		if err != nil || tag.RowsAffected() != 1 {
			return PhaseJournalAcquisition{}, fmt.Errorf("%w: reclaim phase operation", ErrConflict)
		}
		return PhaseJournalAcquisition{
			ClaimToken:                 claimToken,
			PredecessorCanonicalResult: predecessor,
		}, commitConnectorTx(ctx, tx, "pending phase recovery")
	}
	if !errors.Is(err, ErrNotFound) {
		return PhaseJournalAcquisition{}, err
	}
	binding, err := readControlBinding(ctx, tx, operation.InstallationID, true)
	if err != nil {
		return PhaseJournalAcquisition{}, err
	}
	if err := journal.validatePhaseExecutionAuthority(installation, binding, operation); err != nil {
		return PhaseJournalAcquisition{}, err
	}

	predecessor, err := journal.validateNewPhase(ctx, tx, operation, installation.ControlFence)
	if err != nil {
		return PhaseJournalAcquisition{}, err
	}
	now := journal.now().UTC().Truncate(time.Microsecond)
	claimToken, err := journal.token()
	if err != nil || !validPhaseClaimToken(claimToken) {
		return PhaseJournalAcquisition{}, fmt.Errorf("%w: create phase claim", ErrUnavailable)
	}
	claimExpiresAt := now.Add(journal.claimTTL)
	_, err = tx.Exec(ctx, `
INSERT INTO xinsight_connector_phase_operations (
    operation_id,installation_id,org_id,project_id,
    request_sha256,request_canonical,control_fence,claim_fence,
    claim_token,claim_expires_at,created_at,updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,1,$8,$9,$10,$10)`,
		operation.OperationID, installation.InstallationID, installation.OrgID,
		installation.ProjectID, requestSHA256, canonical, installation.ControlFence,
		claimToken, claimExpiresAt, now,
	)
	if err != nil {
		return PhaseJournalAcquisition{}, classifyWriteError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return PhaseJournalAcquisition{}, fmt.Errorf("%w: commit phase acquisition: %v", ErrUnavailable, err)
	}
	return PhaseJournalAcquisition{
		ClaimToken:                 claimToken,
		PredecessorCanonicalResult: predecessor,
	}, nil
}

func (journal *PostgresPhaseJournal) Complete(
	ctx context.Context,
	operation ControlOperationV2,
	requestSHA256, claimToken string,
	canonicalResult []byte,
	resultSHA256 string,
) error {
	if journal == nil || journal.db == nil || journal.now == nil {
		return ErrInvalid
	}
	canonicalOperation, err := operation.CanonicalJSON()
	if err != nil || requestSHA256 != hashBytes(canonicalOperation) ||
		!validPhaseClaimToken(claimToken) ||
		resultSHA256 != hashBytes(canonicalResult) {
		return ErrInvalid
	}
	result, err := decodeAgentCanonicalResult(canonicalResult)
	if err != nil || ValidateControlResultForOperationV2(operation, result) != nil {
		return ErrInvalid
	}
	tx, err := journal.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("%w: begin phase completion: %v", ErrUnavailable, err)
	}
	defer tx.Rollback(ctx)
	installation, err := readInstallationByIdentity(ctx, tx, operation.InstallationID, true)
	if err != nil {
		return err
	}
	record, err := readPhaseRecord(ctx, tx, operation.OperationID, true)
	if err != nil {
		return err
	}
	if record.installationID != operation.InstallationID || record.requestSHA256 != requestSHA256 ||
		!bytes.Equal(record.requestCanonical, canonicalOperation) {
		return ErrConflict
	}
	if len(record.resultCanonical) != 0 {
		if record.resultSHA256 != resultSHA256 || record.resultSHA256 != hashBytes(record.resultCanonical) ||
			!bytes.Equal(record.resultCanonical, canonicalResult) {
			return ErrConflict
		}
		return commitConnectorTx(ctx, tx, "phase completion replay")
	}
	binding, err := readControlBinding(ctx, tx, operation.InstallationID, true)
	if err != nil {
		return err
	}
	if err := journal.validatePhaseExecutionAuthority(installation, binding, operation); err != nil {
		return err
	}
	now := journal.now().UTC().Truncate(time.Microsecond)
	if record.controlFence != installation.ControlFence || record.claimToken != claimToken ||
		!record.claimExpiresAt.Valid || !record.claimExpiresAt.Time.After(now) {
		return ErrConflict
	}
	tag, err := tx.Exec(ctx, `
UPDATE xinsight_connector_phase_operations
SET result_sha256=$2,result_canonical=$3,claim_token=NULL,claim_expires_at=NULL,updated_at=$4
WHERE operation_id=$1 AND result_sha256 IS NULL AND result_canonical IS NULL
  AND control_fence=$5 AND claim_token=$6 AND claim_expires_at>$4`,
		operation.OperationID, resultSHA256, canonicalResult, now,
		record.controlFence, claimToken,
	)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("%w: complete phase", ErrConflict)
	}
	return commitConnectorTx(ctx, tx, "complete phase")
}

func (journal *PostgresPhaseJournal) MarkAcknowledged(
	ctx context.Context,
	operation ControlOperationV2,
	requestSHA256, resultSHA256 string,
) error {
	if journal == nil || journal.db == nil || journal.now == nil {
		return ErrInvalid
	}
	canonicalOperation, err := operation.CanonicalJSON()
	if err != nil || requestSHA256 != hashBytes(canonicalOperation) ||
		!hashPattern.MatchString(resultSHA256) {
		return ErrInvalid
	}
	tx, err := journal.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("%w: begin phase acknowledgement: %v", ErrUnavailable, err)
	}
	defer tx.Rollback(ctx)
	record, err := readPhaseRecord(ctx, tx, operation.OperationID, true)
	if err != nil {
		return err
	}
	if record.installationID != operation.InstallationID || record.requestSHA256 != requestSHA256 ||
		!bytes.Equal(record.requestCanonical, canonicalOperation) ||
		record.resultSHA256 != resultSHA256 || record.resultSHA256 != hashBytes(record.resultCanonical) ||
		len(record.resultCanonical) == 0 {
		return ErrConflict
	}
	if record.acknowledgedAt.Valid {
		return commitConnectorTx(ctx, tx, "phase acknowledgement replay")
	}
	now := journal.now().UTC().Truncate(time.Microsecond)
	tag, err := tx.Exec(ctx, `
UPDATE xinsight_connector_phase_operations SET acknowledged_at=$2,updated_at=$2
WHERE operation_id=$1 AND acknowledged_at IS NULL`, operation.OperationID, now)
	if err != nil {
		return fmt.Errorf("%w: mark phase acknowledged: %v", ErrUnavailable, err)
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	return commitConnectorTx(ctx, tx, "mark phase acknowledged")
}

func (journal *PostgresPhaseJournal) validateNewPhase(
	ctx context.Context,
	tx pgx.Tx,
	operation ControlOperationV2,
	controlFence int64,
) ([]byte, error) {
	rows, err := tx.Query(ctx, `
SELECT request_canonical,control_fence,result_sha256,result_canonical
FROM xinsight_connector_phase_operations
WHERE installation_id=$1
ORDER BY created_at,operation_id
FOR UPDATE`, operation.InstallationID)
	if err != nil {
		return nil, fmt.Errorf("%w: read phase history: %v", ErrUnavailable, err)
	}
	defer rows.Close()
	var predecessor []byte
	var phaseSeen bool
	var projectID string
	for rows.Next() {
		var rawOperation, rawResult []byte
		var storedControlFence int64
		var resultSHA sql.NullString
		if err := rows.Scan(&rawOperation, &storedControlFence, &resultSHA, &rawResult); err != nil {
			return nil, fmt.Errorf("%w: scan phase history: %v", ErrUnavailable, err)
		}
		if storedControlFence != controlFence {
			continue
		}
		storedOperation, err := DecodeControlOperationV2(rawOperation)
		if err != nil {
			return nil, fmt.Errorf("%w: stored phase request is invalid", ErrUnavailable)
		}
		if projectID == "" {
			projectID = storedOperation.ProjectID
		}
		if storedOperation.ProjectID != projectID || storedOperation.ProjectID != operation.ProjectID {
			return nil, ErrConflict
		}
		if storedOperation.Phase == operation.Phase {
			phaseSeen = true
		}
		if storedOperation.OperationID == operation.PredecessorOperationID {
			if !resultSHA.Valid || resultSHA.String != hashBytes(rawResult) {
				return nil, fmt.Errorf("%w: stored predecessor digest mismatch", ErrUnavailable)
			}
			predecessor = bytes.Clone(rawResult)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: read phase history: %v", ErrUnavailable, err)
	}
	if phaseSeen {
		return nil, ErrConflict
	}
	predecessorResult, err := decodeAgentPredecessor(predecessor)
	if err != nil {
		return nil, err
	}
	if err := ValidateControlOperationPredecessorV2(operation, predecessorResult); err != nil {
		return nil, err
	}
	return predecessor, nil
}

func (journal *PostgresPhaseJournal) readAndValidatePredecessor(
	ctx context.Context,
	tx pgx.Tx,
	operation ControlOperationV2,
	controlFence int64,
) ([]byte, error) {
	if operation.PredecessorOperationID == "" {
		return nil, nil
	}
	record, err := readPhaseRecord(ctx, tx, operation.PredecessorOperationID, true)
	if err != nil || record.installationID != operation.InstallationID ||
		record.controlFence != controlFence || len(record.resultCanonical) == 0 {
		return nil, ErrConflict
	}
	if record.resultSHA256 != hashBytes(record.resultCanonical) {
		return nil, fmt.Errorf("%w: stored predecessor digest mismatch", ErrUnavailable)
	}
	predecessor, err := decodeAgentCanonicalResult(record.resultCanonical)
	if err != nil {
		return nil, err
	}
	if err := ValidateControlOperationPredecessorV2(operation, &predecessor); err != nil {
		return nil, err
	}
	return bytes.Clone(record.resultCanonical), nil
}

type controlBindingRecord struct {
	invitationTokenEnc string
	controlTokenEnc    string
	controlTokenSHA256 string
	controlProjectID   string
}

func readControlBinding(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}, installationID string, forUpdate bool) (controlBindingRecord, error) {
	lock := ""
	if forUpdate {
		lock = " FOR UPDATE"
	}
	var invitation, control, controlSHA, controlProject sql.NullString
	err := q.QueryRow(ctx, `
SELECT invitation_token_enc,control_token_enc,control_token_sha256,control_project_id
FROM xinsight_connector_control_bindings
WHERE installation_id=$1`+lock, installationID).Scan(
		&invitation, &control, &controlSHA, &controlProject,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return controlBindingRecord{}, ErrNotFound
	}
	if err != nil {
		return controlBindingRecord{}, fmt.Errorf("%w: read control binding: %v", ErrUnavailable, err)
	}
	return controlBindingRecord{
		invitationTokenEnc: invitation.String, controlTokenEnc: control.String,
		controlTokenSHA256: controlSHA.String, controlProjectID: controlProject.String,
	}, nil
}

type phaseRecord struct {
	installationID   string
	requestSHA256    string
	requestCanonical []byte
	controlFence     int64
	claimFence       int64
	claimToken       string
	claimExpiresAt   sql.NullTime
	resultSHA256     string
	resultCanonical  []byte
	acknowledgedAt   sql.NullTime
}

func readPhaseRecord(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}, operationID string, forUpdate bool) (phaseRecord, error) {
	lock := ""
	if forUpdate {
		lock = " FOR UPDATE"
	}
	var record phaseRecord
	var claimToken, resultSHA sql.NullString
	err := q.QueryRow(ctx, `
SELECT installation_id,request_sha256,request_canonical,
       control_fence,claim_fence,claim_token,claim_expires_at,
       result_sha256,result_canonical,acknowledged_at
FROM xinsight_connector_phase_operations
WHERE operation_id=$1`+lock, operationID).Scan(
		&record.installationID, &record.requestSHA256, &record.requestCanonical,
		&record.controlFence, &record.claimFence, &claimToken, &record.claimExpiresAt,
		&resultSHA, &record.resultCanonical, &record.acknowledgedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return phaseRecord{}, ErrNotFound
	}
	if err != nil {
		return phaseRecord{}, fmt.Errorf("%w: read phase operation: %v", ErrUnavailable, err)
	}
	record.claimToken = claimToken.String
	record.resultSHA256 = resultSHA.String
	return record, nil
}

func readInstallationByIdentity(ctx context.Context, tx pgx.Tx, installationID string, forUpdate bool) (Installation, error) {
	lock := ""
	if forUpdate {
		lock = " FOR UPDATE"
	}
	return scanInstallation(tx.QueryRow(ctx, `
SELECT installation_id, x_deployment_id, invitation_sha256, org_id, project_id,
       event_scope, source_instance, source_binding_sha256, desired_state,
       observed_state, state_revision, control_fence, bootstrap_receipt_sha256,
       last_successful_sync_at, last_error_code, last_error_at,
       consented_at, created_at, updated_at
FROM xinsight_connector_installations
WHERE installation_id=$1`+lock, installationID))
}

func validConnectorSecret(value string) bool {
	return validControlText(value, 1024)
}

func exactSecretEqual(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func validPhaseClaimToken(value string) bool {
	return len(value) == 32 && localIdentityPattern.MatchString(value)
}

func (journal *PostgresPhaseJournal) validatePhaseExecutionAuthority(
	installation Installation,
	binding controlBindingRecord,
	operation ControlOperationV2,
) error {
	if journal == nil || journal.cipher == nil ||
		installation.DesiredState != DesiredActive ||
		installation.ObservedState == ObservedDisconnected ||
		installation.EventScope != EventScopeModelCompleted ||
		binding.controlTokenEnc == "" ||
		!hashPattern.MatchString(binding.controlTokenSHA256) ||
		binding.controlProjectID != operation.ProjectID {
		return ErrConflict
	}
	plaintext, err := journal.cipher.open(
		connectorControlSecret, installation.InstallationID, binding.controlTokenEnc,
	)
	if err != nil || binding.controlTokenSHA256 != hashBytes([]byte(plaintext)) {
		return ErrConflict
	}
	return nil
}

func commitConnectorTx(ctx context.Context, tx pgx.Tx, action string) error {
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("%w: commit %s: %v", ErrUnavailable, action, err)
	}
	return nil
}
