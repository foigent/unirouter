package billing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/executionevidence"
)

const (
	ProviderAttemptOutcomeRejected           = "rejected"
	ProviderAttemptOutcomeResponseAccepted   = "response_accepted"
	ProviderAttemptOutcomeUnknown            = "outcome_unknown"
	ProviderAttemptOutcomeResponseUnusable   = "response_unusable"
	ProviderAttemptStateReleasedUndispatched = "released_undispatched"
)

const (
	providerAttemptStateDispatchStarted    = "dispatch_started"
	providerAttemptStateFundingPending     = "funding_pending"
	providerAttemptStateDeliveryPending    = "delivery_pending"
	providerAttemptStateDeliveryFailed     = "delivery_failed"
	providerAttemptStateOutputWithheld     = ProviderAttemptStateOutputWithheld
	providerAttemptStateSettled            = "settled"
	providerAttemptCertaintyPending        = "pending"
	providerAttemptCertaintyRejected       = "known_rejected"
	providerAttemptCertaintyUnknown        = "unknown"
	providerAttemptCertaintyKnownUsage     = "known_usage"
	providerAttemptCertaintyNotDispatched  = "not_dispatched"
	pendingReasonAmbiguousAttempt          = "ambiguous_attempt"
	pendingReasonResponseUnusable          = "provider_response_unusable"
	pendingReasonProviderAttemptUnresolved = "provider_attempt_unresolved"
)

var ErrProviderAttemptConflict = errors.New("provider attempt evidence conflicts with the durable record")
var ErrProviderAttemptUnresolved = errors.New("provider attempt has unresolved financial liability")

// ProviderAttemptStart is the bounded identity envelope persisted before a
// network call that may create provider liability. It deliberately excludes
// credentials, prompts, response bodies, and full provider URLs.
var runtimeInstancePattern = regexp.MustCompile(`^uri_[a-f0-9]{32}$`)

type ProviderAttemptStart struct {
	RequestID          string
	AttemptID          string
	OrgID              int64
	APIKeyID           int64
	AccessCredentialID int64
	CredentialRef      string
	AccessGrantID      int64
	ChannelID          int64
	ChannelKeyID       int64
	RouteSequence      int
	RouteTotal         int
	Endpoint           string
	Provider           string
	Channel            string
	// ExecutionSnapshotHash is the Plan identity frozen before dispatch. It is
	// independent from RouteEvidence.SnapshotHash because immutable legacy
	// evidence intentionally has no canonical route snapshot field.
	ExecutionSnapshotHash string
	RuntimeInstanceID     string
	// RouteEvidence is nil only for the pre-contract legacy writer. New
	// dispatch coordinators persist the exact secret-free route proof before
	// issuing an upstream request.
	RouteEvidence *executionevidence.EvidenceV1
	// PublicRouteEvidence is the explicit V2 authority for Responses/Messages.
	// It is mutually exclusive with RouteEvidence so a V2 receipt cannot be
	// downgraded to the historical digest by an ambiguous writer.
	PublicRouteEvidence *executionevidence.EvidenceV2
}

type normalizedProviderAttemptStart struct {
	ProviderAttemptStart
	routeEvidenceHash string
	capabilityJSON    []byte
}

// ProviderAttemptCompletion records what UniRouter knows after dispatch. A
// retry is financially authorized only for a durable rejected response.
type ProviderAttemptCompletion struct {
	RequestID         string
	AttemptID         string
	OrgID             int64
	Outcome           string
	RetryAuthorized   bool
	HTTPStatus        int
	FailureKind       string
	UpstreamRequestID string
}

type providerAttemptRecord struct {
	APIKeyID              int64
	AccessCredentialID    sql.NullInt64
	CredentialRef         sql.NullString
	AccessGrantID         sql.NullInt64
	ChannelID             sql.NullInt64
	ChannelKeyID          sql.NullInt64
	RouteSequence         int
	RouteTotal            int
	Endpoint              string
	Provider              string
	Channel               string
	State                 string
	OutcomeCertainty      string
	RetryAuthorized       bool
	HTTPStatus            sql.NullInt32
	FailureKind           sql.NullString
	UpstreamRequestID     sql.NullString
	CalculatedCostMicros  sql.NullInt64
	ChargedCostMicros     sql.NullInt64
	TerminalTransactionID sql.NullInt64
	RouteEvidenceVersion  sql.NullString
	RouteEvidenceHash     sql.NullString
	ExecutionSnapshotHash sql.NullString
	DispatchOwnerHash     sql.NullString
	RuntimeInstanceID     string
}

