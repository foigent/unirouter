package xinsightconnector

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type PhaseExecutionInputV3 struct {
	ConsentActorID         int64
	Installation           Installation
	Operation              ControlOperationV3
	PredecessorOperationV3 *ControlOperationV3
	PredecessorResultV3    *ControlResultV3
	PredecessorResultV2    *ControlResultV2
	ObservedAt             time.Time
}
type PhaseTxExecutorV3 interface {
	AuthorizePhaseV3Tx(context.Context, pgx.Tx, Installation) (int64, error)
	ExecutePhaseV3Tx(context.Context, PhaseProductTx, PhaseExecutionInputV3) (ControlResultV3, error)
}

// journalIdentityV3 projects only shared storage/authorization coordinates.
// It never decodes v3 through v2 or invokes a v2 phase validator/executor.
func journalIdentityV3(o ControlOperationV3) ControlOperationV2 {
	return ControlOperationV2{OperationID: o.OperationID, InstallationID: o.InstallationID, ProjectID: o.ProjectID}
}

// ExecuteV3 reuses M61's installation lock, fence, canonical identity and
// immutable terminal record. Claim, product mutation and terminalization share
// one short transaction; a crash/error rolls everything back. No human wait or
// network call occurs under this transaction.
func (j *PostgresPhaseJournal) ExecuteV3(ctx context.Context, o ControlOperationV3, executor PhaseTxExecutorV3) ([]byte, string, error) {
	if j == nil || j.db == nil || j.now == nil || j.token == nil || executor == nil {
		return nil, "", ErrInvalid
	}
	canonical, e := o.CanonicalJSON()
	if e != nil {
		return nil, "", e
	}
	digest := hashBytes(canonical)
	tx, e := j.db.Begin(ctx)
	if e != nil {
		return nil, "", e
	}
	defer tx.Rollback(ctx)
	scope, e := readInstallationByIdentity(ctx, tx, o.InstallationID, false)
	if e != nil {
		return nil, "", e
	}
	// A known terminal is historical replay. Saved actor revocation cannot
	// turn that historical fact into a new product action.
	prior, priorErr := readPhaseRecord(ctx, tx, o.OperationID, false)
	actor := int64(0)
	if priorErr != nil && !errors.Is(priorErr, ErrNotFound) {
		return nil, "", priorErr
	}
	if len(prior.resultCanonical) == 0 {
		actor, e = executor.AuthorizePhaseV3Tx(ctx, tx, scope)
		if e != nil {
			return nil, "", e
		}
		if e = lockPhaseProductScope(ctx, tx, scope); e != nil {
			return nil, "", e
		}
	}
	installation, e := readInstallationByIdentity(ctx, tx, o.InstallationID, true)
	if e != nil {
		return nil, "", e
	}
	if installation.OrgID != scope.OrgID || installation.ProjectID != scope.ProjectID {
		return nil, "", ErrConflict
	}
	record, readErr := readPhaseRecord(ctx, tx, o.OperationID, true)
	if readErr == nil {
		if !phaseRecordMatchesOperation(record, journalIdentityV3(o), digest, canonical) {
			return nil, "", ErrConflict
		}
		if len(record.resultCanonical) > 0 {
			if _, e = j.v3Predecessor(ctx, tx, o, record.controlFence); e != nil {
				return nil, "", e
			}
			result, e := DecodeControlResultV3(record.resultCanonical)
			if e != nil || ValidateControlResultForOperationV3(o, result) != nil || record.resultSHA256 != hashBytes(record.resultCanonical) {
				return nil, "", ErrUnavailable
			}
			rebuilt, e := result.CanonicalJSON()
			if e != nil || !bytes.Equal(rebuilt, record.resultCanonical) {
				return nil, "", ErrUnavailable
			}
			if e = commitConnectorTx(ctx, tx, "v3 terminal replay"); e != nil {
				return nil, "", e
			}
			return bytes.Clone(record.resultCanonical), record.resultSHA256, nil
		}
	} else if !errors.Is(readErr, ErrNotFound) {
		return nil, "", readErr
	}
	binding, e := readControlBinding(ctx, tx, o.InstallationID, true)
	if e != nil {
		return nil, "", e
	}
	if e = j.validatePhaseExecutionAuthority(installation, binding, journalIdentityV3(o)); e != nil {
		return nil, "", e
	}
	predecessor, e := j.v3Predecessor(ctx, tx, o, installation.ControlFence)
	if e != nil {
		return nil, "", e
	}
	if errors.Is(readErr, ErrNotFound) {
		if e = j.validateNewV3(ctx, tx, o, installation.ControlFence); e != nil {
			return nil, "", e
		}
	}
	now := j.now().UTC().Truncate(time.Microsecond)
	claim, e := j.token()
	if e != nil || !validPhaseClaimToken(claim) {
		return nil, "", ErrUnavailable
	}
	if readErr == nil {
		if record.controlFence != installation.ControlFence || (record.claimExpiresAt.Valid && record.claimExpiresAt.Time.After(now)) {
			return nil, "", ErrConflict
		}
		tag, e := tx.Exec(ctx, `UPDATE xinsight_connector_phase_operations SET claim_fence=claim_fence+1,claim_token=$2,claim_expires_at=$3,updated_at=$4 WHERE operation_id=$1 AND result_canonical IS NULL`, o.OperationID, claim, now.Add(j.claimTTL), now)
		if e != nil || tag.RowsAffected() != 1 {
			return nil, "", ErrConflict
		}
	} else {
		_, e = tx.Exec(ctx, `INSERT INTO xinsight_connector_phase_operations(operation_id,installation_id,org_id,project_id,request_sha256,request_canonical,control_fence,claim_fence,claim_token,claim_expires_at,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,1,$8,$9,$10,$10)`, o.OperationID, o.InstallationID, installation.OrgID, installation.ProjectID, digest, canonical, installation.ControlFence, claim, now.Add(j.claimTTL), now)
		if e != nil {
			return nil, "", classifyWriteError(e)
		}
	}
	record, e = readPhaseRecord(ctx, tx, o.OperationID, true)
	if e != nil {
		return nil, "", e
	}
	input := PhaseExecutionInputV3{ConsentActorID: actor, Installation: clonePhaseExecutionInstallation(installation), Operation: o, ObservedAt: now}
	input.PredecessorOperationV3 = predecessor.PredecessorOperationV3
	input.PredecessorResultV3 = predecessor.PredecessorResultV3
	input.PredecessorResultV2 = predecessor.PredecessorResultV2
	result, e := executor.ExecutePhaseV3Tx(ctx, tx, input)
	if e != nil {
		return nil, "", e
	}
	if e = ValidateControlResultForOperationV3(o, result); e != nil {
		return nil, "", e
	}
	if v3Time(result.ObservedAt).Before(now) || v3Time(result.ObservedAt).After(j.now().UTC().Add(time.Minute)) {
		return nil, "", ErrInvalid
	}
	terminal, e := result.CanonicalJSON()
	if e != nil {
		return nil, "", e
	}
	terminalDigest := hashBytes(terminal)
	if e = completePendingPhaseTx(ctx, tx, installation, record, journalIdentityV3(o), claim, terminal, terminalDigest, j.now().UTC().Truncate(time.Microsecond)); e != nil {
		return nil, "", e
	}
	if e = commitConnectorTx(ctx, tx, "v3 product and terminal"); e != nil {
		return nil, "", e
	}
	return terminal, terminalDigest, nil
}

