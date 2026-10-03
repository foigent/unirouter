package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/xinsightconnector"
)

// XInsightReceiverBinding is the trusted, deployment-owned network scope for
// one receiver. The X control operation supplies only its immutable digest;
// it never supplies an address or URL that UniRouter follows.
type XInsightReceiverBinding struct {
	BindingSHA256 string
	AllowedIP     string
}

// XInsightReceiverBindingResolver resolves only preconfigured deployment
// authority. Implementations must key exact installation/deployment bytes and
// must not derive an address from the untrusted operation body or perform
// network I/O while the activation transaction is holding product locks.
type XInsightReceiverBindingResolver interface {
	ResolveXInsightReceiverBinding(
		context.Context,
		string,
		string,
		string,
	) (XInsightReceiverBinding, error)
}

// Static-peer resolution additionally needs the committed redeem authority in
// this product transaction, avoiding a second connection or stale outside read.
type XInsightReceiverBindingTxResolver interface {
	ResolveXInsightReceiverBindingTx(context.Context, xinsightconnector.PhaseProductTx,
		string, string, string) (XInsightReceiverBinding, error)
}

func (executor *XInsightConnectorActivationExecutor) resolveReceiverBindingTx(ctx context.Context,
	tx xinsightconnector.PhaseProductTx, installationID, deploymentID, digest string) (XInsightReceiverBinding, error) {
	return resolveXInsightReceiverBindingTx(ctx, tx, executor.binding, installationID, deploymentID, digest)
}

func resolveXInsightReceiverBindingTx(ctx context.Context, tx xinsightconnector.PhaseProductTx,
	binding XInsightReceiverBindingResolver, installationID, deploymentID, digest string) (XInsightReceiverBinding, error) {
	if resolver, ok := binding.(XInsightReceiverBindingTxResolver); ok {
		return resolver.ResolveXInsightReceiverBindingTx(ctx, tx, installationID, deploymentID, digest)
	}
	return binding.ResolveXInsightReceiverBinding(ctx, installationID, deploymentID, digest)
}

// ResolveStaticXInsightReceiverBindingTx shares the production static peer
// qualification with the integration fixture. It does not admit a pending or
// historical originless redeem and performs no network I/O.
func ResolveStaticXInsightReceiverBindingTx(ctx context.Context, tx xinsightconnector.PhaseProductTx, peer *config.XInsightConnectorStaticPeer, installationID, deploymentID, digest string) (XInsightReceiverBinding, error) {
	if peer == nil || peer.Validate() != nil || peer.XDeploymentID != deploymentID || peer.NetworkBindingSHA256 != digest {
		return XInsightReceiverBinding{}, xinsightconnector.ErrConflict
	}
	origin, err := xinsightconnector.ReadCompletedRedeemOriginTx(ctx, tx, installationID, deploymentID)
	if err != nil || origin != peer.XInsightOrigin {
		return XInsightReceiverBinding{}, xinsightconnector.ErrConflict
	}
	return XInsightReceiverBinding{BindingSHA256: digest, AllowedIP: peer.AllowedIP}, nil
}

// XInsightConnectorActivationExecutor composes the frozen three phases with
// UniRouter's existing completed-feed state. It is deliberately a transaction
// executor only: counterpart transport and Agent scheduling remain separate.
type XInsightConnectorActivationExecutor struct {
	handler *Handler
	binding XInsightReceiverBindingResolver
}

func NewXInsightConnectorActivationExecutor(
	handler *Handler,
	binding XInsightReceiverBindingResolver,
) (*XInsightConnectorActivationExecutor, error) {
	if handler == nil || handler.completedFeedController == nil ||
		handler.apiKeyHashSecret == "" || binding == nil {
		return nil, xinsightconnector.ErrInvalid
	}
	return &XInsightConnectorActivationExecutor{handler: handler, binding: binding}, nil
}