type providerAttemptEvidenceColumns struct {
	contractVersion               any
	authorityMode                 any
	contractSource                any
	evidenceHash                  any
	publicModelID                 int64
	deploymentID                  int64
	connectionID                  int64
	credentialBindingID           int64
	connectionRevision            int64
	deploymentRevision            int64
	circuitRevision               int64
	credentialStateRevision       int64
	credentialLinkedAt            any
	modelOperationID              any
	operationRevision             int64
	protocolProfileID             any
	protocolProfileVersion        any
	operationEvidenceState        any
	operationEvidenceRef          any
	operationVerifiedAt           any
	capabilityJSON                any
	capabilityEvidenceHash        any
	credentialProbeID             int64
	credentialProbeTestedAt       any
	runtimeReleaseID              int64
	runtimeReleaseRef             any
	runtimeStage                  any
	slotVersion                   int64
	snapshotHash                  any
	legacyRoutingRevision         int64
	legacyCircuitRevision         int64
	legacyCredentialStateRevision int64
	requiresProbe                 any
	probeLeaseTokenHash           any
	publicExecutionBindingID      int64
	deliveryContractVersion       any
	chargeContractVersion         any
	idempotencyContractVersion    any
}

func providerAttemptEvidenceValues(req normalizedProviderAttemptStart) providerAttemptEvidenceColumns {
	if req.RouteEvidence == nil && req.PublicRouteEvidence == nil {
		return providerAttemptEvidenceColumns{}
	}
	var evidence *executionevidence.EvidenceV1
	if req.PublicRouteEvidence != nil {
		evidence = &req.PublicRouteEvidence.EvidenceV1
	} else {
		evidence = req.RouteEvidence
	}
	values := providerAttemptEvidenceColumns{
		contractVersion: evidence.ContractVersion, authorityMode: string(evidence.AuthorityMode),
		contractSource: string(evidence.ExecutionContractSource), evidenceHash: req.routeEvidenceHash,
		publicModelID: evidence.PublicModelID, deploymentID: evidence.DeploymentID,
		connectionID: evidence.ConnectionID, credentialBindingID: evidence.CredentialBindingID,
		connectionRevision: evidence.ConnectionRevision, deploymentRevision: evidence.DeploymentRevision,
		circuitRevision: evidence.CircuitRevision, credentialStateRevision: evidence.CredentialStateRevision,
		modelOperationID: evidence.ModelOperationID, operationRevision: evidence.OperationRevision,
		protocolProfileID:      nullableEvidenceString(evidence.ProtocolProfileID),
		protocolProfileVersion: nullableEvidenceString(evidence.ProtocolProfileVersion),
		operationEvidenceState: nullableEvidenceString(evidence.OperationEvidenceState),
		operationEvidenceRef:   nullableEvidenceString(evidence.OperationEvidenceRef),
		capabilityEvidenceHash: nullableEvidenceString(evidence.CapabilityEvidenceHash),
		credentialProbeID:      evidence.CredentialProbeID, runtimeReleaseID: evidence.RuntimeReleaseID,
		runtimeReleaseRef: nullableEvidenceString(evidence.RuntimeReleaseRef),
		runtimeStage:      nullableEvidenceString(evidence.RuntimeStage), slotVersion: evidence.SlotVersion,
		snapshotHash:                  nullableEvidenceString(evidence.SnapshotHash),
		legacyRoutingRevision:         evidence.LegacyRoutingRevision,
		legacyCircuitRevision:         evidence.LegacyCircuitRevision,
		legacyCredentialStateRevision: evidence.LegacyCredentialStateRevision,
		requiresProbe:                 evidence.RequiresProbe,
		probeLeaseTokenHash:           nullableEvidenceString(evidence.ProbeLeaseTokenHash),
	}
	if req.PublicRouteEvidence != nil {
		values.publicExecutionBindingID = req.PublicRouteEvidence.PublicExecutionBindingID
		values.deliveryContractVersion = req.PublicRouteEvidence.DeliveryContractVersion
		values.chargeContractVersion = req.PublicRouteEvidence.ChargeContractVersion
		values.idempotencyContractVersion = req.PublicRouteEvidence.IdempotencyContractVersion
	}
	values.credentialLinkedAt = nullableEvidenceTime(evidence.CredentialLinkedAt)
	values.operationVerifiedAt = nullableEvidenceTimePointer(evidence.OperationVerifiedAt)
	values.credentialProbeTestedAt = nullableEvidenceTime(evidence.CredentialProbeTestedAt)
	if len(req.capabilityJSON) > 0 {
		values.capabilityJSON = string(req.capabilityJSON)
	}
	return values
}

func nullableEvidenceString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableEvidenceTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func nullableEvidenceTimePointer(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return *value
}

