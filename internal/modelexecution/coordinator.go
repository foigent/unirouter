package modelexecution

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/executionevidence"
	"github.com/uniroute/unirouter/internal/modelqualification"
	"github.com/uniroute/unirouter/internal/modelrouting"
)

var credentialRefPattern = regexp.MustCompile(`^cred_[a-z0-9][a-z0-9_-]{7,66}$`)

// Authorize revalidates one already-selected route and opens its immutable
// provider-attempt receipt in one read-write Repeatable Read transaction. A
// successful commit is the sole authority returned to the caller to dispatch.
func (c *Coordinator) Authorize(
	ctx context.Context,
	plan modelrouting.Plan,
	route modelrouting.Route,
	identity AttemptIdentity,
) (Decision, error) {
	if c == nil || c.database == nil || c.ledger == nil || c.newToken == nil {
		return Decision{}, ErrUnavailable
	}
	// Public operations remain unreachable through the exported coordinator
	// boundary until the common executor is wired in the same release. Keep
	// this before request preparation, token generation, DB access, and ledger
	// access so neither an M75 nor an M77 route can bypass the dispatch fence.
	if modelqualification.IsQualificationRequired(plan.ModelOperationID) || route.PublicEvidence != nil {
		return Decision{}, ErrDispatchNotWired
	}
	request, err := prepareRequest(plan, route, identity)
	if err != nil {
		return Decision{}, err
	}
	request, err = c.prepareDispatchCapabilities(request)
	if err != nil {
		return Decision{}, err
	}

	return runBoundedTransaction(ctx, func(operationCtx context.Context) (Decision, transactionRetry, error) {
		return c.authorizeOnce(operationCtx, request)
	})
}

// AuthorizePublic is the only exported dispatch boundary for the M77 public
// non-stream operations. It accepts neither a legacy/V1 route nor an operation
// outside the compiled Responses/Messages registry. The exact V2 bundle is
// normalized before any token, database, ledger, or provider-side capability
// can be created.
func (c *Coordinator) AuthorizePublic(
	ctx context.Context,
	plan modelrouting.Plan,
	route modelrouting.Route,
	identity AttemptIdentity,
) (Decision, error) {
	if c == nil || c.database == nil || c.ledger == nil || c.newToken == nil {
		return Decision{}, ErrUnavailable
	}
	if !modelqualification.IsQualificationRequired(plan.ModelOperationID) || route.PublicEvidence == nil {
		return Decision{}, fmt.Errorf("%w: public M77 execution authority required", ErrInvalid)
	}
	request, err := preparePublicRequest(plan, route, identity)
	if err != nil {
		return Decision{}, err
	}
	request, err = c.prepareDispatchCapabilities(request)
	if err != nil {
		return Decision{}, err
	}
	return runBoundedTransaction(ctx, func(operationCtx context.Context) (Decision, transactionRetry, error) {
		return c.authorizePublicOnce(operationCtx, request)
	})
}

// authorizePublicOnce keeps the exact attempt-first M77 transaction contract
// behind AuthorizePublic; callers cannot invoke the transactional primitive
// without crossing the V2 preflight boundary above.
func (c *Coordinator) authorizePublicOnce(
	ctx context.Context,
	request preparedRequest,
) (Decision, transactionRetry, error) {
	if request.publicEvidence == nil || request.dispatchPublicEvidence == nil {
		return Decision{}, retryNone, fmt.Errorf("%w: public dispatch evidence", ErrInvalid)
	}
	return c.authorizeOnce(ctx, request)
}