func (executor *XInsightConnectorActivationExecutor) ExecutePhaseTx(
	ctx context.Context,
	productTx xinsightconnector.PhaseProductTx,
	input xinsightconnector.PhaseExecutionInput,
) (xinsightconnector.ControlResultV2, error) {
	if executor == nil || executor.handler == nil || executor.binding == nil ||
		executor.handler.completedFeedController == nil {
		return xinsightconnector.ControlResultV2{}, xinsightconnector.ErrInvalid
	}
	if err := executor.validateLiveScopeTx(ctx, productTx, input); err != nil {
		return xinsightconnector.ControlResultV2{}, err
	}

	switch input.Operation.Phase {
	case xinsightconnector.ControlPhasePreparePaused:
		return executor.preparePausedSourceTx(ctx, productTx, input)
	case xinsightconnector.ControlPhaseRegisterReceiver:
		return executor.registerReceiverTx(ctx, productTx, input)
	case xinsightconnector.ControlPhaseResumeSource:
		return executor.resumeSourceTx(ctx, productTx, input)
	default:
		return xinsightconnector.ControlResultV2{}, xinsightconnector.ErrInvalid
	}
}

func (executor *XInsightConnectorActivationExecutor) validateLiveScopeTx(
	ctx context.Context,
	tx xinsightconnector.PhaseProductTx,
	input xinsightconnector.PhaseExecutionInput,
) error {
	installation := input.Installation
	if installation.InstallationID != input.Operation.InstallationID ||
		installation.EventScope != xinsightconnector.EventScopeModelCompleted ||
		installation.DesiredState != xinsightconnector.DesiredActive ||
		installation.ObservedState == xinsightconnector.ObservedDisconnected ||
		!executor.handler.completedFeedController.Enabled() ||
		executor.handler.completedFeedController.SourceInstance() != installation.SourceInstance {
		return xinsightconnector.ErrConflict
	}
	var orgStatus, projectStatus string
	var consentedBy sql.NullInt64
	if err := tx.QueryRow(ctx, `
SELECT organization.status,project.status,installation.consented_by
FROM xinsight_connector_installations AS installation
JOIN projects AS project
  ON project.id=installation.project_id AND project.org_id=installation.org_id
JOIN orgs AS organization ON organization.id=installation.org_id
WHERE installation.installation_id=$1
  AND installation.org_id=$2 AND installation.project_id=$3`,
		installation.InstallationID, installation.OrgID, installation.ProjectID,
	).Scan(&orgStatus, &projectStatus, &consentedBy); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return xinsightconnector.ErrConflict
		}
		return fmt.Errorf("%w: read connector activation scope: %v", xinsightconnector.ErrUnavailable, err)
	}
	if orgStatus != "active" || projectStatus != "active" || !consentedBy.Valid || consentedBy.Int64 <= 0 {
		return xinsightconnector.ErrConflict
	}
	return nil
}

func (executor *XInsightConnectorActivationExecutor) preparePausedSourceTx(
	ctx context.Context,
	tx xinsightconnector.PhaseProductTx,
	input xinsightconnector.PhaseExecutionInput,
) (xinsightconnector.ControlResultV2, error) {
	status, activeCount, stagedCount, err := readXInsightActivationStateTx(
		ctx, tx, input.Installation.OrgID, input.Installation.ProjectID,
	)
	if err != nil {
		return xinsightconnector.ControlResultV2{}, err
	}
	if status != xInsightFeedConnectionPaused || activeCount != 0 || stagedCount != 0 {
		return xinsightconnector.ControlResultV2{}, xinsightconnector.ErrConflict
	}
	var highWatermark int64
	if err := tx.QueryRow(ctx, `
SELECT GREATEST(
  COALESCE((SELECT MAX(item.id) FROM completed_execution_feed_items AS item
            WHERE item.org_id=$1 AND item.project_id=$2),0),
  COALESCE((SELECT state.retained_after_id FROM completed_execution_feed_states AS state
            WHERE state.org_id=$1 AND state.project_id=$2),0)
)`, input.Installation.OrgID, input.Installation.ProjectID).Scan(&highWatermark); err != nil {
		return xinsightconnector.ControlResultV2{}, fmt.Errorf(
			"%w: read completed-feed high watermark: %v", xinsightconnector.ErrUnavailable, err,
		)
	}
	cursor, err := executor.handler.completedFeedController.EncodeSequence(
		input.Installation.ProjectID, highWatermark,
	)
	if err != nil {
		return xinsightconnector.ControlResultV2{}, fmt.Errorf(
			"%w: encode completed-feed cursor: %v", xinsightconnector.ErrUnavailable, err,
		)
	}
	return completedConnectorPhaseResult(input, xinsightconnector.PreparePausedSourceResultV2{
		SourceInstance: input.Installation.SourceInstance,
		Cursor:         cursor, HighWatermark: cursor,
		SourceState: xinsightconnector.DesiredPaused,
		Readiness:   "paused_source_cursor_ready",
	}), nil
}

