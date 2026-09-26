package modelrouteauthority

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/installstate"
	"github.com/uniroute/unirouter/internal/modelrouting"
)

var (
	ErrSafetyInvalid     = errors.New("modelrouteauthority: invalid safety request")
	ErrSafetyNotFound    = errors.New("modelrouteauthority: safety target not found")
	ErrSafetyConflict    = errors.New("modelrouteauthority: safety authority conflict")
	ErrSafetyUnavailable = errors.New("modelrouteauthority: safety mutation unavailable")
)

type PauseConnectionRequest struct {
	Mode                   modelrouting.Mode
	CustomerOrgID          int64
	ActorUserID            int64
	ConnectionID           int64
	ExpectedConfigRevision int64
}

type RevokeCredentialRequest struct {
	Mode                  modelrouting.Mode
	CustomerOrgID         int64
	ActorUserID           int64
	CredentialBindingID   int64
	ExpectedStateRevision int64
}

type SafetyResult struct {
	ContractVersion string            `json:"contract_version"`
	Action          string            `json:"action"`
	Mode            modelrouting.Mode `json:"mode"`
	ResourceID      int64             `json:"resource_id"`
	State           string            `json:"state"`
	Revision        int64             `json:"revision"`
	Changed         bool              `json:"changed"`
	AppliedAt       time.Time         `json:"applied_at"`
}

type SafetyAuditRecord struct {
	Action       string
	ResourceType string
	ResourceID   string
	Detail       map[string]any
}

type SafetyAuditAppender func(context.Context, pgx.Tx, SafetyAuditRecord) error

type safetyDeployment struct {
	ID              int64
	CustomerOrgID   int64
	ConfigRevision  int64
	AdminState      string
	LegacyChannelID sql.NullInt64
}

// PauseConnection is a reversible safety contraction. It changes only the
// ProviderConnection axis. Verify mode mirrors the legacy channel status in
// the same transaction but deliberately leaves every sibling deployment axis
// untouched.
func (s *Service) PauseConnection(
	ctx context.Context,
	request PauseConnectionRequest,
	appendAudit SafetyAuditAppender,
) (SafetyResult, error) {
	result := SafetyResult{
		ContractVersion: SafetyControlContractVersion,
		Action:          "pause_connection", Mode: request.Mode, ResourceID: request.ConnectionID,
	}
	if s == nil || s.database == nil || appendAudit == nil ||
		request.CustomerOrgID <= 0 || request.ActorUserID <= 0 ||
		request.ConnectionID <= 0 || request.ExpectedConfigRevision <= 0 ||
		(request.Mode != modelrouting.ModeVerify && request.Mode != modelrouting.ModeCanonical) {
		return result, ErrSafetyInvalid
	}
	tx, err := s.database.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return result, safetyUnavailable("begin connection pause", err)
	}
	defer tx.Rollback(ctx)
	if _, err := installstate.RequireOrdinary(ctx, tx); err != nil {
		return result, err
	}
	if err := validateSafetyActor(ctx, tx, request.CustomerOrgID, request.ActorUserID); err != nil {
		return result, err
	}

	var legacyChannelID sql.NullInt64
	var connectionState string
	var connectionRevision int64
	err = tx.QueryRow(ctx, `
SELECT legacy_channel_id,admin_state,config_revision
  FROM provider_connections
 WHERE id=$1
 FOR UPDATE`, request.ConnectionID).Scan(&legacyChannelID, &connectionState, &connectionRevision)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrSafetyNotFound
	}
	if err != nil {
		return result, safetyUnavailable("lock provider connection", err)
	}
	if connectionState == "retired" || connectionRevision != request.ExpectedConfigRevision {
		return result, ErrSafetyConflict
	}
	deployments, err := lockConnectionDeployments(ctx, tx, request.ConnectionID)
	if err != nil {
		return result, err
	}
	if err := validateConnectionOwnership(deployments, request.CustomerOrgID); err != nil {
		return result, err
	}

	changed := connectionState != "disabled"
	if request.Mode == modelrouting.ModeVerify {
		if !legacyChannelID.Valid || legacyChannelID.Int64 <= 0 {
			return result, ErrSafetyConflict
		}
		for _, deployment := range deployments {
			if deployment.AdminState == "retired" || !deployment.LegacyChannelID.Valid ||
				deployment.LegacyChannelID.Int64 != legacyChannelID.Int64 ||
				deployment.ConfigRevision != connectionRevision {
				return result, ErrSafetyConflict
			}
		}
		var channelStatus string
		var channelRevision int64
		if err := tx.QueryRow(ctx, `
SELECT status,routing_epoch
  FROM channels
 WHERE id=$1
 FOR UPDATE`, legacyChannelID.Int64).Scan(&channelStatus, &channelRevision); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return result, ErrSafetyConflict
			}
			return result, safetyUnavailable("lock legacy connection", err)
		}
		expectedConnectionState := "enabled"
		if channelStatus == "disabled" {
			expectedConnectionState = "disabled"
		}
		if channelRevision != connectionRevision || connectionState != expectedConnectionState {
			return result, ErrSafetyConflict
		}
		changed = channelStatus != "disabled" || connectionState != "disabled"
		if changed {
			if tag, err := tx.Exec(ctx, `
UPDATE channels SET status='disabled',updated_at=clock_timestamp()
 WHERE id=$1 AND routing_epoch=$2 AND status<>'disabled'`, legacyChannelID.Int64, channelRevision); err != nil {
				return result, safetyUnavailable("pause legacy connection", err)
			} else if tag.RowsAffected() != 1 {
				return result, ErrSafetyConflict
			}
		}
	}
	if connectionState != "disabled" {
		if tag, err := tx.Exec(ctx, `
UPDATE provider_connections
   SET admin_state='disabled',updated_at=clock_timestamp()
 WHERE id=$1 AND config_revision=$2 AND admin_state=$3`,
			request.ConnectionID, connectionRevision, connectionState,
		); err != nil {
			return result, safetyUnavailable("pause provider connection", err)
		} else if tag.RowsAffected() != 1 {
			return result, ErrSafetyConflict
		}
	}
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&result.AppliedAt); err != nil {
		return result, safetyUnavailable("read connection pause clock", err)
	}
	result.AppliedAt = result.AppliedAt.UTC()
	result.State, result.Revision, result.Changed = "disabled", connectionRevision, changed
	if err := appendAudit(ctx, tx, SafetyAuditRecord{
		Action: "provider_connection.pause", ResourceType: "provider_connection",
		ResourceID: strconv.FormatInt(request.ConnectionID, 10),
		Detail: map[string]any{
			"contract_version": SafetyControlContractVersion, "mode": request.Mode,
			"expected_config_revision": request.ExpectedConfigRevision, "changed": changed,
		},
	}); err != nil {
		return result, safetyUnavailable("append connection pause audit", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return result, safetyUnavailable("commit connection pause", err)
	}
	return result, nil
}