func normalizeProviderAttemptStart(req ProviderAttemptStart) (normalizedProviderAttemptStart, error) {
	req.RequestID = strings.TrimSpace(req.RequestID)
	req.AttemptID = strings.TrimSpace(req.AttemptID)
	req.Endpoint = strings.TrimSpace(req.Endpoint)
	req.Provider = strings.TrimSpace(req.Provider)
	req.Channel = strings.TrimSpace(req.Channel)
	req.ExecutionSnapshotHash = strings.TrimSpace(req.ExecutionSnapshotHash)
	if req.RequestID == "" || len(req.RequestID) > 64 || req.AttemptID == "" || len(req.AttemptID) > 64 {
		return normalizedProviderAttemptStart{}, fmt.Errorf("provider attempt requires bounded request_id and attempt_id")
	}
	if req.OrgID <= 0 || req.APIKeyID <= 0 {
		return normalizedProviderAttemptStart{}, fmt.Errorf("provider attempt requires positive org and API key identities")
	}
	if req.RouteSequence <= 0 || req.RouteTotal <= 0 || req.RouteSequence > req.RouteTotal {
		return normalizedProviderAttemptStart{}, fmt.Errorf("provider attempt route sequence is invalid")
	}
	if req.Endpoint == "" || len(req.Endpoint) > 64 || req.Provider == "" || len(req.Provider) > 50 || req.Channel == "" || len(req.Channel) > 255 {
		return normalizedProviderAttemptStart{}, fmt.Errorf("provider attempt routing metadata is invalid")
	}
	if req.ExecutionSnapshotHash != "" && !validModelExecutionSnapshotHash(req.ExecutionSnapshotHash) {
		return normalizedProviderAttemptStart{}, fmt.Errorf("provider attempt execution snapshot hash is invalid")
	}

	if req.RuntimeInstanceID != "" && !runtimeInstancePattern.MatchString(req.RuntimeInstanceID) {
		return normalizedProviderAttemptStart{}, fmt.Errorf("invalid runtime instance identity")
	}
	normalized := normalizedProviderAttemptStart{ProviderAttemptStart: req}
	if req.RouteEvidence != nil && req.PublicRouteEvidence != nil {
		return normalizedProviderAttemptStart{}, fmt.Errorf("provider attempt route evidence versions are mutually exclusive")
	}
	if req.RouteEvidence == nil && req.PublicRouteEvidence == nil {
		if req.ChannelID <= 0 || req.ChannelKeyID <= 0 {
			return normalizedProviderAttemptStart{}, fmt.Errorf("legacy provider attempt requires positive channel and channel-key identities")
		}
		return normalized, nil
	}
	if req.PublicRouteEvidence != nil {
		evidence, err := executionevidence.NormalizeV2ForDispatch(*req.PublicRouteEvidence)
		if err != nil {
			return normalizedProviderAttemptStart{}, fmt.Errorf("provider attempt public route evidence: %w", err)
		}
		if req.ChannelID != 0 || req.ChannelKeyID != 0 {
			return normalizedProviderAttemptStart{}, fmt.Errorf("public provider attempt cannot carry legacy route identities")
		}
		hash, err := executionevidence.DigestV2(evidence)
		if err != nil {
			return normalizedProviderAttemptStart{}, fmt.Errorf("digest provider attempt public route evidence: %w", err)
		}
		capabilityJSON, err := json.Marshal(evidence.CapabilityEvidence)
		if err != nil {
			return normalizedProviderAttemptStart{}, fmt.Errorf("marshal provider attempt capabilities: %w", err)
		}
		normalized.PublicRouteEvidence = &evidence
		if normalized.ExecutionSnapshotHash != evidence.SnapshotHash {
			return normalizedProviderAttemptStart{}, fmt.Errorf("provider attempt Plan snapshot does not match public route evidence")
		}
		normalized.routeEvidenceHash = hash
		normalized.capabilityJSON = capabilityJSON
		return normalized, nil
	}

	evidence, err := executionevidence.NormalizeForDispatch(*req.RouteEvidence)
	if err != nil {
		return normalizedProviderAttemptStart{}, fmt.Errorf("provider attempt route evidence: %w", err)
	}
	switch evidence.AuthorityMode {
	case executionevidence.ModeLegacy, executionevidence.ModeVerify:
		if req.ChannelID != evidence.LegacyChannelID || req.ChannelKeyID != evidence.LegacyChannelKeyID {
			return normalizedProviderAttemptStart{}, fmt.Errorf("provider attempt legacy route identities do not match route evidence")
		}
	case executionevidence.ModeCanonical:
		if req.ChannelID != 0 || req.ChannelKeyID != 0 {
			return normalizedProviderAttemptStart{}, fmt.Errorf("canonical provider attempt cannot carry legacy route identities")
		}
	default:
		return normalizedProviderAttemptStart{}, fmt.Errorf("provider attempt has unsupported route authority")
	}
	if evidence.SnapshotHash != "" && normalized.ExecutionSnapshotHash != evidence.SnapshotHash {
		return normalizedProviderAttemptStart{}, fmt.Errorf("provider attempt Plan snapshot does not match route evidence")
	}
	hash, err := executionevidence.Digest(evidence)
	if err != nil {
		return normalizedProviderAttemptStart{}, fmt.Errorf("digest provider attempt route evidence: %w", err)
	}
	var capabilityJSON []byte
	if evidence.ExecutionContractSource == executionevidence.ContractSourceCanonicalProfile {
		capabilityJSON, err = json.Marshal(evidence.CapabilityEvidence)
		if err != nil {
			return normalizedProviderAttemptStart{}, fmt.Errorf("marshal provider attempt capabilities: %w", err)
		}
	}
	normalized.RouteEvidence = &evidence
	normalized.routeEvidenceHash = hash
	normalized.capabilityJSON = capabilityJSON
	return normalized, nil
}

