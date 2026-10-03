package xinsightconnector

import (
	"bytes"
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/db"
)

// PhaseProductTx is the deliberately narrow transaction surface available to
// a product phase executor. Product mutations and the terminal journal result
// are committed by ExecuteClaimedPhase in the same PostgreSQL transaction.
type PhaseProductTx interface {
	Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}

// PhaseExecutionInput contains only authority that ExecuteClaimedPhase has
// re-read and validated while holding the installation and operation locks.
// A predecessor, when present, is the exact terminal operation/result from the
// same installation control fence.
type PhaseExecutionInput struct {
	Installation         Installation
	Operation            ControlOperationV2
	PredecessorOperation *ControlOperationV2
	PredecessorResult    *ControlResultV2
	ObservedAt           time.Time
}

// PhaseTxExecutor applies a UniRouter-owned product phase using the journal's
// PostgreSQL transaction. Returning an error rolls back both the product
// effect and terminalization.
type PhaseTxExecutor interface {
	ExecutePhaseTx(context.Context, PhaseProductTx, PhaseExecutionInput) (ControlResultV2, error)
}

// PostgresPhaseJournal stores X-owned operation/result bytes opaquely. The
// typed v2 consumer validates phase semantics before any durable mutation;
// this store adds tenant binding, installation-first serialization and exact
// terminal replay without publishing another wire contract.
type PostgresPhaseJournal struct {
	db            db.DBQuerier
	cipher        *connectorSecretCipher
	now           func() time.Time
	token         func() (string, error)
	controlBearer func() (string, error)
	claimTTL      time.Duration
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
		token: randomLeaseToken, controlBearer: randomConnectorControlBearer,
		claimTTL: 2 * time.Minute,
	}, nil
}

// StageInvitation persists only a purpose-encrypted invitation after proving
// it matches the local consent digest. Exact replay is a no-op.
func (journal *PostgresPhaseJournal) StageInvitation(ctx context.Context, installationID, invitationToken string) error {
	tx, err := journal.db.Begin(ctx)
	if err != nil {
		return ErrUnavailable
	}
	defer tx.Rollback(ctx)
	if err = journal.StageInvitationTx(ctx, tx, installationID, invitationToken); err != nil {
		return err
	}
	return commitConnectorTx(ctx, tx, "stage invitation")
}

// StageInvitationTx persists only the original purpose-encrypted token in the
// caller's installation transaction.
func (journal *PostgresPhaseJournal) StageInvitationTx(ctx context.Context, tx pgx.Tx, installationID, invitationToken string) error {
	if journal == nil || journal.db == nil || journal.cipher == nil ||
		!validExactExternalIdentity(installationID) || !validConnectorSecret(invitationToken) {
		return ErrInvalid
	}
	sealed, err := journal.cipher.seal(connectorInvitationSecret, installationID, invitationToken)
	if err != nil {
		return err
	}
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
		return nil
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	mode, err := readReceiverMode(ctx, tx, installationID)
	if err != nil {
		return err
	}
	var bootstrapCanonical []byte
	var bootstrapSHA any
	if mode == ReceiverModeAdoptExisting {
		bootstrap, err := readAdoptionBootstrap(ctx, tx, installation)
		if err != nil {
			return err
		}
		bootstrapCanonical, err = json.Marshal(bootstrap)
		if err != nil {
			return ErrInvalid
		}
		bootstrapSHA = hashBytes(bootstrapCanonical)
	}
	now := journal.now().UTC().Truncate(time.Microsecond)
	if mode == ReceiverModeAdoptExisting {
		_, err = tx.Exec(ctx, `INSERT INTO xinsight_connector_control_bindings(installation_id,org_id,project_id,invitation_token_enc,created_at,updated_at,receiver_adoption_bootstrap_canonical,receiver_adoption_bootstrap_sha256) VALUES($1,$2,$3,$4,$5,$5,$6,$7)`, installation.InstallationID, installation.OrgID, installation.ProjectID, sealed, now, bootstrapCanonical, bootstrapSHA)
	} else {
		_, err = tx.Exec(ctx, `INSERT INTO xinsight_connector_control_bindings(installation_id,org_id,project_id,invitation_token_enc,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$5)`, installation.InstallationID, installation.OrgID, installation.ProjectID, sealed, now)
	}
	if err != nil {
		return classifyWriteError(err)
	}
	return nil
}