// RevokeCredential is terminal and irreversible for the exact binding row.
// The encrypted secret is scrubbed while identity, links and audit history are
// retained. Verify mode terminalizes and scrubs its legacy source in the same
// transaction so neither projection retains a usable secret.
func (s *Service) RevokeCredential(
	ctx context.Context,
	request RevokeCredentialRequest,
	appendAudit SafetyAuditAppender,
) (SafetyResult, error) {
	result := SafetyResult{
		ContractVersion: SafetyControlContractVersion,
		Action:          "revoke_credential", Mode: request.Mode, ResourceID: request.CredentialBindingID,
	}
	if s == nil || s.database == nil || appendAudit == nil ||
		request.CustomerOrgID <= 0 || request.ActorUserID <= 0 ||
		request.CredentialBindingID <= 0 || request.ExpectedStateRevision <= 0 ||
		(request.Mode != modelrouting.ModeVerify && request.Mode != modelrouting.ModeCanonical) {
		return result, ErrSafetyInvalid
	}
	tx, err := s.database.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return result, safetyUnavailable("begin credential revoke", err)
	}
	defer tx.Rollback(ctx)
	if _, err := installstate.RequireOrdinary(ctx, tx); err != nil {
		return result, err
	}
	if err := validateSafetyActor(ctx, tx, request.CustomerOrgID, request.ActorUserID); err != nil {
		return result, err
	}

	// Keep the same canonical lock order as dispatch: deployments before the
	// credential binding, then the legacy credential in verify mode. Binding
	// identity is immutable. Ownership comes from the binding's connection and
	// all of that connection's live deployments, not only mutable binding links,
	// so an explicitly unlinked leaked secret remains revocable.
	resolvedConnectionID, err := lockCredentialConnectionOwnership(
		ctx, tx, request.CredentialBindingID, request.CustomerOrgID,
	)
	if err != nil {
		return result, err
	}

	var legacyKeyID sql.NullInt64
	var bindingConnectionID int64
	var bindingStatus, bindingSecret string
	var bindingRevision int64
	err = tx.QueryRow(ctx, `
SELECT connection_id,legacy_channel_key_id,status,key_enc,state_revision
  FROM provider_credential_bindings
 WHERE id=$1
 FOR UPDATE`, request.CredentialBindingID).Scan(
		&bindingConnectionID, &legacyKeyID, &bindingStatus, &bindingSecret, &bindingRevision,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrSafetyNotFound
	}
	if err != nil {
		return result, safetyUnavailable("lock credential binding", err)
	}
	if bindingConnectionID != resolvedConnectionID {
		return result, ErrSafetyConflict
	}
	changed := bindingStatus != "retired"
	if bindingStatus == "retired" {
		// An exact replay of the original revoke observes the single revision
		// increment made by that revoke. A caller cannot claim an arbitrary
		// already-retired row by supplying its current revision.
		if bindingSecret != "" || bindingRevision <= 1 || bindingRevision-1 != request.ExpectedStateRevision {
			return result, ErrSafetyConflict
		}
	} else if bindingRevision != request.ExpectedStateRevision {
		return result, ErrSafetyConflict
	}
	if request.Mode == modelrouting.ModeVerify {
		if !legacyKeyID.Valid || legacyKeyID.Int64 <= 0 {
			return result, ErrSafetyConflict
		}
		var legacyStatus, legacySecret string
		var legacyRevision int64
		if err := tx.QueryRow(ctx, `
SELECT status,key_enc,state_epoch
  FROM channel_keys
 WHERE id=$1
 FOR UPDATE`, legacyKeyID.Int64).Scan(&legacyStatus, &legacySecret, &legacyRevision); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return result, ErrSafetyConflict
			}
			return result, safetyUnavailable("lock legacy credential", err)
		}
		if legacyRevision != bindingRevision || legacyStatus != bindingStatus ||
			subtle.ConstantTimeCompare([]byte(legacySecret), []byte(bindingSecret)) != 1 {
			return result, ErrSafetyConflict
		}
		if bindingStatus == "retired" {
			if legacySecret != "" {
				return result, ErrSafetyConflict
			}
			changed = false
		} else {
			if tag, err := tx.Exec(ctx, `
UPDATE channel_keys
   SET status='retired',key_enc='',state_epoch=state_epoch+1,
       cooldown_until=NULL,updated_at=clock_timestamp()
 WHERE id=$1 AND state_epoch=$2 AND status=$3 AND key_enc=$4`,
				legacyKeyID.Int64, legacyRevision, legacyStatus, legacySecret,
			); err != nil {
				return result, safetyUnavailable("revoke legacy credential", err)
			} else if tag.RowsAffected() != 1 {
				return result, ErrSafetyConflict
			}
		}
	}
	if bindingStatus != "retired" {
		if tag, err := tx.Exec(ctx, `
UPDATE provider_credential_bindings
   SET status='retired',key_enc='',retired_at=clock_timestamp(),
       state_revision=state_revision+1,cooldown_until=NULL,updated_at=clock_timestamp()
 WHERE id=$1 AND state_revision=$2 AND status=$3 AND key_enc=$4`,
			request.CredentialBindingID, bindingRevision, bindingStatus, bindingSecret,
		); err != nil {
			return result, safetyUnavailable("revoke credential binding", err)
		} else if tag.RowsAffected() != 1 {
			return result, ErrSafetyConflict
		}
		bindingRevision++
	}
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&result.AppliedAt); err != nil {
		return result, safetyUnavailable("read credential revoke clock", err)
	}
	result.AppliedAt = result.AppliedAt.UTC()
	result.State, result.Revision, result.Changed = "retired", bindingRevision, changed
	if err := appendAudit(ctx, tx, SafetyAuditRecord{
		Action: "provider_credential_binding.revoke", ResourceType: "provider_credential_binding",
		ResourceID: strconv.FormatInt(request.CredentialBindingID, 10),
		Detail: map[string]any{
			"contract_version": SafetyControlContractVersion, "mode": request.Mode,
			"expected_state_revision": request.ExpectedStateRevision, "resulting_state_revision": bindingRevision,
			"changed": changed, "secret_scrubbed": true, "terminal": true,
		},
	}); err != nil {
		return result, safetyUnavailable("append credential revoke audit", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return result, safetyUnavailable("commit credential revoke", err)
	}
	return result, nil
}