func (c *Coordinator) authorizeOnce(ctx context.Context, request preparedRequest) (Decision, transactionRetry, error) {
	tx, err := c.database.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadWrite})
	if err != nil {
		return Decision{}, transactionRetryFor(err), fmt.Errorf("%w: begin dispatch authority: %v", ErrUnavailable, err)
	}
	defer tx.Rollback(ctx)

	start := providerAttemptStart(request)
	created, err := c.ledger.BeginProviderAttemptOwnedTx(ctx, tx, start, request.dispatchOwnerHash)
	if errors.Is(err, billing.ErrProviderAttemptConflict) {
		var dbNow time.Time
		if clockErr := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&dbNow); clockErr != nil {
			return Decision{}, transactionRetryFor(clockErr), wrapDatabaseError("read rejected dispatch clock", clockErr)
		}
		return Decision{Status: Stale, DatabaseTime: dbNow.UTC()}, retryNone, nil
	}
	if err != nil {
		return Decision{}, transactionRetryFor(err), fmt.Errorf("open owned provider-attempt receipt: %w", err)
	}
	if !created {
		var dbNow time.Time
		if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&dbNow); err != nil {
			return Decision{}, transactionRetryFor(err), wrapDatabaseError("read dispatch replay clock", err)
		}
		// The exact owned receipt proves that the ambiguous first transaction
		// committed its route claim and attempt atomically. This recovery
		// transaction is read-only after taking validation locks, so rollback is
		// sufficient and avoids manufacturing a second commit ambiguity.
		return request.appliedDecision(dbNow.UTC()), retryNone, nil
	}

	result, err := c.revalidateAndClaim(ctx, tx, request)
	if err != nil {
		return Decision{}, transactionRetryFor(err), wrapDatabaseError("revalidate dispatch authority", err)
	}
	if result.status != Applied {
		return Decision{Status: result.status, DatabaseTime: result.databaseTime}, retryNone, nil
	}

	if err := tx.Commit(ctx); err != nil {
		return Decision{}, retryCommitAmbiguity, wrapDatabaseError("commit dispatch authority", err)
	}
	return request.appliedDecision(result.databaseTime), retryNone, nil
}

func (c *Coordinator) prepareDispatchCapabilities(request preparedRequest) (preparedRequest, error) {
	owner, err := c.newToken()
	if err != nil || !validEphemeralToken(owner) {
		return preparedRequest{}, fmt.Errorf("%w: generate dispatch owner", ErrUnavailable)
	}
	request.dispatchOwnerToken = owner
	request.dispatchOwnerHash = probeTokenHash(owner)
	if request.route.RequiresProbe {
		request.probeToken, err = c.newToken()
		if err != nil || !validEphemeralToken(request.probeToken) {
			return preparedRequest{}, fmt.Errorf("%w: generate probe owner", ErrUnavailable)
		}
	}

	evidence := request.evidence
	if request.probeToken != "" {
		evidence.ProbeLeaseTokenHash = probeTokenHash(request.probeToken)
	}
	if request.publicEvidence != nil {
		public := *request.publicEvidence
		public.EvidenceV1 = evidence
		public.ContractVersion = executionevidence.ContractVersionV2
		public, err = executionevidence.NormalizeV2ForDispatch(public)
		if err != nil {
			return preparedRequest{}, fmt.Errorf("%w: public dispatch evidence: %v", ErrInvalid, err)
		}
		evidenceHash, digestErr := executionevidence.DigestV2(public)
		if digestErr != nil {
			return preparedRequest{}, fmt.Errorf("%w: digest public dispatch evidence: %v", ErrInvalid, digestErr)
		}
		request.dispatchEvidence = evidence
		request.dispatchPublicEvidence = &public
		request.dispatchEvidenceHash = evidenceHash
		return request, nil
	}
	evidence, err = executionevidence.NormalizeForDispatch(evidence)
	if err != nil {
		return preparedRequest{}, fmt.Errorf("%w: dispatch evidence: %v", ErrInvalid, err)
	}
	evidenceHash, err := executionevidence.Digest(evidence)
	if err != nil {
		return preparedRequest{}, fmt.Errorf("%w: digest dispatch evidence: %v", ErrInvalid, err)
	}
	request.dispatchEvidence = evidence
	request.dispatchEvidenceHash = evidenceHash
	return request, nil
}