func normalizeProviderAttemptCompletion(req ProviderAttemptCompletion) (ProviderAttemptCompletion, string, error) {
	req.RequestID = strings.TrimSpace(req.RequestID)
	req.AttemptID = strings.TrimSpace(req.AttemptID)
	req.Outcome = strings.TrimSpace(req.Outcome)
	req.FailureKind = strings.TrimSpace(req.FailureKind)
	req.UpstreamRequestID = strings.TrimSpace(req.UpstreamRequestID)
	if req.RequestID == "" || len(req.RequestID) > 64 || req.AttemptID == "" || len(req.AttemptID) > 64 || req.OrgID <= 0 {
		return req, "", fmt.Errorf("provider attempt completion requires bounded identities")
	}
	if req.HTTPStatus < 0 || req.HTTPStatus > 599 || (req.HTTPStatus > 0 && req.HTTPStatus < 100) {
		return req, "", fmt.Errorf("provider attempt completion has invalid HTTP status")
	}
	if len(req.FailureKind) > 40 || len(req.UpstreamRequestID) > 128 {
		return req, "", fmt.Errorf("provider attempt completion metadata is too long")
	}
	var certainty string
	switch req.Outcome {
	case ProviderAttemptOutcomeRejected:
		certainty = providerAttemptCertaintyRejected
		if req.HTTPStatus == 0 {
			return req, "", fmt.Errorf("rejected provider attempt requires an HTTP status")
		}
	case ProviderAttemptOutcomeResponseAccepted:
		certainty = providerAttemptCertaintyPending
		if req.HTTPStatus < 200 || req.HTTPStatus > 299 || req.RetryAuthorized {
			return req, "", fmt.Errorf("accepted provider response cannot authorize retry")
		}
	case ProviderAttemptOutcomeUnknown:
		certainty = providerAttemptCertaintyUnknown
		if req.HTTPStatus != 0 || req.RetryAuthorized {
			return req, "", fmt.Errorf("ambiguous provider outcome cannot authorize retry or claim a status")
		}
	case ProviderAttemptOutcomeResponseUnusable:
		certainty = providerAttemptCertaintyUnknown
		if req.HTTPStatus < 200 || req.HTTPStatus > 299 || req.RetryAuthorized {
			return req, "", fmt.Errorf("unusable provider response cannot authorize retry")
		}
	default:
		return req, "", fmt.Errorf("unsupported provider attempt outcome %q", req.Outcome)
	}
	return req, certainty, nil
}

