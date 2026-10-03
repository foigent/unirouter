package admin

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/principallock"
	"github.com/uniroute/unirouter/internal/xinsightconnector"
)

// The system agent revalidates the saved human consent against live DB roles.
// It never manufactures a human HTTP session or calls a human mutation handler.
func (e *XInsightConnectorActivationExecutor) AuthorizePhaseV3Tx(ctx context.Context, tx pgx.Tx, i xinsightconnector.Installation) (int64, error) {
	actor, err := readXInsightConnectorConsentActorTx(ctx, tx, i)
	if err != nil {
		return 0, err
	}
	if err = principallock.LockUsers(ctx, tx, actor); err != nil {
		return 0, err
	}
	var org int64
	var role, status string
	err = tx.QueryRow(ctx, `SELECT org_id,organization_role,status FROM users WHERE id=$1 FOR UPDATE`, actor).Scan(&org, &role, &status)
	if err != nil || status != "active" {
		return 0, xinsightconnector.ErrUnauthorized
	}
	if role == auth.RolePlatformAdmin || (org == i.OrgID && role == auth.RoleOrgAdmin) {
		return actor, nil
	}
	var member string
	err = tx.QueryRow(ctx, `SELECT role FROM project_members WHERE user_id=$1 AND project_id=$2 FOR SHARE`, actor, i.ProjectID).Scan(&member)
	if err != nil || org != i.OrgID || member != projectMemberAdmin {
		return 0, xinsightconnector.ErrUnauthorized
	}
	return actor, nil
}
func (e *XInsightConnectorActivationExecutor) ExecutePhaseV3Tx(ctx context.Context, tx xinsightconnector.PhaseProductTx, in xinsightconnector.PhaseExecutionInputV3) (xinsightconnector.ControlResultV3, error) {
	o := in.Operation
	i := in.Installation
	if e == nil || e.handler == nil || e.handler.completedFeedController == nil || xinsightconnector.ValidateControlOperationV3(o) != nil {
		return xinsightconnector.ControlResultV3{}, xinsightconnector.ErrInvalid
	}
	if err := e.validateLiveScopeTx(ctx, tx, xinsightconnector.PhaseExecutionInput{Installation: i, Operation: xinsightconnector.ControlOperationV2{InstallationID: o.InstallationID}}); err != nil {
		return xinsightconnector.ControlResultV3{}, err
	}
	actor, err := readXInsightConnectorConsentActorTx(ctx, tx, i)
	if err != nil || actor != in.ConsentActorID {
		return xinsightconnector.ControlResultV3{}, xinsightconnector.ErrUnauthorized
	}
	var body any
	switch q := o.Request.(type) {
	case xinsightconnector.BindExistingReceiverRequest:
		body, err = e.bindExistingReceiverV3Tx(ctx, tx, in, q)
	case xinsightconnector.ConfirmExistingReceiverRequest:
		body, err = e.confirmExistingReceiverV3Tx(ctx, tx, in, q)
	case xinsightconnector.DeliverMachineRequest:
		body, err = e.deliverMachineV3Tx(ctx, tx, in, q)
	default:
		err = xinsightconnector.ErrInvalid
	}
	if err != nil {
		return xinsightconnector.ControlResultV3{}, err
	}
	return xinsightconnector.ControlResultV3{SchemaVersion: xinsightconnector.ControlResultV3Schema, OperationID: o.OperationID, InstallationID: o.InstallationID, Kind: o.Kind, Phase: o.Phase, Status: "completed", Result: body, ObservedAt: in.ObservedAt.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano)}, nil
}
func (e *XInsightConnectorActivationExecutor) requireOriginalFeedV3Tx(ctx context.Context, tx xinsightconnector.PhaseProductTx, i xinsightconnector.Installation, c xinsightconnector.ExistingReceiverCandidate) error {
	if c.SourceInstance != i.SourceInstance || c.SourceBindingSHA256 != i.SourceBindingSHA256 {
		return xinsightconnector.ErrConflict
	}
	status, active, staged, err := readXInsightActivationStateTx(ctx, tx, i.OrgID, i.ProjectID)
	if err != nil || status != "active" || active != 1 || staged != 0 {
		return xinsightconnector.ErrConflict
	}
	var hash, prefix, keyStatus, scheme, operation, ip string
	err = tx.QueryRow(ctx, `SELECT key_hash,key_prefix,status,verifier_scheme,registration_operation_id,allowed_ips FROM api_keys WHERE id=$1 AND org_id=$2 AND project_id=$3 AND purpose='xinsight.feed.read' FOR UPDATE`, c.FeedKeyID, i.OrgID, i.ProjectID).Scan(&hash, &prefix, &keyStatus, &scheme, &operation, &ip)
	verifier, decodeErr := auth.DecodeReceiverFeedVerifier(c.ReceiverRegistration.VerifierSHA256)
	if err != nil || decodeErr != nil || keyStatus != "feed_active" || prefix != c.ReceiverRegistration.KeyPrefix || scheme != c.ReceiverRegistration.VerifierScheme || operation != c.ReceiverRegistration.OperationID || ip != c.AllowedIP || subtle.ConstantTimeCompare([]byte(hash), []byte(auth.HashReceiverFeedVerifier(verifier, e.handler.apiKeyHashSecret))) != 1 {
		return xinsightconnector.ErrConflict
	}
	return nil
}
func (e *XInsightConnectorActivationExecutor) bindExistingReceiverV3Tx(ctx context.Context, tx xinsightconnector.PhaseProductTx, in xinsightconnector.PhaseExecutionInputV3, q xinsightconnector.BindExistingReceiverRequest) (any, error) {
	i := in.Installation
	if err := xinsightconnector.ValidateAdoptionBootstrapCandidateTx(ctx, tx, i, q.Candidate); err != nil {
		return nil, err
	}
	if err := e.requireOriginalFeedV3Tx(ctx, tx, i, q.Candidate); err != nil {
		return nil, err
	}
	b := xinsightconnector.AdoptionBinding{InstallationID: i.InstallationID, OrgID: i.OrgID, ProjectID: i.ProjectID, OperationID: in.Operation.OperationID, FeedKeyID: q.Candidate.FeedKeyID, CandidateSHA256: q.CandidateSHA256}
	sha := xinsightconnector.PublicCanonicalSHA256(b)
	_, err := tx.Exec(ctx, `INSERT INTO xinsight_receiver_adoptions(installation_id,org_id,project_id,feed_key_id,bind_operation_id,candidate_sha256,adoption_binding_sha256,status,bound_at) VALUES($1,$2,$3,$4,$5,$6,$7,'bound',$8)`, i.InstallationID, i.OrgID, i.ProjectID, q.Candidate.FeedKeyID, in.Operation.OperationID, q.CandidateSHA256, sha, in.ObservedAt)
	if err != nil {
		return nil, fmt.Errorf("%w: adoption binding", xinsightconnector.ErrConflict)
	}
	if err = e.v3Audit(ctx, tx, in, "data_exchange.xinsight.receiver.bind", map[string]any{"feed_key_id": b.FeedKeyID, "candidate_sha256": q.CandidateSHA256}); err != nil {
		return nil, err
	}
	return xinsightconnector.BindExistingReceiverResult{SourceInstance: i.SourceInstance, CandidateSHA256: q.CandidateSHA256, AdoptionBinding: b, AdoptionBindingSHA256: sha, SourceState: "active", ReceiverState: "feed_active", Readiness: "existing_receiver_bound"}, nil
}
func (e *XInsightConnectorActivationExecutor) confirmExistingReceiverV3Tx(ctx context.Context, tx xinsightconnector.PhaseProductTx, in xinsightconnector.PhaseExecutionInputV3, q xinsightconnector.ConfirmExistingReceiverRequest) (any, error) {
	if in.PredecessorOperationV3 == nil || in.PredecessorResultV3 == nil {
		return nil, xinsightconnector.ErrConflict
	}
	prior, ok := in.PredecessorOperationV3.Request.(xinsightconnector.BindExistingReceiverRequest)
	if !ok {
		return nil, xinsightconnector.ErrConflict
	}
	c := prior.Candidate
	x := q.XIAdoptionReceipt
	f := q.FeedReadReceipt
	if x.CandidateSHA256 != prior.CandidateSHA256 || x.StateID != c.Checkpoint.StateID || x.ConsumerEpoch != c.Checkpoint.ConsumerEpoch || x.EpochOperationID != c.Checkpoint.EpochOperationID || x.CommittedCursorSHA256 != c.Checkpoint.CursorSHA256 || x.CommittedHighWatermarkSHA256 != c.Checkpoint.HighWatermarkSHA256 || f.FeedKeyID != c.FeedKeyID || f.ReceiverRegistrationSHA256 != xinsightconnector.PublicCanonicalSHA256(c.ReceiverRegistration) || f.WorkerConfigSHA256 != xinsightconnector.PublicCanonicalSHA256(c.WorkerConfig) {
		return nil, xinsightconnector.ErrConflict
	}
	if err := e.requireOriginalFeedV3Tx(ctx, tx, in.Installation, c); err != nil {
		return nil, err
	}
	var bindOp, candidate, sha, status string
	var bound time.Time
	err := tx.QueryRow(ctx, `SELECT bind_operation_id,candidate_sha256,adoption_binding_sha256,status,bound_at FROM xinsight_receiver_adoptions WHERE installation_id=$1 AND org_id=$2 AND project_id=$3 AND feed_key_id=$4 FOR UPDATE`, in.Installation.InstallationID, in.Installation.OrgID, in.Installation.ProjectID, c.FeedKeyID).Scan(&bindOp, &candidate, &sha, &status, &bound)
	if err != nil || bindOp != in.Operation.PredecessorOperationID || candidate != prior.CandidateSHA256 || sha != q.AdoptionBindingSHA256 || status != "bound" {
		return nil, xinsightconnector.ErrConflict
	}
	var pull sql.NullTime
	var key sql.NullInt64
	err = tx.QueryRow(ctx, `SELECT last_authenticated_pull_at,last_authenticated_key_id FROM completed_execution_feed_connections WHERE org_id=$1 AND project_id=$2`, in.Installation.OrgID, in.Installation.ProjectID).Scan(&pull, &key)
	if err != nil || !pull.Valid || !key.Valid || strconv.FormatInt(key.Int64, 10) != c.FeedKeyID || !pull.Time.After(bound) {
		return nil, xinsightconnector.ErrConflict
	}
	// UR checks its own authentication evidence; XI's authenticated public
	// projection attests its successful CommitPage. No cross-host time ordering.
	body := xinsightconnector.ConfirmExistingReceiverResult{SourceInstance: in.Installation.SourceInstance, AdoptionBindingSHA256: q.AdoptionBindingSHA256, XIAdoptionReceiptSHA256: q.XIAdoptionReceiptSHA256, FeedReadReceiptSHA256: q.FeedReadReceiptSHA256, SourceState: "active", ReceiverState: "feed_active", Readiness: "existing_receiver_adopted"}
	result := xinsightconnector.ControlResultV3{SchemaVersion: xinsightconnector.ControlResultV3Schema, OperationID: in.Operation.OperationID, InstallationID: in.Installation.InstallationID, Kind: in.Operation.Kind, Phase: in.Operation.Phase, Status: "completed", Result: body, ObservedAt: in.ObservedAt.Format(time.RFC3339Nano)}
	resultSHA, err := result.CanonicalSHA256()
	if err != nil {
		return nil, err
	}
	tag, err := tx.Exec(ctx, `UPDATE xinsight_receiver_adoptions SET status='confirmed',confirm_operation_id=$2,xi_adoption_receipt_sha256=$3,feed_read_receipt_sha256=$4,confirmed_at=$5,confirm_result_sha256=$6 WHERE installation_id=$1 AND status='bound'`, in.Installation.InstallationID, in.Operation.OperationID, q.XIAdoptionReceiptSHA256, q.FeedReadReceiptSHA256, in.ObservedAt, resultSHA)
	if err != nil || tag.RowsAffected() != 1 {
		return nil, xinsightconnector.ErrConflict
	}
	tag, err = tx.Exec(ctx, `UPDATE xinsight_connector_installations SET observed_state='connected',state_revision=state_revision+1,last_error_code=NULL,last_error_at=NULL,updated_at=$3 WHERE installation_id=$1 AND control_fence=$2 AND desired_state='active' AND observed_state<>'disconnected'`, in.Installation.InstallationID, in.Installation.ControlFence, in.ObservedAt)
	if err != nil || tag.RowsAffected() != 1 {
		return nil, xinsightconnector.ErrConflict
	}
	tag, err = tx.Exec(ctx, `UPDATE xinsight_connector_operations SET state='succeeded',result_code='receiver_adoption_complete',result_sha256=$2,lease_owner=NULL,lease_token=NULL,lease_expires_at=NULL,completed_at=$3,updated_at=$3 WHERE installation_id=$1 AND action='connect' AND state IN ('queued','retry_wait') AND control_fence=$4`, in.Installation.InstallationID, resultSHA, in.ObservedAt, in.Installation.ControlFence)
	if err != nil || tag.RowsAffected() != 1 {
		return nil, xinsightconnector.ErrConflict
	}
	if err = e.v3Audit(ctx, tx, in, "data_exchange.xinsight.receiver.confirm", map[string]any{"feed_key_id": c.FeedKeyID, "feed_read_receipt_sha256": q.FeedReadReceiptSHA256}); err != nil {
		return nil, err
	}
	return body, nil
}
func (e *XInsightConnectorActivationExecutor) deliverMachineV3Tx(ctx context.Context, tx xinsightconnector.PhaseProductTx, in xinsightconnector.PhaseExecutionInputV3, q xinsightconnector.DeliverMachineRequest) (any, error) {
	if q.SourceInstance != in.Installation.SourceInstance || q.SourceBindingSHA256 != in.Installation.SourceBindingSHA256 || in.Installation.ObservedState != "connected" {
		return nil, xinsightconnector.ErrConflict
	}
	// Existing active/staged credentials cannot be silently rotated by a new
	// pairing candidate. Reusing the exact already-delivered candidate is journal
	// replay, handled before product execution.
	var conflict bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM xinsight_machine_credentials mc JOIN api_keys ak ON ak.id=mc.api_key_id WHERE mc.installation_id=$1 AND mc.org_id=$2 AND mc.purpose=$3 AND ak.status IN ('feed_active','feed_staged'))`, in.Installation.InstallationID, in.Installation.OrgID, q.Registration.Purpose).Scan(&conflict)
	if err != nil {
		return nil, err
	}
	if conflict {
		return nil, xinsightconnector.ErrConflict
	}
	if err = e.v3Audit(ctx, tx, in, "data_exchange.xinsight.machine_candidate.deliver", map[string]any{"purpose": q.Registration.Purpose, "registration_sha256": q.RegistrationSHA256}); err != nil {
		return nil, err
	}
	return xinsightconnector.DeliverMachineResult{SourceInstance: in.Installation.SourceInstance, Purpose: q.Registration.Purpose, RegistrationSHA256: q.RegistrationSHA256, CustodyReceiptSHA256: q.CustodyReceiptSHA256, Readiness: "public_candidate_delivered", Activation: "not_asserted"}, nil
}
func (e *XInsightConnectorActivationExecutor) v3Audit(ctx context.Context, tx xinsightconnector.PhaseProductTx, in xinsightconnector.PhaseExecutionInputV3, action string, details map[string]any) error {
	return insertAuditEntry(ctx, tx, action, "xinsight_connector_installation", in.Installation.InstallationID, details, auditMeta{OrgID: in.Installation.OrgID, ProjectID: in.Installation.ProjectID, RequestID: in.Operation.OperationID, ClientRequestID: in.Operation.OperationID, Source: "system"})
}