func (executor *XInsightConnectorActivationExecutor) registerReceiverTx(
	ctx context.Context,
	tx xinsightconnector.PhaseProductTx,
	input xinsightconnector.PhaseExecutionInput,
) (xinsightconnector.ControlResultV2, error) {
	request, ok := input.Operation.Request.(xinsightconnector.RegisterReceiverRequestV2)
	if !ok || request.SourceInstance != input.Installation.SourceInstance {
		return xinsightconnector.ControlResultV2{}, xinsightconnector.ErrConflict
	}
	binding, err := executor.resolveReceiverBindingTx(
		ctx, tx,
		input.Installation.InstallationID,
		input.Installation.XDeploymentID,
		request.NetworkBinding.BindingSHA256,
	)
	if err != nil || binding.BindingSHA256 != request.NetworkBinding.BindingSHA256 ||
		!exactHostNetworkBinding(binding.AllowedIP) {
		return xinsightconnector.ControlResultV2{}, xinsightconnector.ErrConflict
	}
	prefix, err := netip.ParsePrefix(binding.AllowedIP)
	if err != nil {
		return xinsightconnector.ControlResultV2{}, xinsightconnector.ErrConflict
	}
	allowedIP := prefix.Masked().String()
	registration := xInsightFeedCredentialRegistrationV1{
		SchemaVersion:  request.ReceiverRegistration.SchemaVersion,
		TokenFormat:    request.ReceiverRegistration.TokenFormat,
		VerifierScheme: request.ReceiverRegistration.VerifierScheme,
		KeyPrefix:      request.ReceiverRegistration.KeyPrefix,
		VerifierSHA256: request.ReceiverRegistration.VerifierSHA256,
		OperationID:    request.ReceiverRegistration.OperationID,
	}
	staged, created, err := executor.handler.registerInitialReceiverTx(
		ctx, tx, input.Installation.OrgID, input.Installation.ProjectID, allowedIP, registration,
	)
	if err != nil {
		return xinsightconnector.ControlResultV2{}, err
	}
	if created {
		actorID, err := readXInsightConnectorConsentActorTx(ctx, tx, input.Installation)
		if err != nil {
			return xinsightconnector.ControlResultV2{}, err
		}
		if err := insertAuditEntryForActor(
			ctx, tx, actorID,
			"data_exchange.xinsight.credential.create", "xinsight_feed_connection",
			fmt.Sprintf("%d", input.Installation.ProjectID),
			map[string]any{
				"installation_id": input.Installation.InstallationID,
				"operation_id":    input.Operation.OperationID,
				"after": map[string]any{
					"credential_id": staged.ID, "lifecycle": "staged",
					"purpose": xInsightFeedKeyPurpose, "network_bound": true,
					"registration_operation_id": staged.OperationID,
				},
			},
			xInsightConnectorSystemAuditMeta(input),
		); err != nil {
			return xinsightconnector.ControlResultV2{}, fmt.Errorf(
				"%w: record connector receiver audit: %v", xinsightconnector.ErrUnavailable, err,
			)
		}
	}
	registrationSHA, err := xinsightconnector.ReceiverRegistrationSHA256(request.ReceiverRegistration)
	if err != nil {
		return xinsightconnector.ControlResultV2{}, xinsightconnector.ErrInvalid
	}
	return completedConnectorPhaseResult(input, xinsightconnector.RegisterReceiverResultV2{
		SourceInstance:             input.Installation.SourceInstance,
		ReceiverRegistrationSHA256: registrationSHA,
		NetworkBindingSHA256:       request.NetworkBinding.BindingSHA256,
		SourceState:                xinsightconnector.DesiredPaused,
		ReceiverState:              middleware.APIKeyStatusFeedStaged,
		Readiness:                  "paused_receiver_registered",
	}), nil
}

