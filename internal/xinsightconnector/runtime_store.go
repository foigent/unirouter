package xinsightconnector

import (
	"context"
	"fmt"
)

const maxRuntimeInstallationBatch = 100

// RuntimeInstallationDescriptor contains only the non-secret, exact-case
// identity required to construct one installation-bound control counterpart.
// Control bearers remain encrypted inside PostgresPhaseJournal.
type RuntimeInstallationDescriptor struct {
	InstallationID   string
	XDeploymentID    string
	ControlProjectID string
}

// ListRedeemPendingInstallations returns both invitation_staged and
// redeem_pending installations. Secret bytes never leave the journal; the
// fixed-origin redeemer calls PrepareRedeem to obtain the exact protected
// request at send time.
func (journal *PostgresPhaseJournal) ListRedeemPendingInstallations(
	ctx context.Context,
	limit int,
) ([]RuntimeInstallationDescriptor, error) {
	return journal.listRuntimeInstallations(ctx, limit, true)
}

// ListRunnableInstallations returns a bounded deterministic set whose control
// binding has already been promoted. Staged invitations are deliberately not
// returned to the ordinary Agent supervisor.
func (journal *PostgresPhaseJournal) ListRunnableInstallations(
	ctx context.Context,
	limit int,
) ([]RuntimeInstallationDescriptor, error) {
	return journal.listRuntimeInstallations(ctx, limit, false)
}

func (journal *PostgresPhaseJournal) listRuntimeInstallations(
	ctx context.Context,
	limit int,
	redeemPending bool,
) ([]RuntimeInstallationDescriptor, error) {
	if journal == nil || journal.db == nil || journal.now == nil ||
		limit < 1 || limit > maxRuntimeInstallationBatch {
		return nil, ErrInvalid
	}
	statePredicate := `binding.invitation_token_enc IS NULL
  AND binding.control_token_enc IS NOT NULL
  AND binding.control_token_sha256 IS NOT NULL
  AND binding.control_project_id IS NOT NULL
  AND binding.control_token_expires_at>$2
  AND binding.redeem_request_sha256 IS NOT NULL
  AND binding.redeem_result_sha256 IS NOT NULL`
	if redeemPending {
		statePredicate = `binding.invitation_token_enc IS NOT NULL
  AND binding.redeem_result_sha256 IS NULL`
	}
	query := `
SELECT installation.installation_id,
       installation.x_deployment_id,
       COALESCE(binding.control_project_id,'')
FROM xinsight_connector_installations AS installation
JOIN xinsight_connector_control_bindings AS binding
  ON binding.installation_id=installation.installation_id
 AND binding.org_id=installation.org_id
 AND binding.project_id=installation.project_id
WHERE installation.desired_state='active'
  AND installation.observed_state<>'disconnected'
  AND ` + statePredicate + `
ORDER BY installation.installation_id COLLATE "C"
LIMIT $1`
	arguments := []any{limit, journal.now().UTC()}
	if redeemPending {
		arguments = arguments[:1]
	}
	rows, err := journal.db.Query(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("%w: list runnable Connector installations: %v", ErrUnavailable, err)
	}
	defer rows.Close()
	descriptors := make([]RuntimeInstallationDescriptor, 0, limit)
	for rows.Next() {
		var descriptor RuntimeInstallationDescriptor
		if err := rows.Scan(
			&descriptor.InstallationID,
			&descriptor.XDeploymentID,
			&descriptor.ControlProjectID,
		); err != nil {
			return nil, fmt.Errorf("%w: scan runnable Connector installation: %v", ErrUnavailable, err)
		}
		if !validControlExternalIdentity(descriptor.InstallationID, "xci_") ||
			!validExactDeploymentIdentity(descriptor.XDeploymentID) ||
			(!redeemPending && !validControlText(descriptor.ControlProjectID, 128)) ||
			(redeemPending && descriptor.ControlProjectID != "" &&
				!validControlText(descriptor.ControlProjectID, 128)) {
			return nil, fmt.Errorf("%w: runnable Connector identity is invalid", ErrUnavailable)
		}
		descriptors = append(descriptors, descriptor)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: iterate runnable Connector installations: %v", ErrUnavailable, err)
	}
	return descriptors, nil
}