func (j *PostgresPhaseJournal) v3Predecessor(ctx context.Context, tx pgx.Tx, o ControlOperationV3, fence int64) (PhaseExecutionInputV3, error) {
	var input PhaseExecutionInputV3
	if o.PredecessorOperationID == "" {
		return input, ValidateControlPredecessorV3(o, nil)
	}
	p, e := readPhaseRecord(ctx, tx, o.PredecessorOperationID, true)
	if e != nil || p.installationID != o.InstallationID || p.controlFence != fence || p.requestSHA256 != hashBytes(p.requestCanonical) || p.resultSHA256 != hashBytes(p.resultCanonical) || len(p.resultCanonical) == 0 {
		return input, ErrConflict
	}
	version, e := ControlSchemaVersion(p.requestCanonical)
	if e != nil {
		return input, e
	}
	switch version {
	case ControlOperationV2Schema:
		po, e := DecodeControlOperationV2(p.requestCanonical)
		if e != nil {
			return input, e
		}
		pr, e := decodeAgentCanonicalResult(p.resultCanonical)
		if e != nil || ValidateControlResultForOperationV2(po, pr) != nil || po.ProjectID != o.ProjectID || po.InstallationID != o.InstallationID || po.OperationID != o.PredecessorOperationID {
			return input, ErrConflict
		}
		b, e := po.CanonicalJSON()
		if e != nil || !bytes.Equal(b, p.requestCanonical) {
			return input, ErrConflict
		}
		input.PredecessorResultV2 = &pr
	case ControlOperationV3Schema:
		po, e := DecodeControlOperationV3(p.requestCanonical)
		if e != nil {
			return input, e
		}
		pr, e := DecodeControlResultV3(p.resultCanonical)
		if e != nil || ValidateControlResultForOperationV3(po, pr) != nil || po.ProjectID != o.ProjectID || po.InstallationID != o.InstallationID || po.OperationID != o.PredecessorOperationID {
			return input, ErrConflict
		}
		b, e := po.CanonicalJSON()
		if e != nil || !bytes.Equal(b, p.requestCanonical) {
			return input, ErrConflict
		}
		b, e = pr.CanonicalJSON()
		if e != nil || !bytes.Equal(b, p.resultCanonical) {
			return input, ErrConflict
		}
		input.PredecessorOperationV3 = &po
		input.PredecessorResultV3 = &pr
	default:
		return input, ErrInvalid
	}
	return input, ValidateControlPredecessorV3(o, p.resultCanonical)
}
func (j *PostgresPhaseJournal) validateNewV3(ctx context.Context, tx pgx.Tx, o ControlOperationV3, fence int64) error {
	rows, e := tx.Query(ctx, `SELECT request_canonical FROM xinsight_connector_phase_operations WHERE installation_id=$1 AND control_fence=$2 FOR UPDATE`, o.InstallationID, fence)
	if e != nil {
		return e
	}
	defer rows.Close()
	for rows.Next() {
		var b []byte
		if e = rows.Scan(&b); e != nil {
			return e
		}
		version, e := ControlSchemaVersion(b)
		if e != nil {
			return e
		}
		switch version {
		case ControlOperationV2Schema:
			p, e := DecodeControlOperationV2(b)
			if e != nil || p.ProjectID != o.ProjectID {
				return ErrConflict
			}
		case ControlOperationV3Schema:
			p, e := DecodeControlOperationV3(b)
			if e != nil || p.ProjectID != o.ProjectID {
				return ErrConflict
			}
			if p.Kind == o.Kind && p.Phase == o.Phase {
				if o.Kind != ControlKindPairMachine {
					return ErrConflict
				}
				a := p.Request.(DeliverMachineRequest)
				q := o.Request.(DeliverMachineRequest)
				if a.Registration.Purpose == q.Registration.Purpose {
					return ErrConflict
				}
			}
		default:
			return ErrInvalid
		}
	}
	return rows.Err()
}
func (j *PostgresPhaseJournal) MarkAcknowledgedV3(ctx context.Context, o ControlOperationV3, digest string) error {
	canonical, e := o.CanonicalJSON()
	if e != nil || !hashPattern.MatchString(digest) {
		return ErrInvalid
	}
	tx, e := j.db.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	r, e := readPhaseRecord(ctx, tx, o.OperationID, true)
	if e != nil {
		return e
	}
	if !phaseRecordMatchesOperation(r, journalIdentityV3(o), hashBytes(canonical), canonical) || r.resultSHA256 != digest || hashBytes(r.resultCanonical) != digest {
		return ErrConflict
	}
	decoded, e := DecodeControlResultV3(r.resultCanonical)
	if e != nil || ValidateControlResultForOperationV3(o, decoded) != nil {
		return ErrConflict
	}
	if !r.acknowledgedAt.Valid {
		tag, e := tx.Exec(ctx, `UPDATE xinsight_connector_phase_operations SET acknowledged_at=$2,updated_at=$2 WHERE operation_id=$1 AND acknowledged_at IS NULL`, o.OperationID, j.now().UTC())
		if e != nil || tag.RowsAffected() != 1 {
			return fmt.Errorf("%w: v3 acknowledgement", ErrConflict)
		}
	}
	return commitConnectorTx(ctx, tx, "v3 acknowledgement")
}