// BeginProviderAttempt durably opens the financial attempt before dispatch.
// A second attempt cannot start while an earlier attempt is unresolved.
func (b *Billing) BeginProviderAttempt(ctx context.Context, request ProviderAttemptStart) error {
	if b == nil || b.db == nil {
		return fmt.Errorf("billing database is unavailable")
	}
	tx, err := b.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin provider attempt: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := b.BeginProviderAttemptTx(ctx, tx, request); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// BeginProviderAttemptTx opens the attempt inside the caller's transaction.
// The caller owns commit/rollback. This is the coordinator boundary that lets
// a probe-lease claim, health fence, and financial attempt become one atomic
// pre-dispatch decision without weakening the legacy public wrapper.
func (b *Billing) BeginProviderAttemptTx(ctx context.Context, tx pgx.Tx, request ProviderAttemptStart) error {
	_, err := b.beginProviderAttemptTx(ctx, tx, request, "")
	return err
}

// BeginProviderAttemptOwnedTx opens an execution-coordinator attempt under a
// one-call dispatch owner. Exact same-owner replay is safe after transaction
// ambiguity; another owner can never turn an existing receipt into a second
// dispatch authority. The returned boolean is true only for a new receipt.
func (b *Billing) BeginProviderAttemptOwnedTx(
	ctx context.Context,
	tx pgx.Tx,
	request ProviderAttemptStart,
	dispatchOwnerHash string,
) (bool, error) {
	if !validProviderAttemptDigest(dispatchOwnerHash) {
		return false, fmt.Errorf("provider attempt dispatch owner hash is invalid")
	}
	if request.RouteEvidence == nil && request.PublicRouteEvidence == nil {
		return false, fmt.Errorf("owned provider attempt requires versioned route evidence")
	}
	return b.beginProviderAttemptTx(ctx, tx, request, dispatchOwnerHash)
}

func (b *Billing) beginProviderAttemptTx(
	ctx context.Context,
	tx pgx.Tx,
	request ProviderAttemptStart,
	dispatchOwnerHash string,
) (bool, error) {
	if tx == nil {
		return false, fmt.Errorf("billing transaction is unavailable")
	}
	req, err := normalizeProviderAttemptStart(request)
	if err != nil {
		return false, err
	}

	record, err := reservationForUpdate(ctx, tx, req.RequestID, req.OrgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, fmt.Errorf("begin provider attempt: reservation not found")
		}
		return false, fmt.Errorf("lock provider attempt reservation: %w", err)
	}
	if record.Status != "frozen" {
		return false, fmt.Errorf("begin provider attempt: reservation is %s", record.Status)
	}
	if nullableString(record.ExecutionSnapshotHash) != req.ExecutionSnapshotHash {
		return false, ErrProviderAttemptConflict
	}
	if record.APIKeyID.Valid && record.APIKeyID.Int64 != req.APIKeyID {
		return false, ErrProviderAttemptConflict
	}
	req.AccessCredentialID = nullableID(record.AccessCredentialID)
	req.CredentialRef = nullableString(record.CredentialRef)
	req.AccessGrantID = nullableID(record.AccessGrantID)

	existing, err := providerAttemptForUpdate(ctx, tx, req.OrgID, req.RequestID, req.AttemptID)
	if err == nil {
		if !providerAttemptStartMatches(existing, req) || existing.State != providerAttemptStateDispatchStarted {
			return false, ErrProviderAttemptConflict
		}
		if dispatchOwnerHash == "" {
			if existing.DispatchOwnerHash.Valid {
				return false, ErrProviderAttemptConflict
			}
		} else if !existing.DispatchOwnerHash.Valid || existing.DispatchOwnerHash.String != dispatchOwnerHash {
			return false, ErrProviderAttemptConflict
		}
		return false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("lock provider attempt: %w", err)
	}

	var unresolved bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (
		    SELECT 1 FROM billing_provider_attempts
		    WHERE org_id = $1 AND request_id = $2
		      AND state IN (
		          'dispatch_started', 'response_accepted', 'outcome_unknown',
		          'response_unusable', 'funding_pending',
		          'delivery_pending', 'delivery_failed', 'output_withheld'
		      )
		)`, req.OrgID, req.RequestID,
	).Scan(&unresolved); err != nil {
		return false, fmt.Errorf("check unresolved provider attempt: %w", err)
	}
	if unresolved {
		return false, ErrProviderAttemptUnresolved
	}

	values := providerAttemptEvidenceValues(req)
	query := `INSERT INTO billing_provider_attempts (
		    org_id, request_id, attempt_id, api_key_id,
		    project_id,access_credential_id,credential_ref,access_grant_id,
		    channel_id,channel_key_id,deployment_id,
		    route_sequence,route_total,endpoint,provider,channel_name,
		    route_evidence_version,route_authority_mode,route_execution_contract_source,
		    route_evidence_hash,route_public_model_id,route_connection_id,
		    route_credential_binding_id,route_connection_revision,route_deployment_revision,
		    route_circuit_revision,route_credential_state_revision,route_credential_linked_at,
		    route_model_operation_id,route_model_operation_revision,
		    route_protocol_profile_id,route_protocol_profile_version,
		    route_operation_evidence_state,route_operation_evidence_ref,route_operation_verified_at,
		    route_capability_evidence,route_capability_evidence_hash,
		    route_credential_probe_id,route_credential_probe_tested_at,
		    route_runtime_release_id,route_runtime_release_ref,route_runtime_stage,
		    route_slot_version,route_snapshot_hash,
		    route_legacy_routing_revision,route_legacy_circuit_revision,
		    route_legacy_credential_state_revision,route_requires_probe,route_probe_lease_token_hash,
		    route_public_execution_binding_id,route_delivery_contract_version,
		    route_charge_contract_version,route_idempotency_contract_version,
		    execution_snapshot_hash,dispatch_owner_hash
		 ) VALUES ($1,$2,$3,$4,NULLIF($5,0),NULLIF($6,0),NULLIF($7,''),NULLIF($8,0),
		           NULLIF($9,0),NULLIF($10,0),NULLIF($11,0),$12,$13,$14,$15,$16,
		           $17,$18,$19,$20,NULLIF($21,0),NULLIF($22,0),NULLIF($23,0),
		           NULLIF($24,0),NULLIF($25,0),NULLIF($26,0),NULLIF($27,0),$28,
		           $29,NULLIF($30,0),$31,$32,$33,$34,$35,$36,$37,
		           NULLIF($38,0),$39,NULLIF($40,0),$41,$42,NULLIF($43,0),$44,
		           NULLIF($45,0),NULLIF($46,0),NULLIF($47,0),$48,$49,
		           NULLIF($50,0),$51,$52,$53,NULLIF($54,''),NULLIF($55,''))`
	args := []any{req.OrgID, req.RequestID, req.AttemptID, req.APIKeyID,
		record.ProjectID, req.AccessCredentialID, req.CredentialRef, req.AccessGrantID,
		req.ChannelID, req.ChannelKeyID, values.deploymentID,
		req.RouteSequence, req.RouteTotal, req.Endpoint, req.Provider, req.Channel,
		values.contractVersion, values.authorityMode, values.contractSource,
		values.evidenceHash, values.publicModelID, values.connectionID, values.credentialBindingID,
		values.connectionRevision, values.deploymentRevision, values.circuitRevision,
		values.credentialStateRevision, values.credentialLinkedAt, values.modelOperationID,
		values.operationRevision, values.protocolProfileID, values.protocolProfileVersion,
		values.operationEvidenceState, values.operationEvidenceRef, values.operationVerifiedAt,
		values.capabilityJSON, values.capabilityEvidenceHash, values.credentialProbeID,
		values.credentialProbeTestedAt, values.runtimeReleaseID, values.runtimeReleaseRef,
		values.runtimeStage, values.slotVersion, values.snapshotHash, values.legacyRoutingRevision,
		values.legacyCircuitRevision, values.legacyCredentialStateRevision, values.requiresProbe,
		values.probeLeaseTokenHash, values.publicExecutionBindingID,
		values.deliveryContractVersion, values.chargeContractVersion,
		values.idempotencyContractVersion, req.ExecutionSnapshotHash, dispatchOwnerHash,
	}
	if req.RuntimeInstanceID != "" {
		query = strings.Replace(query, "execution_snapshot_hash,dispatch_owner_hash", "execution_snapshot_hash,dispatch_owner_hash,runtime_instance_id", 1)
		query = strings.Replace(query, "NULLIF($55,''))", "NULLIF($55,''),$56)", 1)
		args = append(args, req.RuntimeInstanceID)
	}
	if _, err := tx.Exec(ctx, query, args...); err != nil {
		return false, fmt.Errorf("insert provider attempt: %w", err)
	}
	return true, nil
}

// CompleteProviderAttempt advances only monotonic evidence transitions. An
// ambiguous dispatch or unusable 2xx response atomically moves the reservation
// into a non-cancellable provider-cost hold.
func (b *Billing) CompleteProviderAttempt(ctx context.Context, request ProviderAttemptCompletion) error {
	if b == nil || b.db == nil {
		return fmt.Errorf("billing database is unavailable")
	}
	req, certainty, err := normalizeProviderAttemptCompletion(request)
	if err != nil {
		return err
	}
	tx, err := b.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("complete provider attempt: %w", err)
	}
	defer tx.Rollback(ctx)

	reservation, err := reservationForUpdate(ctx, tx, req.RequestID, req.OrgID)
	if err != nil {
		return fmt.Errorf("lock provider attempt reservation: %w", err)
	}
	attempt, err := providerAttemptForUpdate(ctx, tx, req.OrgID, req.RequestID, req.AttemptID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("complete provider attempt: attempt not found")
		}
		return fmt.Errorf("lock provider attempt: %w", err)
	}

	if providerAttemptCompletionMatches(attempt, req) {
		return tx.Commit(ctx)
	}
	allowed := attempt.State == providerAttemptStateDispatchStarted ||
		(attempt.State == ProviderAttemptOutcomeResponseAccepted && req.Outcome == ProviderAttemptOutcomeResponseUnusable)
	if !allowed {
		return ErrProviderAttemptConflict
	}
	if reservation.Status != "frozen" && reservation.Status != "settlement_pending" {
		return fmt.Errorf("complete provider attempt: reservation is %s", reservation.Status)
	}

	if _, err := tx.Exec(ctx,
		`UPDATE billing_provider_attempts
		 SET state = $4::varchar(32),
		     outcome_certainty = $5::varchar(24),
		     retry_authorized = $6,
		     http_status = NULLIF($7, 0),
		     failure_kind = NULLIF($8::text, ''),
		     upstream_request_id = NULLIF($9::text, ''),
		     response_received_at = CASE WHEN $7 > 0 THEN COALESCE(response_received_at, NOW()) ELSE response_received_at END,
		     financial_resolved_at = CASE WHEN $4::text = 'rejected' THEN COALESCE(financial_resolved_at, NOW()) ELSE NULL END,
		     updated_at = NOW()
		 WHERE org_id = $1 AND request_id = $2 AND attempt_id = $3`,
		req.OrgID, req.RequestID, req.AttemptID, req.Outcome, certainty,
		req.RetryAuthorized, req.HTTPStatus, req.FailureKind, req.UpstreamRequestID,
	); err != nil {
		return fmt.Errorf("update provider attempt: %w", err)
	}

	if req.Outcome == ProviderAttemptOutcomeUnknown || req.Outcome == ProviderAttemptOutcomeResponseUnusable {
		reason := pendingReasonAmbiguousAttempt
		if req.Outcome == ProviderAttemptOutcomeResponseUnusable {
			reason = pendingReasonResponseUnusable
		}
		if _, err := tx.Exec(ctx,
			`UPDATE freeze_idempotency
			 SET status = 'settlement_pending',
			     provider_cost_pending_at = COALESCE(provider_cost_pending_at, NOW()),
			     settlement_pending_reason = $3,
			     settlement_claimed_until = NULL,
			     settlement_next_attempt_at = NULL,
			     updated_at = NOW()
			 WHERE org_id = $1 AND request_id = $2
			   AND status IN ('frozen', 'settlement_pending')`,
			req.OrgID, req.RequestID, reason,
		); err != nil {
			return fmt.Errorf("hold unresolved provider attempt: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	b.observeCommitted(ctx, CommittedObservation{Kind: "provider_result", OrgID: req.OrgID, RequestID: req.RequestID, AttemptID: req.AttemptID, Outcome: req.Outcome})
	return nil
}

func providerAttemptForUpdate(ctx context.Context, tx pgx.Tx, orgID int64, requestID, attemptID string) (providerAttemptRecord, error) {
	var record providerAttemptRecord
	err := tx.QueryRow(ctx,
		`SELECT api_key_id,access_credential_id,credential_ref,access_grant_id,
		        channel_id, channel_key_id, route_sequence, route_total,
		        endpoint, provider, channel_name, state, outcome_certainty, retry_authorized,
		        http_status, failure_kind, upstream_request_id, calculated_cost_micros, charged_cost_micros,
		        terminal_transaction_id,route_evidence_version,route_evidence_hash,
		        execution_snapshot_hash,dispatch_owner_hash,COALESCE(to_jsonb(billing_provider_attempts)->>'runtime_instance_id','')
		 FROM billing_provider_attempts
		 WHERE org_id = $1 AND request_id = $2 AND attempt_id = $3
		 FOR UPDATE`, orgID, requestID, attemptID,
	).Scan(
		&record.APIKeyID, &record.AccessCredentialID, &record.CredentialRef, &record.AccessGrantID,
		&record.ChannelID, &record.ChannelKeyID, &record.RouteSequence, &record.RouteTotal,
		&record.Endpoint, &record.Provider, &record.Channel, &record.State, &record.OutcomeCertainty,
		&record.RetryAuthorized, &record.HTTPStatus, &record.FailureKind,
		&record.UpstreamRequestID, &record.CalculatedCostMicros, &record.ChargedCostMicros,
		&record.TerminalTransactionID, &record.RouteEvidenceVersion, &record.RouteEvidenceHash,
		&record.ExecutionSnapshotHash,
		&record.DispatchOwnerHash, &record.RuntimeInstanceID,
	)
	return record, err
}

func providerAttemptStartMatches(record providerAttemptRecord, req normalizedProviderAttemptStart) bool {
	identityMatches := record.RuntimeInstanceID == req.RuntimeInstanceID && record.APIKeyID == req.APIKeyID && nullableID(record.AccessCredentialID) == req.AccessCredentialID &&
		nullableString(record.CredentialRef) == req.CredentialRef && nullableID(record.AccessGrantID) == req.AccessGrantID &&
		nullableID(record.ChannelID) == req.ChannelID && nullableID(record.ChannelKeyID) == req.ChannelKeyID &&
		record.RouteSequence == req.RouteSequence && record.RouteTotal == req.RouteTotal &&
		record.Endpoint == req.Endpoint && record.Provider == req.Provider && record.Channel == req.Channel
	if !identityMatches {
		return false
	}
	if nullableString(record.ExecutionSnapshotHash) != req.ExecutionSnapshotHash {
		return false
	}
	if req.RouteEvidence == nil && req.PublicRouteEvidence == nil {
		return !record.RouteEvidenceVersion.Valid && !record.RouteEvidenceHash.Valid
	}
	version := executionevidence.ContractVersionV1
	if req.PublicRouteEvidence != nil {
		version = executionevidence.ContractVersionV2
	}
	return record.RouteEvidenceVersion.Valid && record.RouteEvidenceVersion.String == version &&
		record.RouteEvidenceHash.Valid && record.RouteEvidenceHash.String == req.routeEvidenceHash
}

func providerAttemptCompletionMatches(record providerAttemptRecord, req ProviderAttemptCompletion) bool {
	return record.State == req.Outcome && record.RetryAuthorized == req.RetryAuthorized &&
		nullableInt32Equals(record.HTTPStatus, req.HTTPStatus) &&
		nullableStringEquals(record.FailureKind, req.FailureKind) &&
		nullableStringEquals(record.UpstreamRequestID, req.UpstreamRequestID)
}

func nullableInt32Equals(value sql.NullInt32, expected int) bool {
	return value.Valid == (expected != 0) && (!value.Valid || int(value.Int32) == expected)
}

func nullableStringEquals(value sql.NullString, expected string) bool {
	return value.Valid == (expected != "") && (!value.Valid || value.String == expected)
}

func markProviderAttemptFundingPendingTx(ctx context.Context, tx pgx.Tx, req SettleRequest, calculatedCost int64) error {
	return updateProviderAttemptFinancialStateTx(
		ctx, tx, req, providerAttemptStateFundingPending, calculatedCost, 0, 0,
	)
}

func markProviderAttemptSettledTx(ctx context.Context, tx pgx.Tx, req SettleRequest, calculatedCost, chargedCost int64, transactionID int64) error {
	return updateProviderAttemptFinancialStateTx(
		ctx, tx, req, providerAttemptStateSettled, calculatedCost, chargedCost, transactionID,
	)
}

func updateProviderAttemptFinancialStateTx(
	ctx context.Context,
	tx pgx.Tx,
	req SettleRequest,
	state string,
	calculatedCost int64,
	chargedCost int64,
	transactionID int64,
) error {
	result, err := tx.Exec(ctx,
		`UPDATE billing_provider_attempts
		 SET state = $4::varchar(32),
		     outcome_certainty = $5::varchar(24),
		     retry_authorized = FALSE,
		     calculated_cost_micros = $6,
		     charged_cost_micros = $7,
		     terminal_transaction_id = NULLIF($8::bigint, 0),
		     financial_resolved_at = CASE WHEN $4::text = 'settled' THEN COALESCE(financial_resolved_at, NOW()) ELSE NULL END,
		     updated_at = NOW()
		 WHERE org_id = $1 AND request_id = $2
		   AND ($3::text = '' OR attempt_id = $3::text)
		   AND state IN (
		       'dispatch_started', 'response_accepted', 'outcome_unknown',
		       'response_unusable', 'funding_pending',
		       'delivery_pending', 'delivery_failed', 'output_withheld'
		   )`,
		req.OrgID, req.RequestID, req.AttemptID, state, providerAttemptCertaintyKnownUsage,
		calculatedCost, chargedCost, transactionID,
	)
	if err != nil {
		return fmt.Errorf("mark provider attempt %s: %w", state, err)
	}
	rows := result.RowsAffected()
	if req.AttemptID != "" && rows != 1 {
		return fmt.Errorf("mark provider attempt %s: %w", state, ErrProviderAttemptConflict)
	}
	if req.AttemptID == "" && rows > 1 {
		return fmt.Errorf("mark provider attempt %s: multiple unresolved attempts", state)
	}
	return nil
}

// holdUnresolvedProviderAttemptTx prevents generic cleanup paths from
// refunding a request after a possible provider-side charge. The reservation
// lock is already held, so the attempt check and hold transition are atomic.
func holdUnresolvedProviderAttemptTx(ctx context.Context, tx pgx.Tx, requestID string, orgID int64) (bool, error) {
	var state string
	err := tx.QueryRow(ctx,
		`SELECT state
		 FROM billing_provider_attempts
		 WHERE org_id = $1 AND request_id = $2
		   AND state IN (
		       'dispatch_started', 'response_accepted', 'outcome_unknown',
		       'response_unusable', 'funding_pending',
		       'delivery_pending', 'delivery_failed', 'output_withheld'
		   )
		 ORDER BY CASE state
		     WHEN 'outcome_unknown' THEN 1
		     WHEN 'response_unusable' THEN 2
		     WHEN 'delivery_failed' THEN 3
		     WHEN 'output_withheld' THEN 3
		     WHEN 'delivery_pending' THEN 4
		     ELSE 5
		 END, started_at
		 LIMIT 1
		 FOR UPDATE`, orgID, requestID,
	).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("lock unresolved provider attempt: %w", err)
	}
	reason := pendingReasonProviderAttemptUnresolved
	if state == ProviderAttemptOutcomeUnknown || state == providerAttemptStateDispatchStarted {
		reason = pendingReasonAmbiguousAttempt
	} else if state == ProviderAttemptOutcomeResponseUnusable {
		reason = pendingReasonResponseUnusable
	}
	result, err := tx.Exec(ctx,
		`UPDATE freeze_idempotency
		 SET status = 'settlement_pending',
		     provider_cost_pending_at = COALESCE(provider_cost_pending_at, NOW()),
		     settlement_pending_reason = $3,
		     settlement_claimed_until = NULL,
		     settlement_next_attempt_at = NULL,
		     updated_at = NOW()
		 WHERE org_id = $1 AND request_id = $2 AND status = 'frozen'`,
		orgID, requestID, reason,
	)
	if err != nil {
		return false, fmt.Errorf("persist provider-attempt hold: %w", err)
	}
	if result.RowsAffected() != 1 {
		return false, fmt.Errorf("persist provider-attempt hold: reservation state changed")
	}
	return true, nil
}