func lockConnectionDeployments(ctx context.Context, tx pgx.Tx, connectionID int64) ([]safetyDeployment, error) {
	rows, err := tx.Query(ctx, `
SELECT id,customer_org_id,config_revision,admin_state,legacy_channel_id
  FROM model_deployments
 WHERE connection_id=$1 AND admin_state<>'retired'
 ORDER BY id
 FOR SHARE`, connectionID)
	if err != nil {
		return nil, safetyUnavailable("lock connection deployments", err)
	}
	defer rows.Close()
	result := make([]safetyDeployment, 0)
	for rows.Next() {
		var item safetyDeployment
		if err := rows.Scan(&item.ID, &item.CustomerOrgID, &item.ConfigRevision, &item.AdminState, &item.LegacyChannelID); err != nil {
			return nil, safetyUnavailable("scan connection deployment", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, safetyUnavailable("iterate connection deployments", err)
	}
	return result, nil
}

func validateConnectionOwnership(deployments []safetyDeployment, customerOrgID int64) error {
	if len(deployments) == 0 {
		return ErrSafetyNotFound
	}
	for _, deployment := range deployments {
		if deployment.CustomerOrgID != customerOrgID {
			return ErrSafetyNotFound
		}
	}
	return nil
}

func validateSafetyActor(ctx context.Context, tx pgx.Tx, customerOrgID, actorUserID int64) error {
	// SHARE excludes concurrent root-customer inserts/updates while the exact
	// actor and target root are validated. The safety write therefore cannot
	// commit from an authentication snapshot that ceased to be authoritative.
	if _, err := tx.Exec(ctx, `LOCK TABLE orgs IN SHARE MODE`); err != nil {
		return safetyUnavailable("lock customer authority", err)
	}
	rows, err := tx.Query(ctx, `
SELECT id FROM orgs
 WHERE status='active' AND org_type='customer' AND parent_id IS NULL
 ORDER BY id
 FOR SHARE`)
	if err != nil {
		return safetyUnavailable("read customer authority", err)
	}
	rootIDs := make([]int64, 0, 2)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return safetyUnavailable("scan customer authority", err)
		}
		rootIDs = append(rootIDs, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return safetyUnavailable("iterate customer authority", err)
	}
	rows.Close()
	if len(rootIDs) != 1 || rootIDs[0] != customerOrgID {
		return ErrSafetyConflict
	}
	var actorOrgID int64
	var actorStatus, actorRole string
	if err := tx.QueryRow(ctx, `
SELECT org_id,status,organization_role FROM users WHERE id=$1 FOR SHARE`, actorUserID).Scan(
		&actorOrgID, &actorStatus, &actorRole,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrSafetyConflict
		}
		return safetyUnavailable("lock safety actor", err)
	}
	if actorOrgID != customerOrgID || actorStatus != "active" ||
		(actorRole != "platform_admin" && actorRole != "org_admin") {
		return ErrSafetyConflict
	}
	return nil
}

func lockCredentialConnectionOwnership(
	ctx context.Context,
	tx pgx.Tx,
	bindingID, customerOrgID int64,
) (int64, error) {
	var connectionID int64
	if err := tx.QueryRow(ctx, `
SELECT connection_id FROM provider_credential_bindings WHERE id=$1`, bindingID).Scan(&connectionID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrSafetyNotFound
		}
		return 0, safetyUnavailable("resolve credential connection", err)
	}
	var lockedConnectionID int64
	if err := tx.QueryRow(ctx, `
SELECT id FROM provider_connections WHERE id=$1 FOR SHARE`, connectionID).Scan(&lockedConnectionID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrSafetyNotFound
		}
		return 0, safetyUnavailable("lock credential connection", err)
	}
	deployments, err := lockConnectionDeployments(ctx, tx, connectionID)
	if err != nil {
		return 0, err
	}
	for _, deployment := range deployments {
		if deployment.CustomerOrgID != customerOrgID {
			return 0, ErrSafetyNotFound
		}
	}
	return lockedConnectionID, nil
}

func safetyUnavailable(operation string, err error) error {
	return fmt.Errorf("%w: %s: %v", ErrSafetyUnavailable, operation, err)
}