func (request preparedRequest) appliedDecision(databaseTime time.Time) Decision {
	decision := Decision{
		Status: Applied, Evidence: request.dispatchEvidence, EvidenceHash: request.dispatchEvidenceHash,
		ProbeLeaseToken: request.probeToken, DispatchOwnerToken: request.dispatchOwnerToken,
		DatabaseTime: databaseTime,
		ProviderAttemptKey: ProviderAttemptKey{
			OrgID: request.identity.OrgID, RequestID: request.identity.RequestID, AttemptID: request.identity.AttemptID,
		},
	}
	if request.dispatchPublicEvidence != nil {
		public := *request.dispatchPublicEvidence
		decision.Evidence = executionevidence.EvidenceV1{}
		decision.PublicEvidence = &public
	}
	return decision
}

func providerAttemptStart(request preparedRequest) billing.ProviderAttemptStart {
	start := billing.ProviderAttemptStart{
		RequestID: request.identity.RequestID, AttemptID: request.identity.AttemptID,
		OrgID: request.identity.OrgID, APIKeyID: request.identity.APIKeyID,
		AccessCredentialID: request.identity.AccessCredentialID,
		CredentialRef:      request.identity.CredentialRef, AccessGrantID: request.identity.AccessGrantID,
		RouteSequence: request.identity.RouteSequence, RouteTotal: request.identity.RouteTotal,
		Endpoint: request.identity.Endpoint, Provider: request.route.Provider,
		Channel: request.route.ConnectionName, ExecutionSnapshotHash: request.plan.SnapshotHash,
	}
	if request.dispatchPublicEvidence != nil {
		public := *request.dispatchPublicEvidence
		start.PublicRouteEvidence = &public
		return start
	}
	evidence := request.dispatchEvidence
	start.RouteEvidence = &evidence
	if evidence.AuthorityMode != executionevidence.ModeCanonical {
		start.ChannelID = evidence.LegacyChannelID
		start.ChannelKeyID = evidence.LegacyChannelKeyID
	}
	return start
}

func prepareRequest(plan modelrouting.Plan, route modelrouting.Route, identity AttemptIdentity) (preparedRequest, error) {
	if route.PublicEvidence != nil {
		return preparedRequest{}, fmt.Errorf("%w: route evidence versions are mutually exclusive", ErrInvalid)
	}
	evidence, err := executionevidence.Normalize(route.Evidence)
	if err != nil {
		return preparedRequest{}, fmt.Errorf("%w: route evidence: %v", ErrInvalid, err)
	}
	return prepareRequestWithEvidence(plan, route, identity, evidence, nil)
}

func preparePublicRequest(plan modelrouting.Plan, route modelrouting.Route, identity AttemptIdentity) (preparedRequest, error) {
	if route.PublicEvidence == nil || !reflect.DeepEqual(route.Evidence, executionevidence.EvidenceV1{}) {
		return preparedRequest{}, fmt.Errorf("%w: public route requires explicit mutually exclusive V2 evidence", ErrInvalid)
	}
	public, err := executionevidence.NormalizeV2(*route.PublicEvidence)
	if err != nil {
		return preparedRequest{}, fmt.Errorf("%w: public route evidence: %v", ErrInvalid, err)
	}
	evidence, err := public.RouteEvidenceV1()
	if err != nil {
		return preparedRequest{}, fmt.Errorf("%w: public route base evidence: %v", ErrInvalid, err)
	}
	compiled, err := modelqualification.CompiledPublicBindingFor(modelqualification.Binding{
		OperationID: evidence.ModelOperationID, PublicPath: route.PublicPath,
		ProtocolProfileID: evidence.ProtocolProfileID, ProtocolProfileVersion: evidence.ProtocolProfileVersion,
		ExecutorContractVersion: route.ExecutorContractVersion,
	})
	if err != nil || route.PublicPath != compiled.PublicPath ||
		evidence.ProtocolProfileID != compiled.ProfileID ||
		evidence.ProtocolProfileVersion != compiled.ProfileVersion ||
		route.ProtocolProfileID != compiled.ProfileID ||
		route.ProtocolProfileVersion != compiled.ProfileVersion ||
		route.ExecutorContractVersion != compiled.ExecutorContractVersion ||
		public.DeliveryContractVersion != compiled.DeliveryContractVersion ||
		public.ChargeContractVersion != compiled.ChargeContractVersion ||
		public.IdempotencyContractVersion != compiled.IdempotencyContractVersion ||
		route.ProductionApprovalID <= 0 {
		return preparedRequest{}, fmt.Errorf("%w: public route contract", ErrInvalid)
	}
	prepared, err := prepareRequestWithEvidence(plan, route, identity, evidence, &public)
	if err != nil {
		return preparedRequest{}, err
	}
	if prepared.identity.Endpoint != route.PublicPath {
		return preparedRequest{}, fmt.Errorf("%w: public endpoint and route disagree", ErrInvalid)
	}
	return prepared, nil
}