func (executor *XInsightConnectorActivationExecutor) resumeSourceTx(
	ctx context.Context,
	tx xinsightconnector.PhaseProductTx,
	input xinsightconnector.PhaseExecutionInput,
) (xinsightconnector.ControlResultV2, error) {
	request, ok := input.Operation.Request.(xinsightconnector.ResumeSourceRequestV2)
	if !ok || request.SourceInstance != input.Installation.SourceInstance ||
		input.PredecessorOperation == nil || input.PredecessorResult == nil {
		return xinsightconnector.ControlResultV2{}, xinsightconnector.ErrConflict
	}
	registerRequest, ok := input.PredecessorOperation.Request.(xinsightconnector.RegisterReceiverRequestV2)
	if !ok {
		return xinsightconnector.ControlResultV2{}, xinsightconnector.ErrConflict
	}
	binding, err := executor.resolveReceiverBindingTx(
		ctx, tx,
		input.Installation.InstallationID,
		input.Installation.XDeploymentID,
		request.NetworkBindingSHA256,
	)
	if err != nil || binding.BindingSHA256 != request.NetworkBindingSHA256 ||
		!exactHostNetworkBinding(binding.AllowedIP) {
		return xinsightconnector.ControlResultV2{}, xinsightconnector.ErrConflict
	}
	prefix, err := netip.ParsePrefix(binding.AllowedIP)
	if err != nil {
		return xinsightconnector.ControlResultV2{}, xinsightconnector.ErrConflict
	}
	result := completedConnectorPhaseResult(input, xinsightconnector.ResumeSourceResultV2{
		SourceInstance: input.Installation.SourceInstance,
		SourceState:    xinsightconnector.DesiredActive,
		ReceiverState:  xinsightconnector.DesiredActive,
		Readiness:      "source_resumed_receiver_active",
	})
	resultSHA, err := result.CanonicalSHA256()
	if err != nil {
		return xinsightconnector.ControlResultV2{}, xinsightconnector.ErrInvalid
	}
	credentialID, err := executor.handler.promoteAndResumeInitialReceiverTx(
		ctx, tx, input.Installation, registerRequest.ReceiverRegistration,
		prefix.Masked().String(), resultSHA, input.ObservedAt,
	)
	if err != nil {
		return xinsightconnector.ControlResultV2{}, err
	}
	actorID, err := readXInsightConnectorConsentActorTx(ctx, tx, input.Installation)
	if err != nil {
		return xinsightconnector.ControlResultV2{}, err
	}
	for _, audit := range []struct {
		action string
		detail map[string]any
	}{
		{
			action: "data_exchange.xinsight.credential.activate",
			detail: map[string]any{
				"installation_id": input.Installation.InstallationID,
				"operation_id":    input.Operation.OperationID,
				"after":           map[string]any{"credential_id": credentialID, "lifecycle": "active"},
			},
		},
		{
			action: "data_exchange.xinsight.connection.resume",
			detail: map[string]any{
				"installation_id": input.Installation.InstallationID,
				"operation_id":    input.Operation.OperationID,
				"before":          map[string]any{"status": xInsightFeedConnectionPaused},
				"after":           map[string]any{"status": xInsightFeedConnectionActive},
			},
		},
	} {
		if err := insertAuditEntryForActor(
			ctx, tx, actorID, audit.action, "xinsight_feed_connection",
			fmt.Sprintf("%d", input.Installation.ProjectID), audit.detail,
			xInsightConnectorSystemAuditMeta(input),
		); err != nil {
			return xinsightconnector.ControlResultV2{}, fmt.Errorf(
				"%w: record connector resume audit: %v", xinsightconnector.ErrUnavailable, err,
			)
		}
	}
	return result, nil
}