// controlToken is package-private so plaintext authority can only be wired by
// the fixed-origin, installation-bound counterpart transport.
func (journal *PostgresPhaseJournal) controlToken(ctx context.Context, installationID string) (string, error) {
	if journal == nil || journal.db == nil || journal.cipher == nil || journal.now == nil ||
		!validExactExternalIdentity(installationID) {
		return "", ErrInvalid
	}
	binding, err := readControlBinding(ctx, journal.db, installationID, false)
	if err != nil {
		return "", err
	}
	if binding.invitationTokenEnc != "" || binding.controlTokenEnc == "" ||
		!binding.controlTokenExpiresAt.Valid ||
		!binding.controlTokenExpiresAt.Time.After(journal.now().UTC()) ||
		!hashPattern.MatchString(binding.redeemRequestSHA256) ||
		!hashPattern.MatchString(binding.redeemResultSHA256) {
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
	canonicalOperation, err := validatePhaseCompletionInput(
		operation, requestSHA256, claimToken, canonicalResult, resultSHA256,
	)
	if err != nil {
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
	if !phaseRecordMatchesOperation(record, operation, requestSHA256, canonicalOperation) {
		return ErrConflict
	}
	if len(record.resultCanonical) != 0 {
		storedCanonical, storedSHA, err := validateStoredTerminalResult(operation, record)
		if err != nil {
			return err
		}
		if storedSHA != resultSHA256 || !bytes.Equal(storedCanonical, canonicalResult) {
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
	if err := completePendingPhaseTx(
		ctx, tx, installation, record, operation, claimToken,
		canonicalResult, resultSHA256, now,
	); err != nil {
		return err
	}
	return commitConnectorTx(ctx, tx, "complete phase")
}

// ExecuteClaimedPhase is the production execution seam. It revalidates the
// claimed operation and current control authority, executes the product effect
// and writes the immutable terminal result in one PostgreSQL transaction. A
// caller retry after an ambiguous commit returns the already-stored terminal
// result without re-running the executor.
func (journal *PostgresPhaseJournal) ExecuteClaimedPhase(
	ctx context.Context,
	operation ControlOperationV2,
	requestSHA256, claimToken string,
	executor PhaseTxExecutor,
) ([]byte, string, error) {
	if journal == nil || journal.db == nil || journal.cipher == nil || journal.now == nil ||
		executor == nil || !validPhaseClaimToken(claimToken) {
		return nil, "", ErrInvalid
	}
	canonicalOperation, err := operation.CanonicalJSON()
	if err != nil || requestSHA256 != hashBytes(canonicalOperation) {
		return nil, "", ErrInvalid
	}
	tx, err := journal.db.Begin(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("%w: begin transactional phase execution: %v", ErrUnavailable, err)
	}
	defer tx.Rollback(ctx)
	initialRecord, err := readPhaseRecord(ctx, tx, operation.OperationID, false)
	if err != nil {
		return nil, "", err
	}
	if !phaseRecordMatchesOperation(initialRecord, operation, requestSHA256, canonicalOperation) {
		return nil, "", ErrConflict
	}
	if len(initialRecord.resultCanonical) != 0 {
		storedCanonical, storedSHA, err := validateStoredTerminalResult(operation, initialRecord)
		if err != nil {
			return nil, "", err
		}
		if err := commitConnectorTx(ctx, tx, "transactional phase replay"); err != nil {
			return nil, "", err
		}
		return storedCanonical, storedSHA, nil
	}
	scopeInstallation, err := readInstallationByIdentity(ctx, tx, operation.InstallationID, false)
	if err != nil {
		return nil, "", err
	}
	if err := lockPhaseProductScope(ctx, tx, scopeInstallation); err != nil {
		return nil, "", err
	}
	installation, err := readInstallationByIdentity(ctx, tx, operation.InstallationID, true)
	if err != nil {
		return nil, "", err
	}
	if installation.OrgID != scopeInstallation.OrgID ||
		installation.ProjectID != scopeInstallation.ProjectID {
		return nil, "", ErrConflict
	}
	record, err := readPhaseRecord(ctx, tx, operation.OperationID, true)
	if err != nil {
		return nil, "", err
	}
	if !phaseRecordMatchesOperation(record, operation, requestSHA256, canonicalOperation) {
		return nil, "", ErrConflict
	}
	if len(record.resultCanonical) != 0 {
		storedCanonical, storedSHA, err := validateStoredTerminalResult(operation, record)
		if err != nil {
			return nil, "", err
		}
		if err := commitConnectorTx(ctx, tx, "transactional phase replay"); err != nil {
			return nil, "", err
		}
		return storedCanonical, storedSHA, nil
	}
	binding, err := readControlBinding(ctx, tx, operation.InstallationID, true)
	if err != nil {
		return nil, "", err
	}
	if err := journal.validatePhaseExecutionAuthority(installation, binding, operation); err != nil {
		return nil, "", err
	}
	startedAt := journal.now().UTC().Truncate(time.Microsecond)
	if err := validatePendingPhaseClaim(installation, record, claimToken, startedAt); err != nil {
		return nil, "", err
	}
	predecessorOperation, predecessorResult, _, err := journal.readAndValidatePredecessorState(
		ctx, tx, operation, installation.ControlFence,
	)
	if err != nil {
		return nil, "", err
	}
	executionOperation := operation
	executionOperation.EventScope = append([]string(nil), operation.EventScope...)
	result, err := executor.ExecutePhaseTx(ctx, tx, PhaseExecutionInput{
		Installation:         clonePhaseExecutionInstallation(installation),
		Operation:            executionOperation,
		PredecessorOperation: predecessorOperation,
		PredecessorResult:    predecessorResult,
		ObservedAt:           startedAt,
	})
	if err != nil {
		return nil, "", err
	}
	canonicalResult, resultSHA256, err := canonicalPhaseResultForOperation(operation, result)
	if err != nil {
		return nil, "", err
	}
	completedAt := journal.now().UTC().Truncate(time.Microsecond)
	if err := completePendingPhaseTx(
		ctx, tx, installation, record, operation, claimToken,
		canonicalResult, resultSHA256, completedAt,
	); err != nil {
		return nil, "", err
	}
	if err := commitConnectorTx(ctx, tx, "transactional phase execution"); err != nil {
		return nil, "", err
	}
	return bytes.Clone(canonicalResult), resultSHA256, nil
}

func lockPhaseProductScope(
	ctx context.Context,
	tx pgx.Tx,
	installation Installation,
) error {
	var organizationStatus, projectStatus string
	if err := tx.QueryRow(ctx, `
SELECT organization.status,project.status
FROM orgs AS organization
JOIN projects AS project ON project.org_id=organization.id
WHERE organization.id=$1 AND project.id=$2
FOR SHARE OF organization,project`, installation.OrgID, installation.ProjectID).Scan(
		&organizationStatus, &projectStatus,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrConflict
		}
		return fmt.Errorf("%w: lock connector product scope: %v", ErrUnavailable, err)
	}
	if organizationStatus != "active" || projectStatus != "active" {
		return ErrConflict
	}
	return nil
}

func validatePhaseCompletionInput(
	operation ControlOperationV2,
	requestSHA256, claimToken string,
	canonicalResult []byte,
	resultSHA256 string,
) ([]byte, error) {
	canonicalOperation, err := operation.CanonicalJSON()
	if err != nil || requestSHA256 != hashBytes(canonicalOperation) ||
		!validPhaseClaimToken(claimToken) || resultSHA256 != hashBytes(canonicalResult) {
		return nil, ErrInvalid
	}
	result, err := decodeAgentCanonicalResult(canonicalResult)
	if err != nil || ValidateControlResultForOperationV2(operation, result) != nil {
		return nil, ErrInvalid
	}
	return canonicalOperation, nil
}

func canonicalPhaseResultForOperation(
	operation ControlOperationV2,
	result ControlResultV2,
) ([]byte, string, error) {
	if err := ValidateControlResultForOperationV2(operation, result); err != nil {
		return nil, "", ErrInvalid
	}
	canonical, err := result.CanonicalJSON()
	if err != nil {
		return nil, "", ErrInvalid
	}
	return canonical, hashBytes(canonical), nil
}

func phaseRecordMatchesOperation(
	record phaseRecord,
	operation ControlOperationV2,
	requestSHA256 string,
	canonicalOperation []byte,
) bool {
	return record.installationID == operation.InstallationID &&
		record.requestSHA256 == requestSHA256 &&
		bytes.Equal(record.requestCanonical, canonicalOperation)
}

func validateStoredTerminalResult(
	operation ControlOperationV2,
	record phaseRecord,
) ([]byte, string, error) {
	if len(record.resultCanonical) == 0 ||
		record.resultSHA256 != hashBytes(record.resultCanonical) {
		return nil, "", fmt.Errorf("%w: stored terminal result digest mismatch", ErrUnavailable)
	}
	result, err := decodeAgentCanonicalResult(record.resultCanonical)
	if err != nil || ValidateControlResultForOperationV2(operation, result) != nil {
		return nil, "", fmt.Errorf("%w: stored terminal result is invalid", ErrUnavailable)
	}
	return bytes.Clone(record.resultCanonical), record.resultSHA256, nil
}

func validatePendingPhaseClaim(
	installation Installation,
	record phaseRecord,
	claimToken string,
	now time.Time,
) error {
	if record.controlFence != installation.ControlFence || record.claimToken != claimToken ||
		!record.claimExpiresAt.Valid || !record.claimExpiresAt.Time.After(now) {
		return ErrConflict
	}
	return nil
}

func completePendingPhaseTx(
	ctx context.Context,
	tx pgx.Tx,
	installation Installation,
	record phaseRecord,
	operation ControlOperationV2,
	claimToken string,
	canonicalResult []byte,
	resultSHA256 string,
	now time.Time,
) error {
	if err := validatePendingPhaseClaim(installation, record, claimToken, now); err != nil {
		return err
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
	return nil
}

func clonePhaseExecutionInstallation(installation Installation) Installation {
	if installation.LastSuccessfulSyncAt != nil {
		value := *installation.LastSuccessfulSyncAt
		installation.LastSuccessfulSyncAt = &value
	}
	if installation.LastErrorAt != nil {
		value := *installation.LastErrorAt
		installation.LastErrorAt = &value
	}
	return installation
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
	_, _, canonicalResult, err := journal.readAndValidatePredecessorState(
		ctx, tx, operation, controlFence,
	)
	return canonicalResult, err
}

func (journal *PostgresPhaseJournal) readAndValidatePredecessorState(
	ctx context.Context,
	tx pgx.Tx,
	operation ControlOperationV2,
	controlFence int64,
) (*ControlOperationV2, *ControlResultV2, []byte, error) {
	if operation.PredecessorOperationID == "" {
		return nil, nil, nil, nil
	}
	record, err := readPhaseRecord(ctx, tx, operation.PredecessorOperationID, true)
	if err != nil || record.installationID != operation.InstallationID ||
		record.controlFence != controlFence || len(record.resultCanonical) == 0 {
		return nil, nil, nil, ErrConflict
	}
	predecessorOperation, err := DecodeControlOperationV2(record.requestCanonical)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%w: stored predecessor request is invalid", ErrUnavailable)
	}
	canonicalOperation, err := predecessorOperation.CanonicalJSON()
	if err != nil || !bytes.Equal(canonicalOperation, record.requestCanonical) ||
		record.requestSHA256 != hashBytes(record.requestCanonical) ||
		predecessorOperation.OperationID != operation.PredecessorOperationID {
		return nil, nil, nil, fmt.Errorf("%w: stored predecessor request identity mismatch", ErrUnavailable)
	}
	if record.resultSHA256 != hashBytes(record.resultCanonical) {
		return nil, nil, nil, fmt.Errorf("%w: stored predecessor digest mismatch", ErrUnavailable)
	}
	predecessor, err := decodeAgentCanonicalResult(record.resultCanonical)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := ValidateControlResultForOperationV2(predecessorOperation, predecessor); err != nil {
		return nil, nil, nil, fmt.Errorf("%w: stored predecessor result is invalid", ErrUnavailable)
	}
	if err := ValidateControlOperationPredecessorV2(operation, &predecessor); err != nil {
		return nil, nil, nil, err
	}
	predecessorOperation.EventScope = append([]string(nil), predecessorOperation.EventScope...)
	return &predecessorOperation, &predecessor, bytes.Clone(record.resultCanonical), nil
}

type controlBindingRecord struct {
	expanded                   bool
	redeemedOrigin             string
	invitationTokenEnc         string
	controlTokenEnc            string
	controlTokenSHA256         string
	controlProjectID           string
	controlTokenExpiresAt      sql.NullTime
	adoptionBootstrapCanonical []byte
	adoptionBootstrapSHA256    string
	redeemRequestSHA256        string
	redeemResultSHA256         string
}

func readControlBinding(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}, installationID string, forUpdate bool) (controlBindingRecord, error) {
	lock := ""
	if forUpdate {
		lock = " FOR UPDATE"
	}
	var expanded bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version='000124_xinsight_receiver_adoption')`).Scan(&expanded); err != nil {
		return controlBindingRecord{}, ErrUnavailable
	}
	bootstrapProjection := "NULL::bytea,''::text,''::text"
	if expanded {
		bootstrapProjection = "receiver_adoption_bootstrap_canonical,COALESCE(receiver_adoption_bootstrap_sha256,''),COALESCE(redeemed_origin,'')"
	}
	var invitation, control, controlSHA, controlProject sql.NullString
	var requestSHA, resultSHA sql.NullString
	var record controlBindingRecord
	err := q.QueryRow(ctx, `
SELECT invitation_token_enc,control_token_enc,control_token_sha256,control_project_id,
       control_token_expires_at,redeem_request_sha256,redeem_result_sha256,
 `+bootstrapProjection+`
FROM xinsight_connector_control_bindings
WHERE installation_id=$1`+lock, installationID).Scan(
		&invitation, &control, &controlSHA, &controlProject,
		&record.controlTokenExpiresAt, &requestSHA, &resultSHA, &record.adoptionBootstrapCanonical, &record.adoptionBootstrapSHA256, &record.redeemedOrigin,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return controlBindingRecord{}, ErrNotFound
	}
	if err != nil {
		return controlBindingRecord{}, fmt.Errorf("%w: read control binding: %v", ErrUnavailable, err)
	}
	record.invitationTokenEnc = invitation.String
	record.controlTokenEnc = control.String
	record.controlTokenSHA256 = controlSHA.String
	record.controlProjectID = controlProject.String
	record.redeemRequestSHA256 = requestSHA.String
	record.redeemResultSHA256 = resultSHA.String
	record.expanded = expanded
	return record, nil
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
		binding.invitationTokenEnc != "" ||
		!binding.controlTokenExpiresAt.Valid ||
		!binding.controlTokenExpiresAt.Time.After(journal.now().UTC()) ||
		!hashPattern.MatchString(binding.redeemRequestSHA256) ||
		!hashPattern.MatchString(binding.redeemResultSHA256) ||
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