func prepareRequestWithEvidence(
	plan modelrouting.Plan,
	route modelrouting.Route,
	identity AttemptIdentity,
	evidence executionevidence.EvidenceV1,
	publicEvidence *executionevidence.EvidenceV2,
) (preparedRequest, error) {
	identity.RequestID = strings.TrimSpace(identity.RequestID)
	identity.AttemptID = strings.TrimSpace(identity.AttemptID)
	identity.Endpoint = strings.TrimSpace(identity.Endpoint)
	identity.CredentialRef = strings.TrimSpace(identity.CredentialRef)
	if identity.OrgID <= 0 || identity.APIKeyID <= 0 || identity.RequestID == "" || len(identity.RequestID) > 64 ||
		identity.AttemptID == "" || len(identity.AttemptID) > 64 || identity.Endpoint == "" || len(identity.Endpoint) > 64 ||
		identity.RouteSequence <= 0 || identity.RouteTotal <= 0 || identity.RouteSequence > identity.RouteTotal ||
		strings.ContainsAny(identity.RequestID+identity.AttemptID+identity.Endpoint, "\x00\r\n") {
		return preparedRequest{}, fmt.Errorf("%w: provider-attempt identity", ErrInvalid)
	}
	accessEmpty := identity.AccessCredentialID == 0 && identity.CredentialRef == "" && identity.AccessGrantID == 0
	accessPresent := identity.AccessCredentialID > 0 && identity.AccessGrantID > 0 && credentialRefPattern.MatchString(identity.CredentialRef)
	if !accessEmpty && !accessPresent {
		return preparedRequest{}, fmt.Errorf("%w: access credential identity must be wholly absent or complete", ErrInvalid)
	}
	if evidence.AuthorityMode == executionevidence.ModeVerify &&
		(evidence.ConnectionRevision != evidence.LegacyRoutingRevision ||
			evidence.DeploymentRevision != evidence.LegacyRoutingRevision ||
			evidence.CircuitRevision != evidence.LegacyCircuitRevision ||
			evidence.CredentialStateRevision != evidence.LegacyCredentialStateRevision) {
		return preparedRequest{}, fmt.Errorf("%w: verify evidence projections are not revision-equivalent", ErrInvalid)
	}
	if evidence.ProbeLeaseTokenHash != "" || route.RequiresProbe != evidence.RequiresProbe ||
		plan.AuthorityMode != evidence.AuthorityMode || plan.ModelOperationID != evidence.ModelOperationID ||
		plan.PublicModel == "" || plan.SnapshotHash == "" || len(plan.Routes) == 0 ||
		strings.TrimSpace(route.Provider) == "" || strings.TrimSpace(route.ConnectionName) == "" {
		return preparedRequest{}, fmt.Errorf("%w: plan and route evidence disagree", ErrInvalid)
	}
	if evidence.AuthorityMode != executionevidence.ModeLegacy {
		if identity.OrgID <= 0 || plan.PublicModelID != evidence.PublicModelID || plan.SnapshotHash != evidence.SnapshotHash ||
			plan.RuntimeReleaseID != evidence.RuntimeReleaseID || plan.RuntimeReleaseRef != evidence.RuntimeReleaseRef ||
			plan.RuntimeStage != evidence.RuntimeStage || plan.SlotVersion != evidence.SlotVersion {
			return preparedRequest{}, fmt.Errorf("%w: canonical plan metadata disagree", ErrInvalid)
		}
	}
	if !routeBelongsToPlan(plan, route) {
		return preparedRequest{}, fmt.Errorf("%w: route is not a member of the plan", ErrInvalid)
	}

	underlying := operationIdentity{id: evidence.ModelOperationID, profileID: evidence.ProtocolProfileID, profileVersion: evidence.ProtocolProfileVersion}
	if evidence.ExecutionContractSource == executionevidence.ContractSourceLegacyAdapter && evidence.AuthorityMode != executionevidence.ModeLegacy {
		underlying = operationIdentity{id: "legacy.any.v1", profileID: "legacy.channel_model_probe", profileVersion: "v1"}
	}
	return preparedRequest{
		plan: plan, route: route, identity: identity, evidence: evidence,
		publicEvidence: publicEvidence, underlying: underlying,
	}, nil
}