func completedConnectorPhaseResult(
	input xinsightconnector.PhaseExecutionInput,
	result xinsightconnector.ControlPhaseResultV2,
) xinsightconnector.ControlResultV2 {
	return xinsightconnector.ControlResultV2{
		SchemaVersion:  xinsightconnector.ControlResultV2Schema,
		OperationID:    input.Operation.OperationID,
		InstallationID: input.Operation.InstallationID,
		Phase:          input.Operation.Phase,
		Status:         xinsightconnector.ControlResultCompleted,
		Result:         result,
		ObservedAt:     input.ObservedAt.UTC().Format(time.RFC3339Nano),
	}
}

func readXInsightActivationStateTx(
	ctx context.Context,
	tx xinsightconnector.PhaseProductTx,
	orgID, projectID int64,
) (string, int, int, error) {
	var status string
	if err := tx.QueryRow(ctx, `
SELECT status
FROM completed_execution_feed_connections
WHERE org_id=$1 AND project_id=$2
FOR UPDATE`, orgID, projectID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", 0, 0, xinsightconnector.ErrConflict
		}
		return "", 0, 0, fmt.Errorf(
			"%w: read connector feed state: %v", xinsightconnector.ErrUnavailable, err,
		)
	}
	var activeCount, stagedCount int
	if err := tx.QueryRow(ctx, `
SELECT COUNT(*) FILTER (WHERE status='feed_active'),
       COUNT(*) FILTER (WHERE status='feed_staged')
FROM api_keys
WHERE org_id=$1 AND project_id=$2 AND purpose='xinsight.feed.read'`,
		orgID, projectID,
	).Scan(&activeCount, &stagedCount); err != nil {
		return "", 0, 0, fmt.Errorf(
			"%w: read connector credential state: %v", xinsightconnector.ErrUnavailable, err,
		)
	}
	return status, activeCount, stagedCount, nil
}

func readXInsightConnectorConsentActorTx(
	ctx context.Context,
	tx xinsightconnector.PhaseProductTx,
	installation xinsightconnector.Installation,
) (int64, error) {
	var actor sql.NullInt64
	if err := tx.QueryRow(ctx, `
SELECT consented_by FROM xinsight_connector_installations
WHERE installation_id=$1 AND org_id=$2 AND project_id=$3`,
		installation.InstallationID, installation.OrgID, installation.ProjectID,
	).Scan(&actor); err != nil {
		return 0, fmt.Errorf("%w: read connector consent actor: %v", xinsightconnector.ErrUnavailable, err)
	}
	if !actor.Valid || actor.Int64 <= 0 {
		return 0, xinsightconnector.ErrConflict
	}
	return actor.Int64, nil
}

func xInsightConnectorSystemAuditMeta(input xinsightconnector.PhaseExecutionInput) auditMeta {
	return auditMeta{
		OrgID:           input.Installation.OrgID,
		ProjectID:       input.Installation.ProjectID,
		RequestID:       input.Operation.OperationID,
		ClientRequestID: input.Operation.OperationID,
		Source:          "system",
	}
}

func (h *Handler) registerInitialReceiverTx(
	ctx context.Context,
	tx xinsightconnector.PhaseProductTx,
	orgID, projectID int64,
	allowedIP string,
	registration xInsightFeedCredentialRegistrationV1,
) (stagedXInsightFeedCredentialResponse, bool, error) {
	decodedVerifier, err := validateXInsightFeedRegistration(registration)
	if err != nil || !exactHostNetworkBinding(allowedIP) {
		return stagedXInsightFeedCredentialResponse{}, false, xinsightconnector.ErrConflict
	}
	status, activeCount, stagedCount, err := readXInsightActivationStateTx(ctx, tx, orgID, projectID)
	if err != nil {
		return stagedXInsightFeedCredentialResponse{}, false, err
	}
	keyHash := auth.HashReceiverFeedVerifier(decodedVerifier, h.apiKeyHashSecret)
	if status != xInsightFeedConnectionPaused || activeCount != 0 {
		return stagedXInsightFeedCredentialResponse{}, false, xinsightconnector.ErrConflict
	}
	if stagedCount != 0 {
		var existing stagedXInsightFeedCredentialResponse
		var existingHash, existingAllowedIP, existingStatus string
		err := tx.QueryRow(ctx, `
SELECT id,key_prefix,status,verifier_scheme,registration_operation_id,
       key_hash,allowed_ips
FROM api_keys
WHERE org_id=$1 AND project_id=$2 AND purpose='xinsight.feed.read'
  AND registration_operation_id=$3
FOR UPDATE`, orgID, projectID, registration.OperationID).Scan(
			&existing.ID, &existing.KeyPrefix, &existingStatus, &existing.VerifierScheme,
			&existing.OperationID, &existingHash, &existingAllowedIP,
		)
		if err != nil || existingStatus != middleware.APIKeyStatusFeedStaged ||
			existing.KeyPrefix != registration.KeyPrefix ||
			existing.VerifierScheme != registration.VerifierScheme ||
			existing.OperationID != registration.OperationID ||
			existingHash != keyHash || existingAllowedIP != allowedIP {
			return stagedXInsightFeedCredentialResponse{}, false, xinsightconnector.ErrConflict
		}
		existing.Lifecycle = "staged"
		existing.ProjectID = projectID
		existing.Purpose = xInsightFeedKeyPurpose
		existing.KeyType = "service"
		return existing, false, nil
	}
	keyName := fmt.Sprintf("X-Insight feed %d %s", projectID, registration.KeyPrefix)
	var keyID int64
	if err := tx.QueryRow(ctx, `
INSERT INTO api_keys (
    org_id,project_id,owner_user_id,key_type,name,key_hash,key_prefix,
    status,rpm_limit,qps_limit,budget_limit,allowed_ips,allowed_models,
    policy_labels,purpose,verifier_scheme,registration_operation_id,
    created_at,updated_at
) VALUES (
    $1,$2,NULL,'service',$3,$4,$5,
    'feed_staged',60,1,0,$6,'','',
    'xinsight.feed.read',$7,$8,NOW(),NOW()
) RETURNING id`,
		orgID, projectID, keyName, keyHash, registration.KeyPrefix,
		allowedIP, registration.VerifierScheme, registration.OperationID,
	).Scan(&keyID); err != nil {
		if isXInsightFeedRegistrationCollision(err) {
			return stagedXInsightFeedCredentialResponse{}, false, xinsightconnector.ErrConflict
		}
		return stagedXInsightFeedCredentialResponse{}, false, fmt.Errorf(
			"%w: create staged receiver: %v", xinsightconnector.ErrUnavailable, err,
		)
	}
	return stagedXInsightFeedCredentialResponse{
		ID: keyID, KeyPrefix: registration.KeyPrefix, Lifecycle: "staged",
		ProjectID: projectID, Purpose: xInsightFeedKeyPurpose, KeyType: "service",
		VerifierScheme: registration.VerifierScheme, OperationID: registration.OperationID,
	}, true, nil
}