func routeBelongsToPlan(plan modelrouting.Plan, route modelrouting.Route) bool {
	wanted, err := routeEvidenceDigest(route)
	if err != nil {
		return false
	}
	for _, candidate := range plan.Routes {
		digest, digestErr := routeEvidenceDigest(candidate)
		if digestErr == nil && subtle.ConstantTimeCompare([]byte(digest), []byte(wanted)) == 1 &&
			candidate.ConnectionName == route.ConnectionName && candidate.Provider == route.Provider &&
			candidate.BaseURL == route.BaseURL && candidate.UpstreamModel == route.UpstreamModel &&
			candidate.AdapterType == route.AdapterType && sameJSON(candidate.AdapterConfig, route.AdapterConfig) &&
			candidate.ProtocolProfileID == route.ProtocolProfileID &&
			candidate.ProtocolProfileVersion == route.ProtocolProfileVersion &&
			candidate.PublicPath == route.PublicPath &&
			candidate.ExecutorContractVersion == route.ExecutorContractVersion &&
			candidate.ProductionApprovalID == route.ProductionApprovalID &&
			candidate.Priority == route.Priority && candidate.Weight == route.Weight &&
			candidate.MaxContext == route.MaxContext && candidate.DefaultMaxOutput == route.DefaultMaxOutput &&
			candidate.RequiresProbe == route.RequiresProbe &&
			subtle.ConstantTimeCompare([]byte(candidate.EncryptedSecret), []byte(route.EncryptedSecret)) == 1 {
			return true
		}
	}
	return false
}

func routeEvidenceDigest(route modelrouting.Route) (string, error) {
	if route.PublicEvidence != nil {
		if !reflect.DeepEqual(route.Evidence, executionevidence.EvidenceV1{}) {
			return "", fmt.Errorf("%w: route evidence versions are mutually exclusive", ErrInvalid)
		}
		return executionevidence.DigestV2(*route.PublicEvidence)
	}
	return executionevidence.Digest(route.Evidence)
}

func randomProbeToken() (string, error) {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate probe lease token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func validEphemeralToken(value string) bool {
	return value != "" && len(value) <= 80 && !strings.ContainsAny(value, "\x00\r\n")
}

func probeTokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func retryableTransaction(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && (pgErr.Code == "40001" || pgErr.Code == "40P01")
}

func wrapDatabaseError(action string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %s: %v", ErrUnavailable, action, err)
}