func (h *Handler) promoteAndResumeInitialReceiverTx(
	ctx context.Context,
	tx xinsightconnector.PhaseProductTx,
	installation xinsightconnector.Installation,
	registration xinsightconnector.ReceiverRegistrationV1,
	allowedIP string,
	resultSHA string,
	completedAt time.Time,
) (int64, error) {
	var rootOperationRef, rootOperationState string
	if err := tx.QueryRow(ctx, `
SELECT operation_ref,state
FROM xinsight_connector_operations
WHERE installation_id=$1 AND org_id=$2 AND project_id=$3
  AND action='connect' AND desired_state='active' AND control_fence=$4
ORDER BY created_at,operation_ref
LIMIT 1
FOR UPDATE`, installation.InstallationID, installation.OrgID,
		installation.ProjectID, installation.ControlFence,
	).Scan(&rootOperationRef, &rootOperationState); err != nil {
		return 0, xinsightconnector.ErrConflict
	}
	if rootOperationState != xinsightconnector.OperationQueued &&
		rootOperationState != xinsightconnector.OperationRetryWait {
		return 0, xinsightconnector.ErrConflict
	}
	status, activeCount, stagedCount, err := readXInsightActivationStateTx(
		ctx, tx, installation.OrgID, installation.ProjectID,
	)
	if err != nil {
		return 0, err
	}
	if status != xInsightFeedConnectionPaused || activeCount != 0 || stagedCount != 1 {
		return 0, xinsightconnector.ErrConflict
	}
	decodedVerifier, err := auth.DecodeReceiverFeedVerifier(registration.VerifierSHA256)
	if err != nil {
		return 0, xinsightconnector.ErrConflict
	}
	expectedHash := auth.HashReceiverFeedVerifier(decodedVerifier, h.apiKeyHashSecret)
	var keyID int64
	var keyHash, keyPrefix, keyStatus, verifierScheme, operationID, storedAllowedIP string
	if err := tx.QueryRow(ctx, `
SELECT id,key_hash,key_prefix,status,verifier_scheme,registration_operation_id,allowed_ips
FROM api_keys
WHERE org_id=$1 AND project_id=$2 AND purpose='xinsight.feed.read'
  AND registration_operation_id=$3
FOR UPDATE`, installation.OrgID, installation.ProjectID, registration.OperationID).Scan(
		&keyID, &keyHash, &keyPrefix, &keyStatus, &verifierScheme, &operationID, &storedAllowedIP,
	); err != nil {
		return 0, xinsightconnector.ErrConflict
	}
	if keyHash != expectedHash || keyPrefix != registration.KeyPrefix ||
		keyStatus != middleware.APIKeyStatusFeedStaged ||
		verifierScheme != registration.VerifierScheme || operationID != registration.OperationID ||
		storedAllowedIP != allowedIP {
		return 0, xinsightconnector.ErrConflict
	}
	tag, err := tx.Exec(ctx, `
UPDATE api_keys SET status='feed_active',updated_at=NOW()
WHERE id=$1 AND status='feed_staged'`, keyID)
	if err != nil || tag.RowsAffected() != 1 {
		return 0, xinsightconnector.ErrConflict
	}
	tag, err = tx.Exec(ctx, `
UPDATE completed_execution_feed_connections
SET status='active',updated_at=NOW()
WHERE org_id=$1 AND project_id=$2 AND status='paused'`,
		installation.OrgID, installation.ProjectID,
	)
	if err != nil || tag.RowsAffected() != 1 {
		return 0, xinsightconnector.ErrConflict
	}
	tag, err = tx.Exec(ctx, `
UPDATE xinsight_connector_operations
SET state='succeeded',lease_owner=NULL,lease_token=NULL,lease_expires_at=NULL,
    result_code='connector_activation_complete',result_sha256=$2,error_code=NULL,
    completed_at=$3,updated_at=$3
WHERE operation_ref=$1 AND state IN ('queued','retry_wait')`,
		rootOperationRef, resultSHA, completedAt,
	)
	if err != nil || tag.RowsAffected() != 1 {
		return 0, xinsightconnector.ErrConflict
	}
	tag, err = tx.Exec(ctx, `
UPDATE xinsight_connector_installations
SET observed_state='connected',last_error_code=NULL,last_error_at=NULL,
    state_revision=state_revision+1,updated_at=$5
WHERE installation_id=$1 AND org_id=$2 AND project_id=$3
  AND desired_state='active' AND observed_state<>'disconnected'
  AND control_fence=$4`,
		installation.InstallationID, installation.OrgID, installation.ProjectID,
		installation.ControlFence, completedAt,
	)
	if err != nil || tag.RowsAffected() != 1 {
		return 0, xinsightconnector.ErrConflict
	}
	return keyID, nil
}
