package accessprovisioning

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/accesscredential"
	"github.com/uniroute/unirouter/internal/accessevidence"
	"github.com/uniroute/unirouter/internal/accessgrantlifecycle"
	"github.com/uniroute/unirouter/internal/accessscope"
	"github.com/uniroute/unirouter/internal/accesssponsor"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/db"
)

const (
	credentialInsertMaxAttempts = 5
)

type CommitAuthorizer interface {
	Reauthorize(context.Context, pgx.Tx, AuthorizationRequirement) (Actor, error)
}

type CommitAuthorizerFunc func(context.Context, pgx.Tx, AuthorizationRequirement) (Actor, error)

func (fn CommitAuthorizerFunc) Reauthorize(ctx context.Context, tx pgx.Tx, requirement AuthorizationRequirement) (Actor, error) {
	return fn(ctx, tx, requirement)
}

type MembershipProvisioner interface {
	EnsureProjectMembership(context.Context, pgx.Tx, Actor, int64, int64, int64, string) (MembershipChange, error)
}

type MembershipProvisionerFunc func(context.Context, pgx.Tx, Actor, int64, int64, int64, string) (MembershipChange, error)

func (fn MembershipProvisionerFunc) EnsureProjectMembership(ctx context.Context, tx pgx.Tx, actor Actor, orgID, projectID, userID int64, role string) (MembershipChange, error) {
	return fn(ctx, tx, actor, orgID, projectID, userID, role)
}

type Service struct {
	db               db.DBQuerier
	apiKeyHashSecret string
	authorizer       CommitAuthorizer
	memberships      MembershipProvisioner
	now              func() time.Time
}

func NewService(database db.DBQuerier, apiKeyHashSecret string, authorizer CommitAuthorizer, memberships MembershipProvisioner) (*Service, error) {
	if database == nil || strings.TrimSpace(apiKeyHashSecret) == "" || authorizer == nil || memberships == nil {
		return nil, fmt.Errorf("%w: service dependencies", ErrInvalid)
	}
	return &Service{db: database, apiKeyHashSecret: apiKeyHashSecret, authorizer: authorizer, memberships: memberships, now: time.Now}, nil
}

func (s *Service) Provision(ctx context.Context, raw ProvisionInput) (ProvisionResult, error) {
	input, _, err := normalizeProvisionInput(raw)
	if err != nil {
		return ProvisionResult{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return ProvisionResult{}, fmt.Errorf("begin access provision: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	result, err := s.provisionTx(ctx, tx, input)
	if err != nil {
		return ProvisionResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ProvisionResult{}, fmt.Errorf("commit access provision: %w", err)
	}
	return result, nil
}

// ProvisionAndIssueCredential is the compatibility bridge for ordinary key
// creation. The access request, any approved Grant/revision, the one-time
// credential, audit rows, and outbox rows share one transaction. A
// needs-review decision is committed without creating a credential; a later
// retry of the same request can issue against the approved Grant without
// duplicating either request or secret-bearing key row.
func (s *Service) ProvisionAndIssueCredential(
	ctx context.Context,
	scope ActorScope,
	rawProvision ProvisionInput,
	rawCredential CredentialInput,
) (ProvisionCredentialResult, error) {
	input, _, err := normalizeProvisionInput(rawProvision)
	if err != nil {
		return ProvisionCredentialResult{}, err
	}
	for attempt := 0; attempt < credentialInsertMaxAttempts; attempt++ {
		tx, err := s.db.Begin(ctx)
		if err != nil {
			return ProvisionCredentialResult{}, fmt.Errorf("begin access provision credential: %w", err)
		}
		result, err := s.provisionTx(ctx, tx, input)
		if err != nil {
			_ = tx.Rollback(ctx)
			return ProvisionCredentialResult{}, err
		}
		combined := ProvisionCredentialResult{ProvisionResult: result}
		if result.Grant == nil {
			if err := tx.Commit(ctx); err != nil {
				return ProvisionCredentialResult{}, fmt.Errorf("commit access review request: %w", err)
			}
			return combined, nil
		}

		credentialInput := rawCredential
		credentialInput.GrantRef = result.Grant.GrantRef
		credentialInput, err = normalizeCredentialInput(credentialInput)
		if err != nil {
			_ = tx.Rollback(ctx)
			return ProvisionCredentialResult{}, err
		}
		credential, retry, err := s.issueCredentialTx(ctx, tx, scope, credentialInput)
		if err != nil {
			_ = tx.Rollback(ctx)
			return ProvisionCredentialResult{}, err
		}
		if retry {
			_ = tx.Rollback(ctx)
			continue
		}
		if err := tx.Commit(ctx); err != nil {
			return ProvisionCredentialResult{}, fmt.Errorf("commit access provision credential: %w", err)
		}
		combined.Credential = &credential
		return combined, nil
	}
	return ProvisionCredentialResult{}, fmt.Errorf("issue grant credential: prefix collision limit reached")
}

func (s *Service) provisionTx(ctx context.Context, tx pgx.Tx, input ProvisionInput) (ProvisionResult, error) {
	// Do not project caller-supplied foreign IDs into the deployment-global
	// principal advisory lanes. This first read is deliberately non-authoritative:
	// it only establishes that every referenced principal belongs to the claimed
	// tenant/project before CommitAuthorizer constructs its deterministic lock
	// order. The authoritative status, role, identity, and sponsor checks are
	// repeated below after those lanes and the admitted session are locked.
	if err := prevalidateProvisionPrincipalScope(ctx, tx, input); err != nil {
		return ProvisionResult{}, err
	}
	additional := []int64{input.SponsorUserID, input.Subject.UserID}
	allowSelfWithoutMembershipUserID := int64(0)
	if input.SubmissionMode == SubmissionSelf && input.Subject.Type == SubjectUser {
		allowSelfWithoutMembershipUserID = input.Subject.UserID
	}
	actor, err := s.authorizer.Reauthorize(ctx, tx, AuthorizationRequirement{
		OrgID: input.OrgID, ProjectID: input.ProjectID,
		AllowedOrganizationRoles:                []string{auth.RolePlatformAdmin, auth.RoleOrgAdmin},
		AllowedProjectRoles:                     []string{auth.RoleProjectAdmin, auth.ProjectRoleUser},
		AllowSelfRequestWithoutMembershipUserID: allowSelfWithoutMembershipUserID,
		AdditionalUserIDs:                       additional,
		AdditionalWorkloadIDs:                   []int64{input.Subject.WorkloadApplicationID},
	})
	if err != nil {
		return ProvisionResult{}, normalizeAuthorizationError(err)
	}
	input, err = bindWorkloadProvisionIdentityTx(ctx, tx, input)
	if err != nil {
		return ProvisionResult{}, err
	}
	input, semanticHash, err := normalizeProvisionInput(input)
	if err != nil {
		return ProvisionResult{}, err
	}

	if existing, found, readErr := readRequestByClientIDTx(ctx, tx, input.OrgID, actor.UserID, input.ClientRequestID); readErr != nil {
		return ProvisionResult{}, readErr
	} else if found {
		if existing.SemanticHash != semanticHash {
			return ProvisionResult{}, fmt.Errorf("%w: client_request_id semantic mismatch", ErrConflict)
		}
		grant, grantFound, grantErr := readGrantByRequestIDTx(ctx, tx, existing.ID)
		if grantErr != nil {
			return ProvisionResult{}, grantErr
		}
		result := ProvisionResult{Request: existing, Idempotent: true}
		if grantFound {
			result.Grant = &grant
		}
		return result, nil
	}
	now := s.now().UTC().Truncate(time.Microsecond)
	if input.ValidUntil != nil && !input.ValidUntil.After(now) {
		return ProvisionResult{}, fmt.Errorf("%w: valid_until must be in the future", ErrInvalid)
	}

	projectActive, subjectRole, subjectOrganizationRole, err := validateSubjectScopeTx(ctx, tx, input)
	if err != nil {
		return ProvisionResult{}, err
	}
	if !projectActive {
		return ProvisionResult{}, fmt.Errorf("%w: project is not active", ErrConflict)
	}
	if err := validateSponsorTx(ctx, tx, input); err != nil {
		return ProvisionResult{}, err
	}
	if err := validateSubmissionAuthority(actor, input); err != nil {
		return ProvisionResult{}, err
	}
	if err := lockGrantSubjectLaneTx(ctx, tx, input); err != nil {
		return ProvisionResult{}, err
	}
	baselineGrant, baselineFound, err := readNonrevokedGrantForSubjectTx(ctx, tx, input)
	if err != nil {
		return ProvisionResult{}, err
	}

	var baseline *Grant
	if baselineFound {
		baseline = &baselineGrant
	}
	decision, err := s.decideTx(ctx, tx, actor, input, subjectRole, subjectOrganizationRole, baseline)
	if err != nil {
		return ProvisionResult{}, err
	}
	requestRef := stableRef("req_", strconv.FormatInt(input.OrgID, 10), strconv.FormatInt(actor.UserID, 10), input.ClientRequestID)
	modelsJSON, _ := json.Marshal(input.Envelope.Models)
	endpointsJSON, _ := json.Marshal(input.Envelope.Endpoints)

	request := Request{
		RequestRef: requestRef, ClientRequestID: input.ClientRequestID,
		SemanticHash: semanticHash, SemanticRevision: 1,
		OrgID: input.OrgID, ProjectID: input.ProjectID, Subject: input.Subject,
		RequestKind: input.RequestKind, Envelope: input.Envelope,
		Environment: input.Environment, ProductionUse: input.ProductionUse, RequestedValidUntil: input.ValidUntil,
		SubmissionMode: input.SubmissionMode, SponsorUserID: input.SponsorUserID,
		TargetAccessGrantID: func() int64 {
			if baselineFound {
				return baselineGrant.ID
			}
			return 0
		}(),
		ExpectedGrantRevision: func() int64 {
			if baselineFound {
				return baselineGrant.Revision
			}
			return 0
		}(),
		Status: decision.status, DecisionMode: decision.mode,
		PolicyReasonCodes: normalizeStringSet(decision.policyReasons), DecisionReasonCodes: normalizeStringSet(decision.decisionReasons), PolicyVersion: PolicyVersion,
		SubmittedBy: actor.UserID, Revision: 1, SubmittedAt: now,
	}
	if decision.status == RequestApproved || decision.status == RequestRejected {
		request.DecidedBy = actor.UserID
		request.DecidedAt = &now
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO access_requests (
		  request_ref,client_request_id,semantic_hash,semantic_revision,
		  org_id,project_id,subject_type,subject_user_id,subject_workload_application_id,
		  request_kind,requested_project_role,requested_scope_mode,requested_models,requested_endpoints,
		  requested_rpm_limit,requested_rps_limit,requested_concurrency_limit,
		  environment,production_use,requested_valid_until,submission_mode,sponsor_user_id,target_access_grant_id,
		  expected_grant_revision,status,decision_mode,
		  policy_reason_codes,decision_reason_codes,policy_version,submitted_by,decided_by,submitted_at,decided_at,revision
		) VALUES (
		  $1,$2,$3,1,$4,$5,$6,NULLIF($7,0),NULLIF($8,0),$9,$10,$11,$12,$13,
		  $14,$15,$16,$17,$18,$19,$20,NULLIF($21,0),NULLIF($22,0),NULLIF($23,0),
		  $24,$25,$26,$27,$28,$29,NULLIF($30,0),$31,$32,1
		) RETURNING id`,
		request.RequestRef, request.ClientRequestID, request.SemanticHash,
		request.OrgID, request.ProjectID, request.Subject.Type, request.Subject.UserID,
		request.Subject.WorkloadApplicationID, request.RequestKind, request.Envelope.ProjectRole,
		request.Envelope.ScopeMode, modelsJSON, endpointsJSON, request.Envelope.RPMLimit, request.Envelope.RPSLimit,
		request.Envelope.ConcurrencyLimit, request.Environment,
		request.ProductionUse, request.RequestedValidUntil, request.SubmissionMode, request.SponsorUserID,
		request.TargetAccessGrantID, request.ExpectedGrantRevision, request.Status,
		request.DecisionMode, request.PolicyReasonCodes, request.DecisionReasonCodes, request.PolicyVersion,
		request.SubmittedBy, request.DecidedBy, request.SubmittedAt, request.DecidedAt,
	).Scan(&request.ID)
	if err != nil {
		return ProvisionResult{}, classifyDatabaseError("insert access request", err)
	}

	result := ProvisionResult{Request: request}
	var membershipChange MembershipChange
	if request.Status == RequestApproved {
		if request.Subject.Type == SubjectUser {
			membershipChange, err = s.memberships.EnsureProjectMembership(
				ctx, tx, actor, request.OrgID, request.ProjectID,
				request.Subject.UserID, request.Envelope.ProjectRole,
			)
			if err != nil {
				return ProvisionResult{}, err
			}
		}
		grant, grantErr := applyApprovedGrantTx(ctx, tx, request, actor.UserID, now)
		if grantErr != nil {
			return ProvisionResult{}, grantErr
		}
		result.Grant = &grant
	}
	if err := writeAuditAndOutboxTx(ctx, tx, request.OrgID, request.ProjectID, actor.UserID,
		"access_request.provision", "access_request", request.RequestRef, input.RequestID,
		input.ClientRequestID, map[string]any{
			"request_ref": request.RequestRef, "subject_type": request.Subject.Type,
			"status": request.Status, "decision_mode": request.DecisionMode,
			"policy_reason_codes":   cloneStrings(request.PolicyReasonCodes),
			"decision_reason_codes": cloneStrings(request.DecisionReasonCodes), "policy_version": PolicyVersion,
			"grant_ref": func() string {
				if result.Grant != nil {
					return result.Grant.GrantRef
				}
				return ""
			}(),
			"membership": membershipChange,
		}, now); err != nil {
		return ProvisionResult{}, err
	}
	return result, nil
}

func prevalidateProvisionPrincipalScope(ctx context.Context, tx pgx.Tx, input ProvisionInput) error {
	if input.OrgID <= 0 || input.ProjectID <= 0 {
		return ErrInvalid
	}
	var visible bool
	switch input.Subject.Type {
	case SubjectUser:
		// Reject malformed cross-axis IDs before they can be projected into
		// deployment-global user/workload advisory lanes. Human requests never
		// carry a sponsor; submitted_by is their complete actor evidence.
		if input.Subject.UserID <= 0 || input.Subject.WorkloadApplicationID != 0 || input.SponsorUserID != 0 {
			return ErrInvalid
		}
		if err := tx.QueryRow(ctx, `SELECT EXISTS (
			SELECT 1 FROM users WHERE id=$1 AND org_id=$2
		)`, input.Subject.UserID, input.OrgID).Scan(&visible); err != nil {
			return fmt.Errorf("prevalidate access subject scope: %w", err)
		}
	case SubjectWorkloadApplication:
		if input.Subject.UserID != 0 || input.Subject.WorkloadApplicationID <= 0 || input.SponsorUserID <= 0 {
			return ErrInvalid
		}
		if err := tx.QueryRow(ctx, `SELECT EXISTS (
			SELECT 1
			FROM workload_applications app
			JOIN users sponsor ON sponsor.id=$4 AND sponsor.org_id=app.org_id
			WHERE app.id=$1 AND app.org_id=$2 AND app.project_id=$3
		)`, input.Subject.WorkloadApplicationID, input.OrgID, input.ProjectID, input.SponsorUserID).Scan(&visible); err != nil {
			return fmt.Errorf("prevalidate workload subject scope: %w", err)
		}
	default:
		return ErrInvalid
	}
	if !visible {
		return ErrNotFound
	}
	return nil
}

type policyDecision struct {
	status          string
	mode            string
	policyReasons   []string
	decisionReasons []string
}

func (s *Service) decideTx(ctx context.Context, tx pgx.Tx, actor Actor, input ProvisionInput, subjectRole, subjectOrganizationRole string, baseline *Grant) (policyDecision, error) {
	reasons := make([]string, 0, 6)
	if input.Envelope.ProjectRole == auth.ProjectRoleViewer ||
		(input.Subject.Type == SubjectUser && subjectOrganizationRole == auth.RoleOrgViewer) {
		return policyDecision{
			status: RequestRejected, mode: DecisionPolicyRejection,
			policyReasons: []string{ReasonReadOnlySubjectCannotInvoke},
		}, nil
	}
	if input.ProductionUse {
		reasons = append(reasons, ReasonProductionWorkload)
	}
	if input.SubmissionMode == SubmissionMigration {
		reasons = append(reasons, ReasonAmbiguousMigration)
	}
	if input.Subject.Type == SubjectUser {
		if subjectRole == "" {
			reasons = append(reasons, ReasonFirstCrossProject)
			if input.Envelope.ProjectRole == auth.ProjectRoleAdmin {
				reasons = append(reasons, ReasonRoleElevation)
			}
		} else if roleRank(input.Envelope.ProjectRole) > roleRank(subjectRole) || subjectRole == auth.ProjectRoleViewer {
			reasons = append(reasons, ReasonRoleElevation)
		}
	}
	if len(input.Envelope.Models) > 0 {
		special, err := modelsOutsideProjectCatalogTx(ctx, tx, input.ProjectID, input.OrgID, input.Envelope.Models)
		if err != nil {
			return policyDecision{}, err
		}
		if special {
			reasons = append(reasons, ReasonSpecialModel)
		}
	}
	if slices.ContainsFunc(input.Envelope.Endpoints, func(endpoint string) bool {
		return !accessscope.IsProjectDefaultEndpoint(endpoint)
	}) {
		reasons = append(reasons, ReasonSpecialEndpoint)
	}
	widening := compareGrantEnvelope(input.Envelope, baseline)
	if widening.role {
		reasons = append(reasons, ReasonRoleElevation)
	}
	if widening.scope {
		reasons = append(reasons, ReasonScopeExpansion)
	}
	if widening.rateOrConcurrency {
		reasons = append(reasons, ReasonExtraRateOrConcurrency)
	}
	if baseline != nil && validityWidens(baseline.ValidUntil, input.ValidUntil) {
		reasons = append(reasons, ReasonValidityExtension)
	}
	reasons = normalizeStringSet(reasons)

	directMode := slices.Contains([]string{SubmissionDirectGrant, SubmissionBootstrap, SubmissionRecovery}, input.SubmissionMode)
	if directMode && !input.ProductionUse {
		policyReasons := cloneStrings(reasons)
		if len(policyReasons) == 0 {
			policyReasons = []string{defaultPolicyReason(input.Subject.Type)}
		}
		if actor.OrganizationRole == auth.RolePlatformAdmin || actor.OrganizationRole == auth.RoleOrgAdmin {
			return policyDecision{status: RequestApproved, mode: DecisionDirect,
				policyReasons: policyReasons, decisionReasons: []string{directDecisionReason(input.SubmissionMode)}}, nil
		}
		if input.SubmissionMode == SubmissionDirectGrant && actor.ProjectRole == auth.RoleProjectAdmin && len(reasons) == 0 {
			return policyDecision{status: RequestApproved, mode: DecisionDirect,
				policyReasons: policyReasons, decisionReasons: []string{ReasonDirectGrantAuthorizedNonProduction}}, nil
		}
	}
	if len(reasons) == 0 && input.Subject.Type == SubjectUser &&
		slices.Contains([]string{auth.RoleProjectAdmin, auth.ProjectRoleUser}, subjectRole) &&
		roleRank(input.Envelope.ProjectRole) <= roleRank(subjectRole) &&
		((input.SubmissionMode == SubmissionSelf && input.Subject.UserID == actor.UserID) ||
			(input.SubmissionMode == SubmissionDelegated && (actor.ProjectRole == auth.RoleProjectAdmin || auth.IsOrgAdmin(actor.OrganizationRole)))) {
		return policyDecision{status: RequestApproved, mode: DecisionAutoPolicy,
			policyReasons: []string{ReasonAutoApprovedCurrentProjectDefault}}, nil
	}
	if len(reasons) == 0 && input.Subject.Type == SubjectWorkloadApplication &&
		actor.ProjectRole == auth.ProjectRoleAdmin && input.SubmissionMode == SubmissionDelegated {
		return policyDecision{status: RequestApproved, mode: DecisionAutoPolicy,
			policyReasons: []string{ReasonAutoApprovedWorkloadProjectDefault}}, nil
	}
	if len(reasons) == 0 {
		reasons = []string{ReasonWorkloadRequiresReview}
	}
	return policyDecision{status: RequestNeedsReview, mode: DecisionPending, policyReasons: reasons}, nil
}

type grantEnvelopeWidening struct {
	role              bool
	scope             bool
	rateOrConcurrency bool
}

// compareGrantEnvelope determines whether a reviewed Grant revision would
// enlarge any authorization or ceiling relative to its locked baseline. It is
// deliberately conservative: automatic policy may apply only to an equivalent
// or narrower envelope. A nil baseline uses the product defaults for an initial
// request; a non-nil baseline compares every field against the live Grant.
func compareGrantEnvelope(requested Envelope, baseline *Grant) grantEnvelopeWidening {
	widening := grantEnvelopeWidening{}
	rpmCeiling := accessscope.ProjectDefaultRPMLimit
	rpsCeiling := accessscope.ProjectDefaultRPSLimit
	var concurrencyCeiling *int
	if baseline != nil {
		widening.role = roleRank(requested.ProjectRole) > roleRank(baseline.Envelope.ProjectRole)
		switch baseline.Envelope.ScopeMode {
		case accessscope.ModeProjectDefault:
			// An explicit scope is narrower than the dynamic project default;
			// current catalog validation below still protects special models.
			widening.scope = requested.ScopeMode != accessscope.ModeProjectDefault &&
				requested.ScopeMode != accessscope.ModeExplicit
		case accessscope.ModeExplicit:
			widening.scope = requested.ScopeMode != accessscope.ModeExplicit ||
				!isSubset(requested.Models, baseline.Envelope.Models) ||
				!isSubset(requested.Endpoints, baseline.Envelope.Endpoints)
		default:
			// A corrupted or future baseline must never silently auto-approve.
			widening.scope = true
		}
		if baseline.Envelope.RPMLimit != nil {
			rpmCeiling = *baseline.Envelope.RPMLimit
		}
		if baseline.Envelope.RPSLimit != nil {
			rpsCeiling = *baseline.Envelope.RPSLimit
		}
		concurrencyCeiling = baseline.Envelope.ConcurrencyLimit
	}
	requestedRPM := accessscope.ProjectDefaultRPMLimit
	if requested.RPMLimit != nil {
		requestedRPM = *requested.RPMLimit
	}
	requestedRPS := accessscope.ProjectDefaultRPSLimit
	if requested.RPSLimit != nil {
		requestedRPS = *requested.RPSLimit
	}
	widening.rateOrConcurrency = requestedRPM > rpmCeiling || requestedRPS > rpsCeiling
	// A nil concurrency ceiling means no Grant-specific concurrency
	// entitlement, so an explicit finite value is a narrowing. Once a Grant
	// has a finite ceiling, removing or increasing it is a widening.
	widening.rateOrConcurrency = widening.rateOrConcurrency || (concurrencyCeiling != nil &&
		(requested.ConcurrencyLimit == nil || *requested.ConcurrencyLimit > *concurrencyCeiling))
	return widening
}

func validityWidens(current, requested *time.Time) bool {
	if current == nil {
		return false
	}
	return requested == nil || requested.After(*current)
}

func defaultPolicyReason(subjectType string) string {
	if subjectType == SubjectWorkloadApplication {
		return ReasonAutoApprovedWorkloadProjectDefault
	}
	return ReasonProjectDefaultScope
}

func directDecisionReason(submissionMode string) string {
	switch submissionMode {
	case SubmissionBootstrap:
		return ReasonBootstrapAuthorized
	case SubmissionRecovery:
		return ReasonRecoveryAuthorized
	default:
		return ReasonDirectGrantAuthorizedNonProduction
	}
}

func validateSubmissionAuthority(actor Actor, input ProvisionInput) error {
	organizationPrivileged := actor.OrganizationRole == auth.RolePlatformAdmin || actor.OrganizationRole == auth.RoleOrgAdmin
	switch input.SubmissionMode {
	case SubmissionSelf:
		if input.Subject.Type != SubjectUser || input.Subject.UserID != actor.UserID {
			return fmt.Errorf("%w: self subject mismatch", ErrForbidden)
		}
	case SubmissionDelegated:
		if !organizationPrivileged && actor.ProjectRole != auth.RoleProjectAdmin {
			return fmt.Errorf("%w: delegated submission requires project administration", ErrForbidden)
		}
	case SubmissionDirectGrant:
		if !organizationPrivileged && actor.ProjectRole != auth.RoleProjectAdmin {
			return fmt.Errorf("%w: direct grant requires administration", ErrForbidden)
		}
	case SubmissionMigration, SubmissionBootstrap, SubmissionRecovery:
		if !organizationPrivileged {
			return fmt.Errorf("%w: privileged submission mode", ErrForbidden)
		}
	default:
		return ErrInvalid
	}
	return nil
}

func validateSubjectScopeTx(ctx context.Context, tx pgx.Tx, input ProvisionInput) (bool, string, string, error) {
	var projectStatus string
	if err := tx.QueryRow(ctx,
		`SELECT status FROM projects WHERE id=$1 AND org_id=$2 FOR SHARE`, input.ProjectID, input.OrgID,
	).Scan(&projectStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, "", "", ErrNotFound
		}
		return false, "", "", fmt.Errorf("read access project: %w", err)
	}
	switch input.Subject.Type {
	case SubjectUser:
		var status, organizationRole string
		if err := tx.QueryRow(ctx, `SELECT status,organization_role FROM users WHERE id=$1 AND org_id=$2 FOR SHARE`, input.Subject.UserID, input.OrgID).Scan(&status, &organizationRole); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return false, "", "", ErrNotFound
			}
			return false, "", "", fmt.Errorf("read access subject: %w", err)
		}
		if status != "active" {
			return false, "", "", fmt.Errorf("%w: subject is inactive", ErrConflict)
		}
		var role string
		err := tx.QueryRow(ctx, `SELECT role FROM project_members WHERE project_id=$1 AND user_id=$2 FOR SHARE`, input.ProjectID, input.Subject.UserID).Scan(&role)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return false, "", "", fmt.Errorf("read subject membership: %w", err)
		}
		if err == nil {
			role, _ = auth.NormalizeProjectMembershipRole(role)
		}
		return projectStatus == "active", role, organizationRole, nil
	case SubjectWorkloadApplication:
		var status, environment, identityVersion string
		var productionUse bool
		if err := tx.QueryRow(ctx, `
			SELECT status,environment,production_use,identity_contract_version
			FROM workload_applications
			WHERE id=$1 AND org_id=$2 AND project_id=$3 FOR SHARE`,
			input.Subject.WorkloadApplicationID, input.OrgID, input.ProjectID,
		).Scan(&status, &environment, &productionUse, &identityVersion); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return false, "", "", ErrNotFound
			}
			return false, "", "", fmt.Errorf("read workload subject: %w", err)
		}
		if status != "active" {
			return false, "", "", fmt.Errorf("%w: workload is inactive", ErrConflict)
		}
		if environment != input.Environment || productionUse != input.ProductionUse {
			return false, "", "", fmt.Errorf("%w: workload environment and production classification are immutable", ErrConflict)
		}
		if identityVersion != "unirouter.workload-identity.v2" {
			return false, "", "", fmt.Errorf("%w: workload requires explicit v2 classification", ErrConflict)
		}
		return projectStatus == "active", "", "", nil
	default:
		return false, "", "", ErrInvalid
	}
}

// bindWorkloadProvisionIdentityTx makes the locked Workload identity the
// authority for environment and production classification before computing
// the request semantic hash. The public input must agree exactly; callers
// cannot relabel a production Workload with an arbitrary environment string.
func bindWorkloadProvisionIdentityTx(ctx context.Context, tx pgx.Tx, input ProvisionInput) (ProvisionInput, error) {
	if input.Subject.Type != SubjectWorkloadApplication {
		return input, nil
	}
	var environment string
	var productionUse bool
	if err := tx.QueryRow(ctx, `
		SELECT environment,production_use
		FROM workload_applications
		WHERE id=$1 AND org_id=$2 AND project_id=$3
		FOR SHARE`, input.Subject.WorkloadApplicationID, input.OrgID, input.ProjectID).Scan(
		&environment, &productionUse,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ProvisionInput{}, ErrNotFound
		}
		return ProvisionInput{}, fmt.Errorf("read workload provision identity: %w", err)
	}
	if input.Environment != environment || input.ProductionUse != productionUse {
		return ProvisionInput{}, fmt.Errorf("%w: workload environment or production classification mismatch", ErrConflict)
	}
	input.Environment = environment
	input.ProductionUse = productionUse
	return input, nil
}

func validateSponsorTx(ctx context.Context, tx pgx.Tx, input ProvisionInput) error {
	if input.Subject.Type == SubjectUser {
		if input.SponsorUserID != 0 {
			return fmt.Errorf("%w: human requests do not accept sponsor evidence", ErrInvalid)
		}
		return nil
	}
	if input.SponsorUserID <= 0 {
		return fmt.Errorf("%w: workload sponsor is required", ErrInvalid)
	}
	err := accesssponsor.ValidateCurrentAuthority(ctx, tx, input.SponsorUserID, input.OrgID, input.ProjectID)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, accesssponsor.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, accesssponsor.ErrInactive):
		return fmt.Errorf("%w: sponsor is inactive", ErrConflict)
	case errors.Is(err, accesssponsor.ErrNotAuthorized):
		return fmt.Errorf("%w: workload sponsor must be an active project caller or organization administrator", ErrForbidden)
	default:
		return err
	}
}

// validateGrantSubjectLiveTx prevents the control plane from creating an
// apparently active credential or resuming a grant whose runtime subject can
// no longer call. It locks the exact live subject lane after committing-actor
// reauthorization; suspend/revoke paths deliberately remain available for
// containment even when the subject is no longer eligible.
func validateGrantSubjectLiveTx(ctx context.Context, tx pgx.Tx, grant Grant) error {
	switch grant.Subject.Type {
	case SubjectUser:
		var userStatus, storedRole string
		err := tx.QueryRow(ctx, `
			SELECT u.status,pm.role
			  FROM users u
			  JOIN project_members pm ON pm.user_id=u.id AND pm.project_id=$3
			 WHERE u.id=$1 AND u.org_id=$2
			 FOR UPDATE OF u,pm`, grant.Subject.UserID, grant.OrgID, grant.ProjectID).Scan(&userStatus, &storedRole)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: grant subject is not currently eligible", ErrConflict)
		}
		if err != nil {
			return fmt.Errorf("read grant human subject: %w", err)
		}
		role, ok := auth.NormalizeProjectMembershipRole(storedRole)
		if userStatus != "active" || !ok || !slices.Contains([]string{auth.ProjectRoleAdmin, auth.ProjectRoleUser}, role) {
			return fmt.Errorf("%w: grant subject is not currently eligible", ErrConflict)
		}
		return nil
	case SubjectWorkloadApplication:
		var status, identityVersion, environment string
		var productionUse bool
		err := tx.QueryRow(ctx, `
			SELECT status,identity_contract_version,environment,production_use
			  FROM workload_applications
			 WHERE id=$1 AND org_id=$2 AND project_id=$3
			 FOR UPDATE`, grant.Subject.WorkloadApplicationID, grant.OrgID, grant.ProjectID).Scan(
			&status, &identityVersion, &environment, &productionUse,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: grant subject is not currently eligible", ErrConflict)
		}
		if err != nil {
			return fmt.Errorf("read grant workload subject: %w", err)
		}
		if status != "active" || identityVersion != "unirouter.workload-identity.v2" ||
			environment != grant.Environment || productionUse != grant.ProductionUse {
			return fmt.Errorf("%w: grant subject is not currently eligible", ErrConflict)
		}
		return nil
	default:
		return fmt.Errorf("%w: grant subject is not currently eligible", ErrConflict)
	}
}

func modelsOutsideProjectCatalogTx(ctx context.Context, tx pgx.Tx, projectID, orgID int64, models []string) (bool, error) {
	var hasExplicitSets bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM project_model_sets pms
		JOIN model_sets ms ON ms.id=pms.model_set_id AND ms.org_id=$2 AND ms.status='active'
		WHERE pms.project_id=$1
	)`, projectID, orgID).Scan(&hasExplicitSets); err != nil {
		return false, fmt.Errorf("read project model-set mode: %w", err)
	}
	for _, model := range models {
		var allowed bool
		query := `
			SELECT EXISTS (
			  SELECT 1 FROM project_model_sets pms
			  JOIN model_sets ms ON ms.id=pms.model_set_id AND ms.org_id=$2 AND ms.status='active'
			  JOIN model_set_items msi ON msi.model_set_id=ms.id
			  WHERE pms.project_id=$1 AND msi.item_type IN ('model','alias') AND msi.item_name=$3
			)`
		if hasExplicitSets {
			if err := tx.QueryRow(ctx, query, projectID, orgID, model).Scan(&allowed); err != nil {
				return false, fmt.Errorf("read project model scope: %w", err)
			}
		} else {
			// The base approved catalog is mutable decision input. Lock the exact
			// registry row (and alias row, when applicable) through commit so an
			// automatic approval cannot race a catalog de-list. If the de-list
			// commits first, neither query finds an eligible row and the request is
			// classified as special instead.
			var registryID int64
			err := tx.QueryRow(ctx, `
				SELECT id
				  FROM model_registry
				 WHERE org_id=$1 AND model_name=$2
				   AND approved_catalog AND lifecycle <> 'retired'
				 FOR SHARE`, orgID, model).Scan(&registryID)
			switch {
			case err == nil:
				allowed = true
			case !errors.Is(err, pgx.ErrNoRows):
				return false, fmt.Errorf("lock approved project model: %w", err)
			default:
				var aliasID int64
				err = tx.QueryRow(ctx, `
					SELECT mr.id,ma.id
					  FROM model_aliases ma
					  JOIN model_registry mr
					    ON mr.org_id=ma.org_id AND mr.model_name=ma.target_model
					 WHERE ma.org_id=$1 AND ma.alias=$2 AND ma.status='active'
					   AND mr.approved_catalog AND mr.lifecycle <> 'retired'
					 FOR SHARE OF mr,ma`, orgID, model).Scan(&registryID, &aliasID)
				switch {
				case err == nil:
					allowed = true
				case errors.Is(err, pgx.ErrNoRows):
					allowed = false
				default:
					return false, fmt.Errorf("lock approved project model alias: %w", err)
				}
			}
		}
		if !allowed {
			return true, nil
		}
	}
	return false, nil
}

func roleRank(role string) int {
	switch role {
	case auth.RoleProjectAdmin:
		return 3
	case auth.ProjectRoleUser:
		return 2
	case auth.ProjectRoleViewer:
		return 1
	default:
		return 0
	}
}

func lockGrantSubjectLaneTx(ctx context.Context, tx pgx.Tx, input ProvisionInput) error {
	subjectID := input.Subject.UserID
	if input.Subject.Type == SubjectWorkloadApplication {
		subjectID = input.Subject.WorkloadApplicationID
	}
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf(
		"access-grant:%d:%d:%s:%d:%s", input.OrgID, input.ProjectID,
		input.Subject.Type, subjectID, input.Environment,
	))
	if err != nil {
		return fmt.Errorf("lock access grant subject lane: %w", err)
	}
	return nil
}

func readNonrevokedGrantForSubjectTx(ctx context.Context, tx pgx.Tx, input ProvisionInput) (Grant, bool, error) {
	query := grantSelect + ` WHERE ag.org_id=$1 AND ag.project_id=$2 AND ag.environment=$3
		AND ag.status IN ('active','suspended') AND ag.subject_type=$4`
	args := []any{input.OrgID, input.ProjectID, input.Environment, input.Subject.Type}
	if input.Subject.Type == SubjectUser {
		query += ` AND ag.subject_user_id=$5`
		args = append(args, input.Subject.UserID)
	} else {
		query += ` AND ag.subject_workload_application_id=$5`
		args = append(args, input.Subject.WorkloadApplicationID)
	}
	query += ` FOR UPDATE OF ag`
	return readGrantRow(tx.QueryRow(ctx, query, args...))
}

func applyApprovedGrantTx(ctx context.Context, tx pgx.Tx, request Request, actorUserID int64, now time.Time) (Grant, error) {
	grant := Grant{
		AccessRequestID: request.ID,
		OrgID:           request.OrgID, ProjectID: request.ProjectID, Subject: request.Subject,
		Envelope: request.Envelope, Environment: request.Environment, ProductionUse: request.ProductionUse,
		SponsorUserID: request.SponsorUserID, Status: GrantActive,
		Revision: 1, ValidFrom: now, ValidUntil: request.RequestedValidUntil, DecisionMode: request.DecisionMode,
		PolicyReasonCodes:   normalizeStringSet(request.PolicyReasonCodes),
		DecisionReasonCodes: normalizeStringSet(request.DecisionReasonCodes), PolicyVersion: PolicyVersion,
		SemanticHash: request.SemanticHash, CreatedBy: actorUserID, UpdatedBy: actorUserID,
		CreatedAt: now, UpdatedAt: now,
	}
	modelsJSON, _ := json.Marshal(grant.Envelope.Models)
	endpointsJSON, _ := json.Marshal(grant.Envelope.Endpoints)
	if request.TargetAccessGrantID == 0 {
		current, found, err := readNonrevokedGrantForSubjectTx(ctx, tx, ProvisionInput{
			OrgID: request.OrgID, ProjectID: request.ProjectID, Subject: request.Subject,
			Environment: request.Environment,
		})
		if err != nil {
			return Grant{}, err
		}
		if found {
			return Grant{}, fmt.Errorf("%w: access grant baseline changed at revision %d", ErrConflict, current.Revision)
		}
		// A grant identity is stable across its scope revisions, but revocation
		// ends that authorization lifecycle. Binding a new grant ref to the
		// first approved request lets a later, legitimate re-grant create a new
		// identity while idempotent replay of that request remains deterministic.
		grant.GrantRef = stableRef("grant_", request.RequestRef)
		err = tx.QueryRow(ctx, `
		INSERT INTO access_grants (
		  grant_ref,access_request_id,org_id,project_id,subject_type,subject_user_id,
		  subject_workload_application_id,sponsor_user_id,project_role_snapshot,scope_mode,allowed_models,allowed_endpoints,
		  rpm_limit,rps_limit,concurrency_limit,environment,production_use,status,revision,
		  valid_from,valid_until,decision_mode,policy_reason_codes,decision_reason_codes,policy_version,semantic_hash,
		  created_by,updated_by,created_at,updated_at
		) VALUES ($1,$2,$3,$4,$5,NULLIF($6,0),NULLIF($7,0),NULLIF($8,0),$9,$10,$11,$12,$13,$14,$15,$16,$17,'active',1,$18,$19,$20,$21,$22,$23,$24,$25,$25,$18,$18)
		RETURNING id`,
			grant.GrantRef, grant.AccessRequestID, grant.OrgID, grant.ProjectID,
			grant.Subject.Type, grant.Subject.UserID, grant.Subject.WorkloadApplicationID, grant.SponsorUserID,
			grant.Envelope.ProjectRole, grant.Envelope.ScopeMode, modelsJSON, endpointsJSON, grant.Envelope.RPMLimit,
			grant.Envelope.RPSLimit, grant.Envelope.ConcurrencyLimit,
			grant.Environment, grant.ProductionUse, grant.ValidFrom, grant.ValidUntil, grant.DecisionMode, grant.PolicyReasonCodes,
			grant.DecisionReasonCodes, grant.PolicyVersion, grant.SemanticHash, actorUserID,
		).Scan(&grant.ID)
		if err != nil {
			return Grant{}, classifyDatabaseError("insert access grant", err)
		}
	} else {
		current, found, err := readGrantRow(tx.QueryRow(ctx, grantSelect+`
			WHERE ag.id=$1 AND ag.org_id=$2 AND ag.project_id=$3 FOR UPDATE OF ag`,
			request.TargetAccessGrantID, request.OrgID, request.ProjectID))
		if err != nil {
			return Grant{}, err
		}
		if !found || current.Revision != request.ExpectedGrantRevision || current.Status == GrantRevoked ||
			current.Subject != request.Subject || current.Environment != request.Environment ||
			current.ProductionUse != request.ProductionUse {
			return Grant{}, fmt.Errorf("%w: access grant baseline changed", ErrConflict)
		}
		grant.ID, grant.GrantRef, grant.Status = current.ID, current.GrantRef, current.Status
		grant.Revision = current.Revision + 1
		grant.CreatedBy, grant.CreatedAt = current.CreatedBy, current.CreatedAt
		tag, updateErr := tx.Exec(ctx, `
			UPDATE access_grants
			SET access_request_id=$1,sponsor_user_id=NULLIF($2,0),project_role_snapshot=$3,
			    scope_mode=$4,allowed_models=$5,allowed_endpoints=$6,rpm_limit=$7,rps_limit=$8,
			    concurrency_limit=$9,production_use=$10,valid_from=$11,valid_until=$12,
			    decision_mode=$13,policy_reason_codes=$14,decision_reason_codes=$15,
			    policy_version=$16,semantic_hash=$17,revision=$18,updated_by=$19,updated_at=$11
			WHERE id=$20 AND revision=$21 AND status IN ('active','suspended')`,
			grant.AccessRequestID, grant.SponsorUserID, grant.Envelope.ProjectRole,
			grant.Envelope.ScopeMode, modelsJSON, endpointsJSON, grant.Envelope.RPMLimit,
			grant.Envelope.RPSLimit, grant.Envelope.ConcurrencyLimit,
			grant.ProductionUse, grant.ValidFrom, grant.ValidUntil, grant.DecisionMode, grant.PolicyReasonCodes, grant.DecisionReasonCodes,
			grant.PolicyVersion, grant.SemanticHash, grant.Revision, actorUserID,
			grant.ID, current.Revision)
		if updateErr != nil {
			return Grant{}, classifyDatabaseError("update access grant revision", updateErr)
		}
		if tag.RowsAffected() != 1 {
			return Grant{}, fmt.Errorf("%w: access grant revision changed", ErrConflict)
		}
	}
	if err := insertGrantRevisionTx(ctx, tx, grant, actorUserID); err != nil {
		return Grant{}, err
	}
	return grant, nil
}

func insertGrantRevisionTx(ctx context.Context, tx pgx.Tx, grant Grant, actorUserID int64) error {
	modelsJSON, _ := json.Marshal(grant.Envelope.Models)
	endpointsJSON, _ := json.Marshal(grant.Envelope.Endpoints)
	_, err := tx.Exec(ctx, `
		INSERT INTO access_grant_revisions (
		  access_grant_id,grant_ref,revision,access_request_id,org_id,project_id,
		  subject_type,subject_user_id,subject_workload_application_id,sponsor_user_id,
		  project_role_snapshot,scope_mode,allowed_models,allowed_endpoints,rpm_limit,rps_limit,
		  concurrency_limit,environment,production_use,status,valid_from,valid_until,decision_mode,
		  policy_reason_codes,decision_reason_codes,policy_version,semantic_hash,changed_by,created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,0),NULLIF($9,0),NULLIF($10,0),
		          $11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29)`,
		grant.ID, grant.GrantRef, grant.Revision, grant.AccessRequestID, grant.OrgID, grant.ProjectID,
		grant.Subject.Type, grant.Subject.UserID, grant.Subject.WorkloadApplicationID,
		grant.SponsorUserID, grant.Envelope.ProjectRole, grant.Envelope.ScopeMode, modelsJSON,
		endpointsJSON, grant.Envelope.RPMLimit, grant.Envelope.RPSLimit,
		grant.Envelope.ConcurrencyLimit, grant.Environment, grant.ProductionUse,
		grant.Status, grant.ValidFrom, grant.ValidUntil, grant.DecisionMode,
		grant.PolicyReasonCodes, grant.DecisionReasonCodes, grant.PolicyVersion,
		grant.SemanticHash, actorUserID, grant.UpdatedAt)
	if err != nil {
		return classifyDatabaseError("insert access grant revision", err)
	}
	return nil
}

func (s *Service) Decide(ctx context.Context, scope ActorScope, input DecisionInput) (ProvisionResult, error) {
	input.RequestRef = strings.TrimSpace(input.RequestRef)
	input.Decision = strings.ToLower(strings.TrimSpace(input.Decision))
	input.ReasonCodes = normalizeStringSet(input.ReasonCodes)
	input.ClientRequestID = strings.TrimSpace(input.ClientRequestID)
	if input.RequestRef == "" || input.ExpectedRevision <= 0 ||
		!clientRequestIDPattern.MatchString(input.ClientRequestID) ||
		!slices.Contains([]string{"approve", "reject"}, input.Decision) {
		return ProvisionResult{}, ErrInvalid
	}
	expectedReason := ReasonManualApproved
	if input.Decision == "reject" {
		expectedReason = ReasonManualRejected
	}
	if len(input.ReasonCodes) > 1 || (len(input.ReasonCodes) == 1 && input.ReasonCodes[0] != expectedReason) {
		return ProvisionResult{}, fmt.Errorf("%w: decision reason code does not match decision", ErrInvalid)
	}
	visible, found, err := readRequestByRefScopedMode(ctx, s.db, input.RequestRef, scope, objectDecision)
	if err != nil {
		return ProvisionResult{}, err
	}
	if !found {
		return ProvisionResult{}, ErrNotFound
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return ProvisionResult{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	actor, err := s.authorizer.Reauthorize(ctx, tx, AuthorizationRequirement{
		OrgID: visible.OrgID, ProjectID: visible.ProjectID,
		AllowedOrganizationRoles: []string{auth.RolePlatformAdmin, auth.RoleOrgAdmin},
		AllowedProjectRoles:      []string{auth.RoleProjectAdmin},
		// The request has already been actor-scoped before this commit lock.
		// Decision authority remains valid for an inactive project so rejection
		// can close stale work; approval performs an explicit live-project check
		// below and returns a stable state conflict instead of a hidden 404.
		AllowInactiveProject: true,
		AdditionalUserIDs:    []int64{visible.Subject.UserID, visible.SponsorUserID},
		AdditionalWorkloadIDs: []int64{
			visible.Subject.WorkloadApplicationID,
		},
	})
	if err != nil {
		return ProvisionResult{}, normalizeAuthorizationError(err)
	}
	request, found, err := readRequestByRefScopedTx(ctx, tx, input.RequestRef, scope, objectDecision)
	if err != nil {
		return ProvisionResult{}, err
	}
	if !found || request.ID != visible.ID {
		return ProvisionResult{}, ErrNotFound
	}
	decisionHash, err := operationSemanticHash(struct {
		RequestRef       string   `json:"request_ref"`
		ExpectedRevision int64    `json:"expected_revision"`
		Decision         string   `json:"decision"`
		ReasonCodes      []string `json:"decision_reason_codes"`
	}{input.RequestRef, input.ExpectedRevision, input.Decision, input.ReasonCodes})
	if err != nil {
		return ProvisionResult{}, err
	}
	if err := lockAccessOperationTx(ctx, tx, request.OrgID, actor.UserID, input.ClientRequestID); err != nil {
		return ProvisionResult{}, err
	}
	if existing, found, err := readAccessOperationTx(ctx, tx, request.OrgID, actor.UserID, input.ClientRequestID); err != nil {
		return ProvisionResult{}, err
	} else if found {
		if existing.Kind != "access_request_decide" || existing.SemanticHash != decisionHash {
			return ProvisionResult{}, fmt.Errorf("%w: client request operation mismatch", ErrConflict)
		}
		var replay ProvisionResult
		if err := json.Unmarshal(existing.ResultPayload, &replay); err != nil {
			return ProvisionResult{}, fmt.Errorf("decode decision operation result: %w", err)
		}
		replay.Idempotent = true
		return replay, nil
	}
	if request.Status != RequestNeedsReview || request.Revision != input.ExpectedRevision {
		return ProvisionResult{}, fmt.Errorf("%w: request revision or state changed", ErrConflict)
	}
	organizationAdmin := actor.OrganizationRole == auth.RolePlatformAdmin || actor.OrganizationRole == auth.RoleOrgAdmin
	now := s.now().UTC()
	if input.Decision == "reject" {
		if !organizationAdmin {
			if !projectAdminMayReject(request) {
				return ProvisionResult{}, fmt.Errorf("%w: request reasons require organization authority", ErrForbidden)
			}
		}
	} else {
		projectActive, subjectRole, subjectOrganizationRole, validateErr := validateSubjectScopeTx(ctx, tx, ProvisionInput{
			OrgID: request.OrgID, ProjectID: request.ProjectID, Subject: request.Subject,
			RequestKind: request.RequestKind, Envelope: request.Envelope,
			Environment: request.Environment, ProductionUse: request.ProductionUse,
			SubmissionMode: request.SubmissionMode, SponsorUserID: request.SponsorUserID,
		})
		if validateErr != nil {
			return ProvisionResult{}, validateErr
		}
		if !projectActive {
			return ProvisionResult{}, fmt.Errorf("%w: project is not active", ErrConflict)
		}
		if validateErr = validateSponsorTx(ctx, tx, ProvisionInput{
			OrgID: request.OrgID, ProjectID: request.ProjectID, Subject: request.Subject,
			SubmissionMode: request.SubmissionMode, SponsorUserID: request.SponsorUserID,
		}); validateErr != nil {
			return ProvisionResult{}, validateErr
		}
		if !organizationAdmin {
			if !projectAdminMayDecide(request, subjectRole) {
				return ProvisionResult{}, fmt.Errorf("%w: request reasons require organization authority", ErrForbidden)
			}
		}
		if request.ProductionUse && !organizationAdmin {
			return ProvisionResult{}, ErrForbidden
		}
		if request.Envelope.ProjectRole == auth.ProjectRoleViewer || subjectOrganizationRole == auth.RoleOrgViewer {
			return ProvisionResult{}, fmt.Errorf("%w: read-only subjects cannot receive calling grants", ErrConflict)
		}
		currentSpecialModel, validateErr := modelsOutsideProjectCatalogTx(
			ctx, tx, request.ProjectID, request.OrgID, request.Envelope.Models,
		)
		if validateErr != nil {
			return ProvisionResult{}, validateErr
		}
		recordedSpecialModel := slices.Contains(request.PolicyReasonCodes, ReasonSpecialModel)
		if currentSpecialModel != recordedSpecialModel {
			// Policy reasons are immutable decision evidence. A mutable catalog
			// classification change cannot be folded silently into the old
			// request; keep it pending and require a new request whose evidence
			// and decision authority reflect the current classification.
			return ProvisionResult{}, fmt.Errorf("%w: model classification changed; submit a new access request", ErrConflict)
		}
		if request.RequestedValidUntil != nil && !request.RequestedValidUntil.After(now) {
			return ProvisionResult{}, fmt.Errorf("%w: requested access validity expired before approval", ErrConflict)
		}
	}
	status := RequestApproved
	reasons := input.ReasonCodes
	if input.Decision == "reject" {
		status = RequestRejected
		if len(reasons) == 0 {
			reasons = []string{ReasonManualRejected}
		}
	} else if len(reasons) == 0 {
		reasons = []string{ReasonManualApproved}
	}
	tag, err := tx.Exec(ctx, `
		UPDATE access_requests
		SET status=$1,decision_mode='manual',decision_reason_codes=$2,decided_by=$3,
		    decided_at=$4,revision=revision+1,updated_at=$4
		WHERE id=$5 AND status='needs_review' AND revision=$6`,
		status, reasons, actor.UserID, now, request.ID, input.ExpectedRevision)
	if err != nil {
		return ProvisionResult{}, fmt.Errorf("decide access request: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ProvisionResult{}, fmt.Errorf("%w: request revision changed", ErrConflict)
	}
	request.Status, request.DecisionMode, request.DecisionReasonCodes = status, DecisionManual, reasons
	request.DecidedBy, request.DecidedAt, request.Revision = actor.UserID, &now, request.Revision+1
	result := ProvisionResult{Request: request}
	var membershipChange MembershipChange
	if status == RequestApproved {
		if request.Subject.Type == SubjectUser {
			membershipChange, err = s.memberships.EnsureProjectMembership(
				ctx, tx, actor, request.OrgID, request.ProjectID,
				request.Subject.UserID, request.Envelope.ProjectRole,
			)
			if err != nil {
				return ProvisionResult{}, err
			}
		}
		grant, err := applyApprovedGrantTx(ctx, tx, request, actor.UserID, now)
		if err != nil {
			return ProvisionResult{}, err
		}
		result.Grant = &grant
	}
	if err := writeAuditAndOutboxTx(ctx, tx, request.OrgID, request.ProjectID, actor.UserID,
		"access_request.decide", "access_request", request.RequestRef, input.RequestID,
		input.ClientRequestID, map[string]any{
			"status": status, "policy_reason_codes": cloneStrings(request.PolicyReasonCodes),
			"decision_reason_codes": reasons, "revision": request.Revision,
			"membership": membershipChange,
		}, now); err != nil {
		return ProvisionResult{}, err
	}
	if err := insertDecisionOperationTx(ctx, tx, request, result, actor.UserID, input.ClientRequestID, decisionHash); err != nil {
		return ProvisionResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ProvisionResult{}, err
	}
	return result, nil
}

func insertDecisionOperationTx(
	ctx context.Context,
	tx pgx.Tx,
	request Request,
	result ProvisionResult,
	actorUserID int64,
	clientRequestID, semanticHash string,
) error {
	payload, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal decision operation result: %w", err)
	}
	grantID := int64(0)
	if result.Grant != nil {
		grantID = result.Grant.ID
	}
	operationRef := stableRef("op_", fmt.Sprint(request.OrgID), fmt.Sprint(actorUserID), clientRequestID)
	_, err = tx.Exec(ctx, `
		INSERT INTO access_operations (
		  operation_ref,org_id,project_id,access_request_id,access_grant_id,actor_user_id,
		  client_request_id,operation_kind,semantic_hash,result_payload
		) VALUES ($1,$2,$3,$4,NULLIF($5,0),$6,$7,'access_request_decide',$8,$9)`,
		operationRef, request.OrgID, request.ProjectID, request.ID, grantID, actorUserID,
		clientRequestID, semanticHash, payload)
	if err != nil {
		return classifyDatabaseError("insert access decision operation", err)
	}
	return nil
}

// CancelRequest closes a still-pending request without changing project
// membership, Grant, or credential state. The actor-scoped first lookup and
// the commit-time actor/session lock make hidden requests indistinguishable
// from absent ones while a concurrent manual decision and cancellation
// serialize on the same access_requests row.
func (s *Service) CancelRequest(ctx context.Context, scope ActorScope, input CancelRequestInput) (CancelRequestResult, error) {
	input.RequestRef = strings.TrimSpace(input.RequestRef)
	input.ClientRequestID = strings.TrimSpace(input.ClientRequestID)
	if input.RequestRef == "" || input.ExpectedRevision <= 0 ||
		!clientRequestIDPattern.MatchString(input.ClientRequestID) {
		return CancelRequestResult{}, ErrInvalid
	}
	visible, found, err := readRequestByRefScopedMode(ctx, s.db, input.RequestRef, scope, objectCancel)
	if err != nil {
		return CancelRequestResult{}, err
	}
	if !found {
		return CancelRequestResult{}, ErrNotFound
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return CancelRequestResult{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	actor, err := s.authorizer.Reauthorize(ctx, tx, AuthorizationRequirement{
		OrgID: visible.OrgID, ProjectID: visible.ProjectID,
		AllowedOrganizationRoles: []string{
			auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleBillingAdmin,
			auth.RoleOrgViewer, auth.RoleMember,
		},
		AllowInactiveProject: true,
		AdditionalUserIDs:    []int64{visible.Subject.UserID, visible.SponsorUserID},
		AdditionalWorkloadIDs: []int64{
			visible.Subject.WorkloadApplicationID,
		},
	})
	if err != nil {
		return CancelRequestResult{}, normalizeAuthorizationError(err)
	}
	request, found, err := readRequestByRefScopedTx(ctx, tx, input.RequestRef, scope, objectCancel)
	if err != nil {
		return CancelRequestResult{}, err
	}
	if !found || request.ID != visible.ID {
		return CancelRequestResult{}, ErrNotFound
	}
	if actor.UserID != request.SubmittedBy &&
		!(request.Subject.Type == SubjectWorkloadApplication && actor.UserID == request.SponsorUserID) {
		return CancelRequestResult{}, ErrNotFound
	}
	semanticHash, err := operationSemanticHash(struct {
		RequestRef       string `json:"request_ref"`
		ExpectedRevision int64  `json:"expected_revision"`
		Action           string `json:"action"`
	}{input.RequestRef, input.ExpectedRevision, "cancel"})
	if err != nil {
		return CancelRequestResult{}, err
	}
	if err := lockAccessOperationTx(ctx, tx, request.OrgID, actor.UserID, input.ClientRequestID); err != nil {
		return CancelRequestResult{}, err
	}
	if existing, exists, readErr := readAccessOperationTx(ctx, tx, request.OrgID, actor.UserID, input.ClientRequestID); readErr != nil {
		return CancelRequestResult{}, readErr
	} else if exists {
		if existing.Kind != "access_request_cancel" || existing.SemanticHash != semanticHash {
			return CancelRequestResult{}, fmt.Errorf("%w: client request operation mismatch", ErrConflict)
		}
		var replay CancelRequestResult
		if err := json.Unmarshal(existing.ResultPayload, &replay); err != nil {
			return CancelRequestResult{}, fmt.Errorf("decode cancellation operation result: %w", err)
		}
		replay.Idempotent = true
		return replay, nil
	}
	if request.Status != RequestNeedsReview || request.Revision != input.ExpectedRevision {
		return CancelRequestResult{}, fmt.Errorf("%w: request revision or state changed", ErrConflict)
	}
	now := s.now().UTC()
	reasons := []string{ReasonRequesterCancelled}
	tag, err := tx.Exec(ctx, `
		UPDATE access_requests
		SET status='cancelled',decision_mode='requester',decision_reason_codes=$1,
		    cancelled_by=$2,cancelled_at=$3,revision=revision+1,updated_at=$3
		WHERE id=$4 AND status='needs_review' AND revision=$5`,
		reasons, actor.UserID, now, request.ID, input.ExpectedRevision)
	if err != nil {
		return CancelRequestResult{}, fmt.Errorf("cancel access request: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return CancelRequestResult{}, fmt.Errorf("%w: request revision changed", ErrConflict)
	}
	request.Status = RequestCancelled
	request.DecisionMode = DecisionRequester
	request.DecisionReasonCodes = reasons
	request.CancelledBy = actor.UserID
	request.CancelledAt = &now
	request.Revision++
	result := CancelRequestResult{Request: request}
	operationRef := stableRef("op_", fmt.Sprint(request.OrgID), fmt.Sprint(actor.UserID), input.ClientRequestID)
	payload, err := json.Marshal(result)
	if err != nil {
		return CancelRequestResult{}, fmt.Errorf("marshal cancellation result: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO access_operations (
		  operation_ref,org_id,project_id,access_request_id,actor_user_id,
		  client_request_id,operation_kind,semantic_hash,result_payload
		) VALUES ($1,$2,$3,$4,$5,$6,'access_request_cancel',$7,$8)`,
		operationRef, request.OrgID, request.ProjectID, request.ID, actor.UserID,
		input.ClientRequestID, semanticHash, payload); err != nil {
		return CancelRequestResult{}, classifyDatabaseError("insert access cancellation operation", err)
	}
	if err := writeAuditAndOutboxTx(ctx, tx, request.OrgID, request.ProjectID, actor.UserID,
		"access_request.cancel", "access_request", request.RequestRef, input.RequestID,
		input.ClientRequestID, map[string]any{
			"status": RequestCancelled, "reason_code": ReasonRequesterCancelled,
			"revision": request.Revision, "operation_ref": operationRef,
		}, now); err != nil {
		return CancelRequestResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CancelRequestResult{}, err
	}
	return result, nil
}

func projectAdminMayDecide(request Request, subjectRole string) bool {
	if request.ProductionUse ||
		request.Envelope.ProjectRole != auth.ProjectRoleUser ||
		slices.Contains([]string{SubmissionMigration, SubmissionBootstrap, SubmissionRecovery}, request.SubmissionMode) {
		return false
	}
	if len(request.PolicyReasonCodes) == 0 {
		return false
	}
	allowed := map[string]struct{}{
		ReasonFirstCrossProject:      {},
		ReasonWorkloadRequiresReview: {},
	}
	for _, reason := range request.PolicyReasonCodes {
		if reason == ReasonRoleElevation && request.Subject.Type == SubjectUser &&
			subjectRole == auth.ProjectRoleViewer && request.Envelope.ProjectRole == auth.ProjectRoleUser {
			continue
		}
		if _, ok := allowed[reason]; !ok {
			return false
		}
	}
	return true
}

func projectAdminMayReject(request Request) bool {
	if request.ProductionUse ||
		slices.Contains([]string{SubmissionMigration, SubmissionBootstrap, SubmissionRecovery}, request.SubmissionMode) {
		return false
	}
	if len(request.PolicyReasonCodes) == 0 {
		return false
	}
	for _, reason := range request.PolicyReasonCodes {
		switch reason {
		case ReasonFirstCrossProject, ReasonWorkloadRequiresReview:
			continue
		case ReasonRoleElevation:
			if request.Subject.Type == SubjectUser && request.Envelope.ProjectRole == auth.ProjectRoleUser {
				continue
			}
		}
		return false
	}
	return true
}

func (s *Service) ChangeGrantLifecycle(ctx context.Context, scope ActorScope, input GrantLifecycleInput) (Grant, error) {
	input.GrantRef = strings.TrimSpace(input.GrantRef)
	input.Action = strings.ToLower(strings.TrimSpace(input.Action))
	var err error
	input.Reason, err = normalizeGrantLifecycleReason(input.Reason)
	if err != nil {
		return Grant{}, err
	}
	input.ClientRequestID = strings.TrimSpace(input.ClientRequestID)
	if input.GrantRef == "" || input.ExpectedRevision <= 0 || !slices.Contains([]string{"suspend", "resume", "revoke"}, input.Action) || input.Reason == "" ||
		!clientRequestIDPattern.MatchString(input.ClientRequestID) {
		return Grant{}, ErrInvalid
	}
	visible, found, err := readGrantByRefScopedMode(ctx, s.db, input.GrantRef, scope, objectAdminMutation)
	if err != nil {
		return Grant{}, err
	}
	if !found {
		return Grant{}, ErrNotFound
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Grant{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	actor, err := s.authorizer.Reauthorize(ctx, tx, AuthorizationRequirement{
		OrgID: visible.OrgID, ProjectID: visible.ProjectID,
		AllowedOrganizationRoles: []string{auth.RolePlatformAdmin, auth.RoleOrgAdmin},
		AllowedProjectRoles:      []string{auth.RoleProjectAdmin},
		AdditionalUserIDs:        []int64{visible.Subject.UserID, visible.SponsorUserID},
		AdditionalWorkloadIDs:    []int64{visible.Subject.WorkloadApplicationID},
	})
	if err != nil {
		return Grant{}, normalizeAuthorizationError(err)
	}
	grant, found, err := readGrantByRefScopedTx(ctx, tx, input.GrantRef, scope, objectAdminMutation)
	if err != nil {
		return Grant{}, err
	}
	if !found || grant.ID != visible.ID {
		return Grant{}, ErrNotFound
	}
	semanticHash, err := operationSemanticHash(map[string]any{
		"grant_ref": input.GrantRef, "action": input.Action,
		"expected_revision": input.ExpectedRevision, "reason": input.Reason,
	})
	if err != nil {
		return Grant{}, err
	}
	if err := lockAccessOperationTx(ctx, tx, grant.OrgID, actor.UserID, input.ClientRequestID); err != nil {
		return Grant{}, err
	}
	if existing, found, err := readAccessOperationTx(ctx, tx, grant.OrgID, actor.UserID, input.ClientRequestID); err != nil {
		return Grant{}, err
	} else if found {
		if existing.Kind != "grant_"+input.Action || existing.SemanticHash != semanticHash {
			return Grant{}, fmt.Errorf("%w: client request operation mismatch", ErrConflict)
		}
		var replay Grant
		if err := json.Unmarshal(existing.ResultPayload, &replay); err != nil {
			return Grant{}, fmt.Errorf("decode access operation result: %w", err)
		}
		return replay, nil
	}
	if grant.Revision != input.ExpectedRevision {
		return Grant{}, fmt.Errorf("%w: grant revision changed", ErrConflict)
	}
	now := s.now().UTC()
	if input.Action == "resume" {
		if grant.ValidUntil != nil && !grant.ValidUntil.After(now) {
			return Grant{}, fmt.Errorf("%w: expired grant cannot be resumed", ErrConflict)
		}
		if err := validateGrantSubjectLiveTx(ctx, tx, grant); err != nil {
			return Grant{}, err
		}
	}
	next := ""
	lifecycleReason := ""
	switch input.Action {
	case "suspend":
		if grant.Status != GrantActive {
			return Grant{}, fmt.Errorf("%w: grant is not active", ErrConflict)
		}
		next = GrantSuspended
		lifecycleReason = accessgrantlifecycle.ReasonManualSuspend
	case "resume":
		if grant.Status != GrantSuspended {
			return Grant{}, fmt.Errorf("%w: grant is not suspended", ErrConflict)
		}
		next = GrantActive
		lifecycleReason = accessgrantlifecycle.ReasonManualResume
	case "revoke":
		if grant.Status == GrantRevoked {
			return Grant{}, fmt.Errorf("%w: grant is revoked", ErrConflict)
		}
		next = GrantRevoked
		lifecycleReason = accessgrantlifecycle.ReasonManualRevoke
	}
	lifecycleResult, err := accessgrantlifecycle.Change(
		ctx, tx, grant.ID, input.ExpectedRevision, actor.UserID, input.Action, lifecycleReason, now,
	)
	if err != nil {
		if errors.Is(err, accessgrantlifecycle.ErrRevisionConflict) || errors.Is(err, accessgrantlifecycle.ErrInvalidTransition) {
			return Grant{}, fmt.Errorf("%w: grant lifecycle changed", ErrConflict)
		}
		return Grant{}, classifyDatabaseError("change grant lifecycle", err)
	}
	grant.Status, grant.Revision, grant.UpdatedBy, grant.UpdatedAt = next, lifecycleResult.Revision, actor.UserID, now
	operationRef, err := insertAccessOperationTx(ctx, tx, grant.OrgID, grant.ProjectID, grant.ID,
		actor.UserID, input.ClientRequestID, "grant_"+input.Action, semanticHash, 0, 0, grant)
	if err != nil {
		return Grant{}, err
	}
	if err := writeAuditAndOutboxTx(ctx, tx, grant.OrgID, grant.ProjectID, actor.UserID,
		"access_grant."+input.Action, "access_grant", grant.GrantRef, input.RequestID, input.ClientRequestID,
		// ClientRequestID participates in the event identity; revision remains in
		// detail so distinct suspend/resume cycles cannot collapse.
		map[string]any{"status": next, "reason": input.Reason, "revision": grant.Revision,
			"lifecycle_reason_code":        lifecycleReason,
			"affected_credential_families": lifecycleResult.CredentialFamilies,
			"affected_credential_versions": lifecycleResult.SecretVersions,
			"client_request_id":            input.ClientRequestID, "operation_ref": operationRef}, now); err != nil {
		return Grant{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Grant{}, err
	}
	return grant, nil
}

func (s *Service) IssueCredential(ctx context.Context, scope ActorScope, input CredentialInput) (Credential, error) {
	var err error
	input, err = normalizeCredentialInput(input)
	if err != nil {
		return Credential{}, err
	}
	for attempt := 0; attempt < credentialInsertMaxAttempts; attempt++ {
		credential, retry, err := s.issueCredentialAttempt(ctx, scope, input)
		if !retry {
			return credential, err
		}
	}
	return Credential{}, fmt.Errorf("issue grant credential: prefix collision limit reached")
}

func (s *Service) RotateCredential(ctx context.Context, scope ActorScope, input RotateCredentialInput) (RotationResult, error) {
	input.GrantRef = strings.TrimSpace(input.GrantRef)
	input.ClientRequestID = strings.TrimSpace(input.ClientRequestID)
	if input.GrantRef == "" || !clientRequestIDPattern.MatchString(input.ClientRequestID) ||
		input.PredecessorKeyID <= 0 {
		return RotationResult{}, ErrInvalid
	}
	for attempt := 0; attempt < credentialInsertMaxAttempts; attempt++ {
		result, retry, err := s.rotateCredentialAttempt(ctx, scope, input)
		if !retry {
			return result, err
		}
	}
	return RotationResult{}, fmt.Errorf("rotate grant credential: prefix collision limit reached")
}

func (s *Service) rotateCredentialAttempt(ctx context.Context, scope ActorScope, input RotateCredentialInput) (RotationResult, bool, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return RotationResult{}, false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	visible, found, err := readGrantByRefScopedMode(ctx, tx, input.GrantRef, scope, objectCredentialMutate)
	if err != nil {
		return RotationResult{}, false, err
	}
	if !found {
		return RotationResult{}, false, ErrNotFound
	}
	actor, err := s.authorizer.Reauthorize(ctx, tx, AuthorizationRequirement{
		OrgID: visible.OrgID, ProjectID: visible.ProjectID,
		AllowedOrganizationRoles: []string{auth.RolePlatformAdmin, auth.RoleOrgAdmin},
		AllowedProjectRoles:      []string{auth.RoleProjectAdmin, auth.ProjectRoleUser},
		AdditionalUserIDs:        []int64{visible.Subject.UserID, visible.SponsorUserID},
		AdditionalWorkloadIDs:    []int64{visible.Subject.WorkloadApplicationID},
	})
	if err != nil {
		return RotationResult{}, false, normalizeAuthorizationError(err)
	}
	grant, found, err := readGrantByRefScopedTx(ctx, tx, input.GrantRef, scope, objectCredentialMutate)
	if err != nil {
		return RotationResult{}, false, err
	}
	if !found || grant.ID != visible.ID {
		return RotationResult{}, false, ErrNotFound
	}
	if !actorMayManageCredentialSecret(actor, grant.Subject) {
		return RotationResult{}, false, ErrForbidden
	}
	semanticHash, err := operationSemanticHash(map[string]any{
		"kind": "credential_rotate", "grant_ref": input.GrantRef,
		"predecessor_key_id": input.PredecessorKeyID,
	})
	if err != nil {
		return RotationResult{}, false, err
	}
	if err := lockAccessOperationTx(ctx, tx, grant.OrgID, actor.UserID, input.ClientRequestID); err != nil {
		return RotationResult{}, false, err
	}
	if existing, found, err := readAccessOperationTx(ctx, tx, grant.OrgID, actor.UserID, input.ClientRequestID); err != nil {
		return RotationResult{}, false, err
	} else if found {
		if existing.Kind != "credential_rotate" || existing.SemanticHash != semanticHash {
			return RotationResult{}, false, fmt.Errorf("%w: client request operation mismatch", ErrConflict)
		}
		var replay RotationResult
		if err := json.Unmarshal(existing.ResultPayload, &replay); err != nil {
			return RotationResult{}, false, fmt.Errorf("decode rotation operation result: %w", err)
		}
		replay.Idempotent = true
		replay.Credential.Key = ""
		replay.Credential.Idempotent = true
		replay.Credential.SecretAvailable = false
		replay.Credential.RecoveryAction = "rotate_again_if_successor_secret_was_not_saved"
		return replay, false, nil
	}
	if grant.Status != GrantActive || (grant.ValidUntil != nil && !grant.ValidUntil.After(s.now())) {
		return RotationResult{}, false, fmt.Errorf("%w: grant is not active", ErrConflict)
	}
	if err := validateGrantSubjectLiveTx(ctx, tx, grant); err != nil {
		return RotationResult{}, false, err
	}

	var predecessor CredentialMetadata
	var purpose string
	var logical logicalCredential
	if err := tx.QueryRow(ctx, `
		SELECT ak.id,ak.key_prefix,ak.status,COALESCE(ak.purpose,''),
		       ac.id,ac.credential_ref,ac.name,ac.allowed_ips,ac.allowed_models,
		       ac.allowed_endpoints,ac.policy_labels,ac.rpm_limit,ac.rps_limit,
		       ac.concurrency_limit,ac.legacy_budget_limit_cents,ac.status
		FROM api_keys ak
		JOIN access_credentials ac
		  ON ac.id=ak.access_credential_id AND ac.credential_ref=ak.credential_ref
		 AND ac.access_grant_id=ak.access_grant_id AND ac.org_id=ak.org_id AND ac.project_id=ak.project_id
		WHERE ak.id=$1 AND ak.access_grant_id=$2 AND ak.org_id=$3 AND ak.project_id=$4
		FOR UPDATE OF ak,ac`, input.PredecessorKeyID, grant.ID, grant.OrgID, grant.ProjectID).Scan(
		&predecessor.ID, &predecessor.KeyPrefix, &predecessor.Status, &purpose,
		&logical.ID, &logical.Ref, &logical.Name, &logical.AllowedIPs, &logical.AllowedModels,
		&logical.AllowedEndpoints, &logical.PolicyLabels, &logical.RPM, &logical.RPS,
		&logical.Concurrency, &logical.LegacyBudget, &logical.Status,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RotationResult{}, false, ErrNotFound
		}
		return RotationResult{}, false, err
	}
	if purpose != "" {
		return RotationResult{}, false, ErrNotFound
	}
	predecessor.CredentialRef = logical.Ref
	predecessor.Name = logical.Name
	if predecessor.Status != "active" || logical.Status != "active" {
		return RotationResult{}, false, fmt.Errorf("%w: predecessor is not rotation-ready", ErrConflict)
	}
	rawKey, prefix, err := auth.GenerateAPIKey(grant.OrgID)
	if err != nil {
		return RotationResult{}, false, fmt.Errorf("generate credential: %w", err)
	}
	keyHash := auth.HashAPIKey(rawKey, s.apiKeyHashSecret)
	keyType := "personal"
	var ownerUserID, workloadID any
	if grant.Subject.Type == SubjectUser {
		ownerUserID = grant.Subject.UserID
	} else {
		keyType, workloadID = "service", grant.Subject.WorkloadApplicationID
	}
	now := s.now().UTC()
	credential := Credential{
		Key: rawKey, CredentialRef: logical.Ref, KeyPrefix: prefix, Name: logical.Name, GrantRef: grant.GrantRef,
		Status: "active", AllowedIPs: logical.AllowedIPs, AllowedModels: csvScope(logical.AllowedModels),
		AllowedEndpoints: csvScope(logical.AllowedEndpoints), RPMLimit: logical.RPM,
		RPSLimit: logical.RPS, ConcurrencyLimit: nullIntPointer(logical.Concurrency),
		CreatedAt:      now,
		Classification: "grant_bound", SecretAvailable: true,
	}
	tag, err := tx.Exec(ctx, `
		UPDATE api_keys
		SET status='disabled',auth_epoch=auth_epoch+1,updated_at=$1
		WHERE id=$2 AND org_id=$3 AND project_id=$4
		  AND access_grant_id=$5 AND access_credential_id=$6 AND credential_ref=$7
		  AND status='active' AND COALESCE(purpose,'')=''`, now, predecessor.ID,
		grant.OrgID, grant.ProjectID, grant.ID, logical.ID, logical.Ref)
	if err != nil {
		return RotationResult{}, false, fmt.Errorf("disable rotation predecessor: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return RotationResult{}, false, fmt.Errorf("%w: predecessor authorization changed", ErrConflict)
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO api_keys (
		  org_id,project_id,owner_user_id,key_type,name,key_hash,key_prefix,status,
		  rpm_limit,qps_limit,concurrency_limit,budget_limit,allowed_ips,allowed_models,allowed_endpoints,policy_labels,
		  purpose,workload_application_id,access_grant_id,access_credential_id,credential_ref,
		  rotated_from_key_id,created_at,updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,'active',$8,$9,$10,$11,$12,$13,$14,$15,'',$16,$17,$18,$19,$20,$21,$21)
		RETURNING id`, grant.OrgID, grant.ProjectID, ownerUserID, keyType, logical.Name, keyHash,
		prefix, logical.RPM, logical.RPS, nullIntPointer(logical.Concurrency), logical.LegacyBudget,
		logical.AllowedIPs, logical.AllowedModels, logical.AllowedEndpoints, logical.PolicyLabels,
		workloadID, grant.ID, logical.ID, logical.Ref, predecessor.ID, now).Scan(&credential.ID)
	if err != nil {
		if isConstraintViolation(err, "idx_api_keys_prefix") {
			return RotationResult{}, true, nil
		}
		return RotationResult{}, false, classifyDatabaseError("insert rotation credential", err)
	}
	predecessor.Status = "disabled"
	result := RotationResult{Credential: credential, Predecessor: predecessor}
	stored := result
	stored.Credential.Key = ""
	stored.Credential.SecretAvailable = false
	operationRef, err := insertAccessOperationTx(ctx, tx, grant.OrgID, grant.ProjectID, grant.ID,
		actor.UserID, input.ClientRequestID, "credential_rotate", semanticHash,
		predecessor.ID, credential.ID, stored)
	if err != nil {
		return RotationResult{}, false, err
	}
	if err := writeAuditAndOutboxTx(ctx, tx, grant.OrgID, grant.ProjectID, actor.UserID,
		"access_grant.credential.rotate", "api_key", strconv.FormatInt(credential.ID, 10),
		input.RequestID, input.ClientRequestID, map[string]any{
			"grant_ref": grant.GrantRef, "credential_ref": logical.Ref, "predecessor_key_id": predecessor.ID,
			"successor_key_id": credential.ID,
			"operation_ref":    operationRef,
		}, now); err != nil {
		return RotationResult{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RotationResult{}, false, err
	}
	return result, false, nil
}

func (s *Service) issueCredentialAttempt(ctx context.Context, scope ActorScope, input CredentialInput) (Credential, bool, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Credential{}, false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	credential, retry, err := s.issueCredentialTx(ctx, tx, scope, input)
	if err != nil || retry {
		return credential, retry, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Credential{}, false, err
	}
	return credential, false, nil
}

func (s *Service) issueCredentialTx(ctx context.Context, tx pgx.Tx, scope ActorScope, input CredentialInput) (Credential, bool, error) {
	visible, found, err := readGrantByRefScopedMode(ctx, tx, input.GrantRef, scope, objectCredentialMutate)
	if err != nil {
		return Credential{}, false, err
	}
	if !found {
		return Credential{}, false, ErrNotFound
	}
	actor, err := s.authorizer.Reauthorize(ctx, tx, AuthorizationRequirement{
		OrgID: visible.OrgID, ProjectID: visible.ProjectID,
		AllowedOrganizationRoles: []string{auth.RolePlatformAdmin, auth.RoleOrgAdmin},
		AllowedProjectRoles:      []string{auth.RoleProjectAdmin, auth.ProjectRoleUser},
		AdditionalUserIDs:        []int64{visible.Subject.UserID, visible.SponsorUserID},
		AdditionalWorkloadIDs:    []int64{visible.Subject.WorkloadApplicationID},
	})
	if err != nil {
		return Credential{}, false, normalizeAuthorizationError(err)
	}
	grant, found, err := readGrantByRefScopedTx(ctx, tx, input.GrantRef, scope, objectCredentialMutate)
	if err != nil {
		return Credential{}, false, err
	}
	if !found || grant.ID != visible.ID {
		return Credential{}, false, ErrNotFound
	}
	if !actorMayManageCredentialSecret(actor, grant.Subject) {
		return Credential{}, false, ErrForbidden
	}
	semanticHash, err := operationSemanticHash(credentialOperationIntent("credential_issue", input))
	if err != nil {
		return Credential{}, false, err
	}
	if err := lockAccessOperationTx(ctx, tx, grant.OrgID, actor.UserID, input.ClientRequestID); err != nil {
		return Credential{}, false, err
	}
	if existing, found, err := readAccessOperationTx(ctx, tx, grant.OrgID, actor.UserID, input.ClientRequestID); err != nil {
		return Credential{}, false, err
	} else if found {
		if existing.Kind != "credential_issue" || existing.SemanticHash != semanticHash {
			return Credential{}, false, fmt.Errorf("%w: client request operation mismatch", ErrConflict)
		}
		var replay Credential
		if err := json.Unmarshal(existing.ResultPayload, &replay); err != nil {
			return Credential{}, false, fmt.Errorf("decode credential operation result: %w", err)
		}
		replay.Key = ""
		replay.Idempotent = true
		replay.SecretAvailable = false
		replay.RecoveryAction = "rotate_if_initial_secret_was_not_saved"
		return replay, false, nil
	}
	if grant.Status != GrantActive || (grant.ValidUntil != nil && !grant.ValidUntil.After(s.now())) {
		return Credential{}, false, fmt.Errorf("%w: grant is not active", ErrConflict)
	}
	if err := validateGrantSubjectLiveTx(ctx, tx, grant); err != nil {
		return Credential{}, false, err
	}
	resolved, err := resolveCredentialScope(input, grant)
	if err != nil {
		return Credential{}, false, err
	}
	if err := lockCredentialNameTx(ctx, tx, grant.OrgID, grant.ProjectID, input.Name); err != nil {
		return Credential{}, false, err
	}
	var duplicateName bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM api_keys
		  WHERE org_id=$1 AND project_id=$2 AND name=$3
		)`, grant.OrgID, grant.ProjectID, input.Name).Scan(&duplicateName); err != nil {
		return Credential{}, false, fmt.Errorf("read credential name availability: %w", err)
	}
	if duplicateName {
		return Credential{}, false, errors.Join(ErrConflict, ErrCredentialNameConflict)
	}
	credentialRef := stableRef("cred_", grant.GrantRef, strconv.FormatInt(actor.UserID, 10), input.ClientRequestID)
	rawKey, prefix, err := auth.GenerateAPIKey(grant.OrgID)
	if err != nil {
		return Credential{}, false, fmt.Errorf("generate credential: %w", err)
	}
	keyHash := auth.HashAPIKey(rawKey, s.apiKeyHashSecret)
	keyType := "personal"
	var ownerUserID, workloadID any
	if grant.Subject.Type == SubjectUser {
		ownerUserID = grant.Subject.UserID
	} else {
		keyType, workloadID = "service", grant.Subject.WorkloadApplicationID
	}
	now := s.now().UTC()
	var accessCredentialID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO access_credentials (
		  credential_ref,access_grant_id,org_id,project_id,name,allowed_ips,allowed_models,
		  allowed_endpoints,policy_labels,rpm_limit,rps_limit,concurrency_limit,legacy_budget_limit_cents,
		  status,created_by,created_at,updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,'active',$14,$15,$15)
		RETURNING id`, credentialRef, grant.ID, grant.OrgID, grant.ProjectID, input.Name,
		resolved.allowedIPs, strings.Join(resolved.models, ","), strings.Join(resolved.endpoints, ","),
		strings.Join(input.LegacyPolicyLabels, ","), resolved.rpm, resolved.rps, resolved.concurrency,
		input.LegacyBudgetLimitCents, actor.UserID, now).Scan(&accessCredentialID)
	if err != nil {
		if isConstraintViolation(err, "access_credentials_project_name_unique") {
			return Credential{}, false, errors.Join(ErrConflict, ErrCredentialNameConflict)
		}
		return Credential{}, false, classifyDatabaseError("insert logical access credential", err)
	}
	credential := Credential{Key: rawKey, CredentialRef: credentialRef, KeyPrefix: prefix, Name: input.Name, GrantRef: grant.GrantRef,
		Status: "active", AllowedIPs: resolved.allowedIPs, AllowedModels: resolved.models,
		AllowedEndpoints: resolved.endpoints, RPMLimit: resolved.rpm, RPSLimit: resolved.rps,
		ConcurrencyLimit: resolved.concurrency,
		CreatedAt:        now, Classification: "grant_bound", SecretAvailable: true}
	err = tx.QueryRow(ctx, `
		INSERT INTO api_keys (
		  org_id,project_id,owner_user_id,key_type,name,key_hash,key_prefix,status,
		  rpm_limit,qps_limit,concurrency_limit,budget_limit,allowed_ips,allowed_models,allowed_endpoints,policy_labels,
		  purpose,workload_application_id,access_grant_id,access_credential_id,credential_ref,created_at,updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,'active',$8,$9,$10,$11,$12,$13,$14,$15,'',$16,$17,$18,$19,$20,$20)
		RETURNING id`, grant.OrgID, grant.ProjectID, ownerUserID, keyType, input.Name, keyHash,
		prefix, resolved.rpm, resolved.rps, resolved.concurrency, input.LegacyBudgetLimitCents,
		resolved.allowedIPs, strings.Join(resolved.models, ","), strings.Join(resolved.endpoints, ","),
		strings.Join(input.LegacyPolicyLabels, ","), workloadID, grant.ID, accessCredentialID,
		credentialRef, now).Scan(&credential.ID)
	if err != nil {
		if isConstraintViolation(err, "idx_api_keys_prefix") {
			return Credential{}, true, nil
		}
		return Credential{}, false, classifyDatabaseError("insert grant credential", err)
	}
	storedCredential := credential
	storedCredential.Key = ""
	storedCredential.SecretAvailable = false
	operationRef, err := insertAccessOperationTx(ctx, tx, grant.OrgID, grant.ProjectID, grant.ID,
		actor.UserID, input.ClientRequestID, "credential_issue", semanticHash, 0, credential.ID, storedCredential)
	if err != nil {
		return Credential{}, false, err
	}
	if err := writeAuditAndOutboxTx(ctx, tx, grant.OrgID, grant.ProjectID, actor.UserID,
		"access_grant.credential.issue", "api_key", strconv.FormatInt(credential.ID, 10), input.RequestID, input.ClientRequestID,
		map[string]any{"grant_ref": grant.GrantRef, "credential_ref": credentialRef, "key_prefix": prefix, "key_type": keyType,
			"operation_ref": operationRef}, now); err != nil {
		return Credential{}, false, err
	}
	return credential, false, nil
}

// lockCredentialNameTx serializes the historical project-local name check
// without relying on a racy preflight query. It deliberately lives inside the
// same transaction that creates the grant-bound key so concurrent callers
// cannot both observe the name as free.
func lockCredentialNameTx(ctx context.Context, tx pgx.Tx, orgID, projectID int64, name string) error {
	lockIdentity := fmt.Sprintf("access-credential-name:%d:%d:%s", orgID, projectID, name)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, lockIdentity); err != nil {
		return fmt.Errorf("lock credential name: %w", err)
	}
	return nil
}

// actorMayManageCredentialSecret keeps grant administration separate from
// possession of a subject's authentication material. Human personal secrets
// are self-service only; service secrets are managed by project/org/platform
// administrators for the workload identity.
func actorMayManageCredentialSecret(actor Actor, subject Subject) bool {
	switch subject.Type {
	case SubjectUser:
		return actor.UserID == subject.UserID
	case SubjectWorkloadApplication:
		return actor.OrganizationRole == auth.RolePlatformAdmin || actor.OrganizationRole == auth.RoleOrgAdmin ||
			actor.ProjectRole == auth.ProjectRoleAdmin
	default:
		return false
	}
}

// actorMayRevokeCredential separates containment authority from secret
// issuance. Organization and project administrators may revoke any family in
// their authorized project; a project user may contain only their own human
// personal family. Organization authority takes precedence when one actor also
// happens to hold a lower project membership.
func actorMayRevokeCredential(actor Actor, subject Subject) bool {
	if actor.OrganizationRole == auth.RolePlatformAdmin || actor.OrganizationRole == auth.RoleOrgAdmin ||
		actor.ProjectRole == auth.ProjectRoleAdmin {
		return true
	}
	return actor.ProjectRole == auth.ProjectRoleUser && subject.Type == SubjectUser && subject.UserID == actor.UserID
}

func (s *Service) RevokeCredential(ctx context.Context, scope ActorScope, grantRef string, keyID int64, requestID, clientRequestID string) error {
	grantRef = strings.TrimSpace(grantRef)
	clientRequestID = strings.TrimSpace(clientRequestID)
	if grantRef == "" || keyID <= 0 || !clientRequestIDPattern.MatchString(clientRequestID) {
		return ErrInvalid
	}
	visible, found, err := readGrantByRefScopedMode(ctx, s.db, grantRef, scope, objectCredentialMutate)
	if err != nil {
		return err
	}
	if !found {
		return ErrNotFound
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	actor, err := s.authorizer.Reauthorize(ctx, tx, AuthorizationRequirement{
		OrgID: visible.OrgID, ProjectID: visible.ProjectID,
		AllowedOrganizationRoles: []string{auth.RolePlatformAdmin, auth.RoleOrgAdmin},
		AllowedProjectRoles:      []string{auth.RoleProjectAdmin, auth.ProjectRoleUser},
		AdditionalUserIDs:        []int64{visible.Subject.UserID, visible.SponsorUserID},
		AdditionalWorkloadIDs:    []int64{visible.Subject.WorkloadApplicationID},
	})
	if err != nil {
		return normalizeAuthorizationError(err)
	}
	grant, found, err := readGrantByRefScopedTx(ctx, tx, grantRef, scope, objectCredentialMutate)
	if err != nil {
		return err
	}
	if !found || grant.ID != visible.ID {
		return ErrNotFound
	}
	if !actorMayRevokeCredential(actor, grant.Subject) {
		return ErrForbidden
	}
	semanticHash, err := operationSemanticHash(map[string]any{
		"kind": "credential_revoke", "grant_ref": grantRef, "key_id": keyID,
	})
	if err != nil {
		return err
	}
	if err := lockAccessOperationTx(ctx, tx, grant.OrgID, actor.UserID, clientRequestID); err != nil {
		return err
	}
	if existing, found, err := readAccessOperationTx(ctx, tx, grant.OrgID, actor.UserID, clientRequestID); err != nil {
		return err
	} else if found {
		if existing.Kind != "credential_revoke" || existing.SemanticHash != semanticHash {
			return fmt.Errorf("%w: client request operation mismatch", ErrConflict)
		}
		return nil
	}
	var purpose, credentialRef, credentialStatus string
	var accessCredentialID int64
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(ak.purpose,''),ac.id,ac.credential_ref,ac.status
		FROM api_keys ak
		JOIN access_credentials ac
		  ON ac.id=ak.access_credential_id AND ac.credential_ref=ak.credential_ref
		 AND ac.access_grant_id=ak.access_grant_id AND ac.org_id=ak.org_id AND ac.project_id=ak.project_id
		WHERE ak.id=$1 AND ak.access_grant_id=$2 AND ak.org_id=$3 AND ak.project_id=$4
		FOR UPDATE OF ak,ac`,
		keyID, grant.ID, grant.OrgID, grant.ProjectID).Scan(&purpose, &accessCredentialID, &credentialRef, &credentialStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if purpose != "" {
		return ErrNotFound
	}
	if credentialStatus != "active" {
		return fmt.Errorf("%w: logical credential is not active", ErrConflict)
	}
	now := s.now().UTC()
	containment, err := accesscredential.RevokeFamily(ctx, tx, accessCredentialID, grant.ID, actor.UserID, now)
	if err != nil {
		return err
	}
	if containment.CredentialFamilies != 1 {
		return fmt.Errorf("%w: logical credential changed", ErrConflict)
	}
	operationRef, err := insertAccessOperationTx(ctx, tx, grant.OrgID, grant.ProjectID, grant.ID,
		actor.UserID, clientRequestID, "credential_revoke", semanticHash, keyID, 0,
		map[string]any{"status": "revoked", "credential_ref": credentialRef, "locator_key_id": keyID,
			"affected_version_count": containment.SecretVersions})
	if err != nil {
		return err
	}
	if err := writeAuditAndOutboxTx(ctx, tx, grant.OrgID, grant.ProjectID, actor.UserID,
		"access_grant.credential.revoke", "access_credential", credentialRef, requestID, clientRequestID,
		map[string]any{"grant_ref": grant.GrantRef, "credential_ref": credentialRef,
			"locator_key_id": keyID, "affected_version_count": containment.SecretVersions, "operation_ref": operationRef}, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) GetRequest(ctx context.Context, scope ActorScope, requestRef string) (Request, *Grant, error) {
	requestRef = strings.TrimSpace(requestRef)
	request, found, err := readRequestByRefScoped(ctx, s.db, requestRef, scope)
	if err != nil {
		return Request{}, nil, err
	}
	if !found {
		return Request{}, nil, ErrNotFound
	}
	if err := s.projectRequestActions(ctx, scope, &request); err != nil {
		return Request{}, nil, err
	}
	grant, grantFound, err := readGrantByRequestID(ctx, s.db, request.ID)
	if err != nil {
		return Request{}, nil, err
	}
	if !grantFound {
		return request, nil, nil
	}
	return request, &grant, nil
}

func (s *Service) GetGrant(ctx context.Context, scope ActorScope, grantRef string) (Grant, error) {
	grant, found, err := readGrantByRefScoped(ctx, s.db, strings.TrimSpace(grantRef), scope)
	if err != nil {
		return Grant{}, err
	}
	if !found {
		return Grant{}, ErrNotFound
	}
	return grant, nil
}

func (s *Service) ListGrantCredentials(ctx context.Context, scope ActorScope, grantRef string, pageInput PageInput) (GrantCredentialPage, error) {
	grantRef = strings.TrimSpace(grantRef)
	if grantRef == "" {
		return GrantCredentialPage{}, ErrInvalid
	}
	pageInput, err := NormalizePageInput(pageInput)
	if err != nil {
		return GrantCredentialPage{}, err
	}
	if !scope.valid() {
		return GrantCredentialPage{}, ErrNotFound
	}
	grant, found, err := readGrantRow(s.db.QueryRow(ctx, grantSelect+`
		WHERE ag.grant_ref=$1 AND (`+grantAccessPredicate(objectCredentialRead, scope.EnforceOrgBoundary)+`)`,
		grantRef, scope.UserID, scope.OrgID, scope.OrganizationRole))
	if err != nil {
		return GrantCredentialPage{}, err
	}
	if !found {
		return GrantCredentialPage{}, ErrNotFound
	}
	subjectLive, err := readGrantSubjectLive(ctx, s.db, grant)
	if err != nil {
		return GrantCredentialPage{}, err
	}
	rows, err := s.db.Query(ctx, `
		WITH credential_page AS (
		  SELECT ac.id
		  FROM access_credentials ac
		  WHERE ac.access_grant_id=$1 AND ac.org_id=$2 AND ac.project_id=$3
		    AND ($4::bigint=0 OR ac.id<$4)
		  ORDER BY ac.id DESC
		  LIMIT $5
		)
		SELECT ac.id,ac.credential_ref,ac.name,ac.status,ac.allowed_ips,ac.allowed_models,
		       ac.allowed_endpoints,ac.rpm_limit,ac.rps_limit,
		       ac.concurrency_limit,ac.created_at,
		       k.id,k.key_prefix,k.status,k.created_at,k.rotated_from_key_id
		FROM credential_page page
		JOIN access_credentials ac ON ac.id=page.id
		LEFT JOIN api_keys k
		  ON k.access_credential_id=ac.id AND k.credential_ref=ac.credential_ref
		 AND k.access_grant_id=ac.access_grant_id AND k.org_id=ac.org_id AND k.project_id=ac.project_id
		 AND COALESCE(k.purpose,'')=''
		ORDER BY ac.id DESC,k.id`, grant.ID, grant.OrgID, grant.ProjectID, pageInput.AfterID, pageInput.Limit+1)
	if err != nil {
		return GrantCredentialPage{}, err
	}
	defer rows.Close()
	now := s.now().UTC()
	result := make([]GrantCredentialMetadata, 0)
	indexByID := make(map[int64]int)
	for rows.Next() {
		var familyID int64
		var credentialRef, name, status, allowedIPs, modelsCSV, endpointsCSV string
		var rpmLimit, rpsLimit int
		var concurrency sql.NullInt64
		var createdAt time.Time
		var keyID sql.NullInt64
		var keyPrefix, keyStatus sql.NullString
		var keyCreatedAt sql.NullTime
		var rotatedFrom sql.NullInt64
		if err := rows.Scan(&familyID, &credentialRef, &name, &status, &allowedIPs,
			&modelsCSV, &endpointsCSV, &rpmLimit, &rpsLimit, &concurrency,
			&createdAt, &keyID, &keyPrefix, &keyStatus, &keyCreatedAt, &rotatedFrom); err != nil {
			return GrantCredentialPage{}, err
		}
		index, exists := indexByID[familyID]
		if !exists {
			item := GrantCredentialMetadata{
				ID: familyID, CredentialRef: credentialRef, Name: name, Status: status,
				AllowedIPs: allowedIPs, AllowedModels: csvScope(modelsCSV),
				AllowedEndpoints: csvScope(endpointsCSV),
				RPMLimit:         rpmLimit, RPSLimit: rpsLimit, ConcurrencyLimit: nullIntPointer(concurrency),
				CreatedAt: createdAt,
				Versions:  make([]CredentialVersionMetadata, 0),
			}
			item.EffectiveStatus = effectiveCredentialFamilyStatus(item.Status, grant, subjectLive, false, now)
			result = append(result, item)
			index = len(result) - 1
			indexByID[familyID] = index
		}
		if keyID.Valid {
			version := CredentialVersionMetadata{ID: keyID.Int64, KeyPrefix: keyPrefix.String,
				Status: keyStatus.String, CreatedAt: keyCreatedAt.Time}
			if rotatedFrom.Valid {
				value := rotatedFrom.Int64
				version.RotatedFromKeyID = &value
			}
			version.EffectiveStatus = effectiveCredentialVersionStatus(version.Status,
				result[index].Status, grant, subjectLive, now)
			result[index].Versions = append(result[index].Versions, version)
			result[index].VersionCount++
			if version.EffectiveStatus == "active" {
				result[index].EffectiveStatus = "active"
			}
		}
	}
	if err := rows.Err(); err != nil {
		return GrantCredentialPage{}, err
	}
	page := GrantCredentialPage{Data: result, Pagination: PageInfo{Limit: pageInput.Limit}}
	if len(page.Data) > pageInput.Limit {
		page.Data = page.Data[:pageInput.Limit]
		page.Pagination.HasMore = true
		next := page.Data[len(page.Data)-1].ID
		page.Pagination.NextAfterID = &next
	}
	return page, nil
}

func readGrantSubjectLive(ctx context.Context, rower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, grant Grant) (bool, error) {
	var live bool
	switch grant.Subject.Type {
	case SubjectUser:
		err := rower.QueryRow(ctx, `
			SELECT u.status='active'
			   AND u.organization_role<>'org_viewer'
			   AND p.status='active'
			   AND EXISTS (
			       SELECT 1 FROM project_members pm
			       WHERE pm.project_id=$2 AND pm.user_id=u.id
			         AND pm.role IN ('project_admin','project_user','project_developer','developer')
			   )
			FROM users u
			JOIN projects p ON p.id=$2 AND p.org_id=$3
			WHERE u.id=$1 AND u.org_id=$3`, grant.Subject.UserID, grant.ProjectID, grant.OrgID).Scan(&live)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return live, err
	case SubjectWorkloadApplication:
		err := rower.QueryRow(ctx, `
			SELECT wa.status='active' AND p.status='active'
			   AND wa.identity_contract_version='unirouter.workload-identity.v2'
			   AND wa.contract_version='unirouter.workload-application.v2'
			   AND wa.environment=$4
			   AND EXISTS (
			       SELECT 1 FROM users sponsor
			       WHERE sponsor.id=$5 AND sponsor.org_id=$2 AND sponsor.status='active'
			         AND (
			           sponsor.organization_role IN ('platform_admin','org_admin')
			           OR EXISTS (
			             SELECT 1 FROM project_members sponsor_pm
			             WHERE sponsor_pm.project_id=$3 AND sponsor_pm.user_id=sponsor.id
			               AND sponsor_pm.role IN ('project_admin','project_user','project_developer','developer')
			           )
			         )
			   )
			FROM workload_applications wa
			JOIN projects p ON p.id=wa.project_id AND p.org_id=wa.org_id
			WHERE wa.id=$1 AND wa.org_id=$2 AND wa.project_id=$3`,
			grant.Subject.WorkloadApplicationID, grant.OrgID, grant.ProjectID, grant.Environment, grant.SponsorUserID).Scan(&live)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return live, err
	default:
		return false, nil
	}
}

func csvScope(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	return normalizeStringSet(strings.Split(raw, ","))
}

func effectiveCredentialFamilyStatus(status string, grant Grant, subjectLive, hasActiveVersion bool, now time.Time) string {
	if status != "active" {
		return "revoked"
	}
	if grant.Status != GrantActive || now.Before(grant.ValidFrom) ||
		(grant.ValidUntil != nil && !grant.ValidUntil.After(now)) {
		return "disabled_by_grant"
	}
	if !subjectLive {
		return "subject_blocked"
	}
	if !hasActiveVersion {
		return "no_active_version"
	}
	return "active"
}

func effectiveCredentialVersionStatus(status string, familyStatus string, grant Grant, subjectLive bool, now time.Time) string {
	if status != "active" {
		return status
	}
	familyEffective := effectiveCredentialFamilyStatus(familyStatus, grant, subjectLive, true, now)
	if familyEffective != "active" {
		return familyEffective
	}
	return familyEffective
}

func (s *Service) ListRequests(ctx context.Context, scope ActorScope, orgID, projectID int64, pageInput PageInput) (RequestPage, error) {
	return s.ListRequestsFiltered(ctx, scope, orgID, projectID, RequestListFilter{}, pageInput)
}

// ListRequestsFiltered applies every queue filter in PostgreSQL before the
// keyset limit. This prevents a browser page from manufacturing false empty
// review queues by filtering an already-truncated result.
func (s *Service) ListRequestsFiltered(ctx context.Context, scope ActorScope, orgID, projectID int64, filter RequestListFilter, pageInput PageInput) (RequestPage, error) {
	pageInput, err := NormalizePageInput(pageInput)
	if err != nil {
		return RequestPage{}, err
	}
	filter.Status = strings.TrimSpace(filter.Status)
	if filter.Status != "" && !slices.Contains([]string{RequestApproved, RequestNeedsReview, RequestRejected, RequestCancelled}, filter.Status) {
		return RequestPage{}, ErrInvalid
	}
	if filter.SubjectWorkloadApplicationID < 0 {
		return RequestPage{}, ErrInvalid
	}
	if !scope.valid() {
		return RequestPage{Data: []Request{}, Pagination: PageInfo{Limit: pageInput.Limit}, Scope: "project"}, nil
	}
	rows, err := s.db.Query(ctx, requestSelect+` WHERE $1::text='' AND ar.org_id=$5 AND ar.project_id=$6 AND (`+requestAccessPredicate(objectRead, scope.EnforceOrgBoundary)+`)
		AND ($7::text='' OR ar.status=$7)
		AND ($8::bigint=0 OR ar.subject_workload_application_id=$8)
		AND ($9::bigint=0 OR ar.id<$9) ORDER BY ar.id DESC LIMIT $10`,
		"", scope.UserID, scope.OrgID, scope.OrganizationRole, orgID, projectID,
		filter.Status, filter.SubjectWorkloadApplicationID, pageInput.AfterID, pageInput.Limit+1)
	if err != nil {
		return RequestPage{}, err
	}
	defer rows.Close()
	var result []Request
	for rows.Next() {
		request, err := scanRequest(rows)
		if err != nil {
			return RequestPage{}, err
		}
		if err := s.projectRequestActions(ctx, scope, &request); err != nil {
			return RequestPage{}, err
		}
		result = append(result, request)
	}
	if err := rows.Err(); err != nil {
		return RequestPage{}, err
	}
	return requestPage(result, pageInput, "project"), nil
}

// ListMyRequests is the actor-owned access view. It intentionally does not
// require project membership so a first-project applicant can observe their
// pending request, but it cannot enumerate a project or another actor's
// objects. Submitter, subject, and sponsor ownership remain bounded by the
// admitted customer organization in ActorScope.
func (s *Service) ListMyRequests(ctx context.Context, scope ActorScope, pageInput PageInput) (RequestPage, error) {
	pageInput, err := NormalizePageInput(pageInput)
	if err != nil {
		return RequestPage{}, err
	}
	if !scope.valid() {
		return RequestPage{Data: []Request{}, Pagination: PageInfo{Limit: pageInput.Limit}, Scope: "my"}, nil
	}
	ownershipPredicate := `ar.org_id=$1
		  AND (ar.submitted_by=$2 OR ar.subject_user_id=$2 OR ar.sponsor_user_id=$2)`
	if scope.OrganizationRole == auth.RolePlatformAdmin && !scope.EnforceOrgBoundary {
		ownershipPredicate = `(ar.submitted_by=$2 OR (ar.org_id=$1 AND (ar.subject_user_id=$2 OR ar.sponsor_user_id=$2)))`
	}
	rows, err := s.db.Query(ctx, requestSelect+`
		WHERE `+ownershipPredicate+`
		  AND ($3::bigint=0 OR ar.id<$3)
		ORDER BY ar.id DESC LIMIT $4`, scope.OrgID, scope.UserID, pageInput.AfterID, pageInput.Limit+1)
	if err != nil {
		return RequestPage{}, err
	}
	defer rows.Close()
	var result []Request
	for rows.Next() {
		request, err := scanRequest(rows)
		if err != nil {
			return RequestPage{}, err
		}
		if err := s.projectRequestActions(ctx, scope, &request); err != nil {
			return RequestPage{}, err
		}
		result = append(result, request)
	}
	if err := rows.Err(); err != nil {
		return RequestPage{}, err
	}
	return requestPage(result, pageInput, "my"), nil
}

func (s *Service) ListGrants(ctx context.Context, scope ActorScope, orgID, projectID int64, pageInput PageInput) (GrantPage, error) {
	return s.ListGrantsFiltered(ctx, scope, orgID, projectID, GrantListFilter{}, pageInput)
}

func (s *Service) ListGrantsFiltered(ctx context.Context, scope ActorScope, orgID, projectID int64, filter GrantListFilter, pageInput PageInput) (GrantPage, error) {
	pageInput, err := NormalizePageInput(pageInput)
	if err != nil {
		return GrantPage{}, err
	}
	if !scope.valid() {
		return GrantPage{Data: []Grant{}, Pagination: PageInfo{Limit: pageInput.Limit}, Scope: "project"}, nil
	}
	if filter.SubjectWorkloadApplicationID < 0 {
		return GrantPage{}, ErrInvalid
	}
	rows, err := s.db.Query(ctx, grantSelect+` WHERE $1::text='' AND ag.org_id=$5 AND ag.project_id=$6 AND (`+grantAccessPredicate(objectRead, scope.EnforceOrgBoundary)+`)
		AND ($7::bigint=0 OR ag.subject_workload_application_id=$7)
		AND ($8::bigint=0 OR ag.id<$8) ORDER BY ag.id DESC LIMIT $9`,
		"", scope.UserID, scope.OrgID, scope.OrganizationRole, orgID, projectID,
		filter.SubjectWorkloadApplicationID, pageInput.AfterID, pageInput.Limit+1)
	if err != nil {
		return GrantPage{}, err
	}
	defer rows.Close()
	result := make([]Grant, 0)
	for rows.Next() {
		grant, err := scanGrant(rows)
		if err != nil {
			return GrantPage{}, err
		}
		result = append(result, grant)
	}
	if err := rows.Err(); err != nil {
		return GrantPage{}, err
	}
	page := GrantPage{Data: result, Pagination: PageInfo{Limit: pageInput.Limit}, Scope: "project"}
	if len(page.Data) > pageInput.Limit {
		page.Data = page.Data[:pageInput.Limit]
		page.Pagination.HasMore = true
		next := page.Data[len(page.Data)-1].ID
		page.Pagination.NextAfterID = &next
	}
	return page, nil
}

// ListMyGrants returns only human Grants whose subject is the admitted actor.
// It deliberately does not require a current membership so suspended,
// revoked, expired, or first-project history remains visible without granting
// project enumeration or exposing sponsored workload access.
func (s *Service) ListMyGrants(ctx context.Context, scope ActorScope, pageInput PageInput) (GrantPage, error) {
	pageInput, err := NormalizePageInput(pageInput)
	if err != nil {
		return GrantPage{}, err
	}
	if !scope.valid() {
		return GrantPage{Data: []Grant{}, Pagination: PageInfo{Limit: pageInput.Limit}, Scope: "my"}, nil
	}
	enforceOrgBoundary := scope.EnforceOrgBoundary || scope.OrganizationRole != auth.RolePlatformAdmin
	rows, err := s.db.Query(ctx, grantSelect+`
		WHERE (NOT $1::boolean OR ag.org_id=$2)
		  AND ag.subject_type='user' AND ag.subject_user_id=$3
		  AND ($4::bigint=0 OR ag.id<$4)
		ORDER BY ag.id DESC LIMIT $5`, enforceOrgBoundary, scope.OrgID, scope.UserID, pageInput.AfterID, pageInput.Limit+1)
	if err != nil {
		return GrantPage{}, err
	}
	defer rows.Close()
	result := make([]Grant, 0)
	for rows.Next() {
		grant, err := scanGrant(rows)
		if err != nil {
			return GrantPage{}, err
		}
		result = append(result, grant)
	}
	if err := rows.Err(); err != nil {
		return GrantPage{}, err
	}
	page := GrantPage{Data: result, Pagination: PageInfo{Limit: pageInput.Limit}, Scope: "my"}
	if len(page.Data) > pageInput.Limit {
		page.Data = page.Data[:pageInput.Limit]
		page.Pagination.HasMore = true
		next := page.Data[len(page.Data)-1].ID
		page.Pagination.NextAfterID = &next
	}
	return page, nil
}

func (s *Service) projectRequestActions(ctx context.Context, scope ActorScope, request *Request) error {
	actions := RequestActions{ReasonCode: "request_terminal"}
	request.Actions = &actions
	if request.Status != RequestNeedsReview {
		return nil
	}
	actions.CanCancel = request.SubmittedBy == scope.UserID ||
		(request.Subject.Type == SubjectWorkloadApplication && request.SponsorUserID == scope.UserID)
	if (scope.OrganizationRole == auth.RolePlatformAdmin && (!scope.EnforceOrgBoundary || scope.OrgID == request.OrgID)) ||
		(scope.OrgID == request.OrgID && scope.OrganizationRole == auth.RoleOrgAdmin) {
		actions.CanApprove = true
		actions.CanReject = true
		actions.ReasonCode = "organization_admin_actionable"
		return nil
	}
	if scope.OrgID != request.OrgID {
		actions.ReasonCode = "not_authorized"
		return nil
	}
	var actorProjectAdmin bool
	var subjectRole string
	err := s.db.QueryRow(ctx, `
		SELECT EXISTS (
		         SELECT 1 FROM project_members pm
		         WHERE pm.project_id=$1 AND pm.user_id=$2 AND pm.role='project_admin'
		       ),
		       COALESCE((
		         SELECT pm.role FROM project_members pm
		         WHERE pm.project_id=$1 AND pm.user_id=$3
		       ),'')`, request.ProjectID, scope.UserID, request.Subject.UserID).Scan(&actorProjectAdmin, &subjectRole)
	if err != nil {
		return err
	}
	if actorProjectAdmin {
		canonicalSubjectRole, _ := auth.NormalizeProjectMembershipRole(subjectRole)
		actions.CanApprove = projectAdminMayDecide(*request, canonicalSubjectRole)
		actions.CanReject = projectAdminMayReject(*request)
	}
	switch {
	case actions.CanApprove || actions.CanReject:
		actions.ReasonCode = "project_admin_actionable"
	case actions.CanCancel:
		actions.ReasonCode = "requester_actionable"
	case actorProjectAdmin:
		actions.ReasonCode = "organization_admin_required"
	default:
		actions.ReasonCode = "not_authorized"
	}
	return nil
}

func requestPage(result []Request, input PageInput, scope string) RequestPage {
	if result == nil {
		result = make([]Request, 0)
	}
	page := RequestPage{Data: result, Pagination: PageInfo{Limit: input.Limit}, Scope: scope}
	if len(page.Data) > input.Limit {
		page.Data = page.Data[:input.Limit]
		page.Pagination.HasMore = true
		next := page.Data[len(page.Data)-1].ID
		page.Pagination.NextAfterID = &next
	}
	return page
}

func (s *Service) LegacyPreview(ctx context.Context, orgID int64) ([]LegacyClassification, error) {
	if orgID <= 0 {
		return nil, ErrInvalid
	}
	type legacyKeyEvidence struct {
		summary               LegacyCredentialSummary
		workloadApplicationID int64
		accessGrantID         int64
		ownerStatus           string
		hasCallingMembership  bool
	}
	type legacyApplicationEvidence struct {
		item          LegacyClassification
		projectStatus string
		keys          []legacyKeyEvidence
	}

	applicationRows, err := s.db.Query(ctx, `
		SELECT wa.id,wa.application_ref,wa.project_id,wa.kind,wa.environment,
		       wa.identity_mode,wa.status,p.status,
		       COALESCE(ARRAY(
		         SELECT cp.client_profile
		           FROM workload_application_client_profiles cp
		          WHERE cp.workload_application_id=wa.id
		          ORDER BY cp.client_profile
		       ),ARRAY[]::text[])
		  FROM workload_applications wa
		  JOIN projects p ON p.id=wa.project_id AND p.org_id=wa.org_id
		 WHERE wa.org_id=$1
		   AND wa.identity_contract_version='unirouter.workload-identity.v1'
		 ORDER BY wa.id`, orgID)
	if err != nil {
		return nil, err
	}
	applications := make([]*legacyApplicationEvidence, 0)
	applicationByID := make(map[int64]*legacyApplicationEvidence)
	for applicationRows.Next() {
		state := &legacyApplicationEvidence{}
		if err := applicationRows.Scan(
			&state.item.ObjectID, &state.item.ApplicationRef, &state.item.ProjectID,
			&state.item.Kind, &state.item.Environment, &state.item.IdentityMode,
			&state.item.Status, &state.projectStatus, &state.item.ClientProfiles,
		); err != nil {
			applicationRows.Close()
			return nil, err
		}
		state.item.ObjectType = "workload_application"
		state.item.ObjectRef = state.item.ApplicationRef
		state.item.WorkloadApplicationID = state.item.ObjectID
		state.item.AutomaticMigration = false
		applications = append(applications, state)
		applicationByID[state.item.ObjectID] = state
	}
	if err := applicationRows.Err(); err != nil {
		applicationRows.Close()
		return nil, err
	}
	applicationRows.Close()

	keyRows, err := s.db.Query(ctx, `
		SELECT ak.id,ak.key_prefix,COALESCE(ak.project_id,0),COALESCE(ak.owner_user_id,0),
		       COALESCE(ak.workload_application_id,0),COALESCE(ak.access_grant_id,0),
		       COALESCE(ak.purpose,''),COALESCE(NULLIF(ak.key_type,''),'shared'),
		       COALESCE(u.status,''),EXISTS(
		          SELECT 1 FROM project_members pm
		           WHERE pm.project_id=ak.project_id AND pm.user_id=ak.owner_user_id
		             AND pm.role IN ('project_admin','project_user','project_developer','developer')
		       )
		  FROM api_keys ak
		  LEFT JOIN users u ON u.id=ak.owner_user_id AND u.org_id=ak.org_id
		 WHERE ak.org_id=$1
		 ORDER BY ak.id`, orgID)
	if err != nil {
		return nil, err
	}
	defer keyRows.Close()
	credentialItems := make([]LegacyClassification, 0)
	for keyRows.Next() {
		var evidence legacyKeyEvidence
		var purpose, keyType string
		if err := keyRows.Scan(
			&evidence.summary.KeyID, &evidence.summary.KeyPrefix, &evidence.summary.ProjectID,
			&evidence.summary.OwnerUserID, &evidence.workloadApplicationID,
			&evidence.accessGrantID, &purpose, &keyType, &evidence.ownerStatus,
			&evidence.hasCallingMembership,
		); err != nil {
			return nil, err
		}
		evidence.summary.KeyType = keyType
		evidence.summary.Purpose = purpose
		if application := applicationByID[evidence.workloadApplicationID]; application != nil {
			application.keys = append(application.keys, evidence)
			application.item.LinkedCredentials = append(application.item.LinkedCredentials, evidence.summary)
		}
		item := LegacyClassification{
			ObjectType: "credential", ObjectID: evidence.summary.KeyID,
			ObjectRef: evidence.summary.KeyPrefix, KeyID: evidence.summary.KeyID,
			KeyPrefix: evidence.summary.KeyPrefix, ProjectID: evidence.summary.ProjectID,
			OwnerUserID:           evidence.summary.OwnerUserID,
			WorkloadApplicationID: evidence.workloadApplicationID,
			AutomaticMigration:    false,
		}
		switch {
		case purpose != "":
			item.Classification, item.ReasonCode, item.MigrationStatus =
				"connector_separate", "purpose_credential_excluded", "excluded"
		case evidence.accessGrantID > 0:
			item.Classification, item.ReasonCode, item.MigrationStatus =
				"grant_bound", "current_access_grant", "current"
		case evidence.workloadApplicationID > 0 && applicationByID[evidence.workloadApplicationID] != nil:
			item.Classification, item.ReasonCode, item.MigrationStatus =
				"linked_to_workload_application", "classified_at_workload_application", "linked"
		case evidence.workloadApplicationID > 0:
			item.Classification, item.ReasonCode, item.MigrationStatus =
				"review_required", "unbound_current_or_foreign_workload", "review_required"
		case keyType == "personal" && evidence.summary.ProjectID <= 0:
			item.Classification, item.ReasonCode, item.MigrationStatus =
				"review_required", "personal_key_missing_project", "review_required"
		case keyType == "personal" && evidence.summary.OwnerUserID <= 0:
			item.Classification, item.ReasonCode, item.MigrationStatus =
				"review_required", "personal_key_missing_owner", "review_required"
		case keyType == "personal" && evidence.ownerStatus != "active":
			item.Classification, item.ReasonCode, item.MigrationStatus =
				"review_required", "personal_key_owner_not_active", "review_required"
		case keyType == "personal" && !evidence.hasCallingMembership:
			item.Classification, item.ReasonCode, item.MigrationStatus =
				"review_required", "personal_key_owner_not_calling", "review_required"
		case keyType == "personal":
			item.Classification, item.ReasonCode, item.MigrationStatus =
				"human_personal_candidate", "unambiguous_single_owner_project", "candidate"
		case keyType == "shared":
			item.Classification, item.ReasonCode, item.MigrationStatus =
				"legacy_unbound", "ordinary_key_without_grant", "legacy_unbound"
		default:
			item.Classification, item.ReasonCode, item.MigrationStatus =
				"review_required", "unbound_service_subject_ambiguous", "review_required"
		}
		credentialItems = append(credentialItems, item)
	}
	if err := keyRows.Err(); err != nil {
		return nil, err
	}

	classifyApplication := func(application *legacyApplicationEvidence) {
		review := func(reason string) {
			application.item.Classification = "review_required"
			application.item.ReasonCode = reason
			application.item.MigrationStatus = "review_required"
		}
		candidate := func(classification, reason string) {
			application.item.Classification = classification
			application.item.ReasonCode = reason
			application.item.MigrationStatus = "candidate"
		}
		if application.item.Kind == "interactive_client" {
			review("interactive_client_requires_review")
			return
		}
		if application.item.Status != "active" {
			review("legacy_application_not_active")
			return
		}
		if application.projectStatus != "active" {
			review("legacy_project_not_active")
			return
		}

		ordinary := make([]legacyKeyEvidence, 0, len(application.keys))
		for _, key := range application.keys {
			if key.accessGrantID > 0 {
				review("mixed_current_and_legacy_credentials")
				return
			}
			if key.summary.Purpose == "" {
				ordinary = append(ordinary, key)
			}
		}
		for _, key := range ordinary {
			if key.summary.ProjectID != application.item.ProjectID {
				review("legacy_key_project_mismatch")
				return
			}
		}
		if len(application.keys) > 0 && len(ordinary) == 0 {
			application.item.Classification = "connector_separate"
			application.item.ReasonCode = "purpose_credential_excluded"
			application.item.MigrationStatus = "excluded"
			return
		}

		switch application.item.IdentityMode {
		case "personal_development":
			if len(ordinary) == 0 {
				review("personal_development_missing_owner")
				return
			}
			owners := make(map[int64]struct{})
			for _, key := range ordinary {
				if key.summary.OwnerUserID <= 0 {
					review("personal_development_missing_owner")
					return
				}
				owners[key.summary.OwnerUserID] = struct{}{}
				if key.ownerStatus != "active" || !key.hasCallingMembership {
					review("personal_owner_not_active_or_calling")
					return
				}
			}
			if len(owners) != 1 {
				review("personal_development_multiple_owners")
				return
			}
			for ownerID := range owners {
				application.item.OwnerUserID = ownerID
			}
			candidate("human_personal_candidate", "unambiguous_personal_development_owner_project")
		case "service_credential":
			owners := make(map[int64]struct{})
			hasUnowned := false
			for _, key := range ordinary {
				if key.summary.OwnerUserID <= 0 {
					hasUnowned = true
					continue
				}
				owners[key.summary.OwnerUserID] = struct{}{}
				if key.ownerStatus != "active" || !key.hasCallingMembership {
					review("nonhuman_workload_owner_not_active_or_calling")
					return
				}
			}
			if len(owners) > 1 || (hasUnowned && len(owners) > 0) {
				review("nonhuman_workload_mixed_key_ownership")
				return
			}
			for ownerID := range owners {
				application.item.OwnerUserID = ownerID
			}
			candidate("workload_candidate", "unambiguous_nonhuman_workload")
		default:
			review("unsupported_legacy_identity")
		}
	}

	result := make([]LegacyClassification, 0, len(applications)+len(credentialItems))
	for _, application := range applications {
		application.item.LinkedCredentialCount = len(application.keys)
		classifyApplication(application)
		result = append(result, application.item)
	}
	result = append(result, credentialItems...)
	return result, nil
}

func isSubset(candidate, ceiling []string) bool {
	if len(ceiling) == 0 {
		return true
	}
	allowed := make(map[string]struct{}, len(ceiling))
	for _, value := range ceiling {
		allowed[value] = struct{}{}
	}
	for _, value := range candidate {
		if _, ok := allowed[value]; !ok {
			return false
		}
	}
	return true
}

type resolvedCredentialScope struct {
	allowedIPs  string
	models      []string
	endpoints   []string
	rpm         int
	rps         int
	concurrency *int
}

type logicalCredential struct {
	ID               int64
	Ref              string
	Name             string
	AllowedIPs       string
	AllowedModels    string
	AllowedEndpoints string
	PolicyLabels     string
	LegacyBudget     sql.NullInt64
	RPM              int
	RPS              int
	Concurrency      sql.NullInt64
	Status           string
}

func (credential logicalCredential) matches(input CredentialInput, resolved resolvedCredentialScope) bool {
	return input.Name == credential.Name &&
		resolved.allowedIPs == credential.AllowedIPs &&
		strings.Join(resolved.models, ",") == credential.AllowedModels &&
		strings.Join(resolved.endpoints, ",") == credential.AllowedEndpoints &&
		resolved.rpm == credential.RPM && resolved.rps == credential.RPS &&
		nullIntMatchesPointer(credential.Concurrency, resolved.concurrency)
}

func nullIntMatchesPointer(value sql.NullInt64, pointer *int) bool {
	return (!value.Valid && pointer == nil) || (value.Valid && pointer != nil && value.Int64 == int64(*pointer))
}

func resolveCredentialScope(input CredentialInput, grant Grant) (resolvedCredentialScope, error) {
	resolved := resolvedCredentialScope{allowedIPs: input.AllowedIPs, models: input.AllowedModels, endpoints: input.AllowedEndpoints}
	switch grant.Envelope.ScopeMode {
	case accessscope.ModeProjectDefault:
		// Empty models deliberately retain the dynamic project catalog/model-set
		// boundary. Endpoints have a fixed default contract so an empty legacy
		// key field cannot accidentally mean every future Gateway endpoint.
		if len(resolved.endpoints) == 0 {
			resolved.endpoints = accessscope.ProjectDefaultEndpoints()
		}
		if !isSubset(resolved.endpoints, accessscope.ProjectDefaultEndpoints()) {
			return resolvedCredentialScope{}, fmt.Errorf("%w: credential endpoint scope widens project default", ErrConflict)
		}
	case accessscope.ModeExplicit:
		if len(resolved.models) == 0 {
			resolved.models = cloneStrings(grant.Envelope.Models)
		}
		if len(resolved.endpoints) == 0 {
			resolved.endpoints = cloneStrings(grant.Envelope.Endpoints)
		}
		if !isSubset(resolved.models, grant.Envelope.Models) || !isSubset(resolved.endpoints, grant.Envelope.Endpoints) {
			return resolvedCredentialScope{}, fmt.Errorf("%w: credential scope widens grant", ErrConflict)
		}
	default:
		return resolvedCredentialScope{}, fmt.Errorf("%w: unsupported grant scope mode", ErrConflict)
	}
	var err error
	resolved.rpm, err = boundedInt(input.RPMLimit, grant.Envelope.RPMLimit, accessscope.ProjectDefaultRPMLimit)
	if err != nil {
		return resolvedCredentialScope{}, err
	}
	resolved.rps, err = boundedInt(input.RPSLimit, grant.Envelope.RPSLimit, accessscope.ProjectDefaultRPSLimit)
	if err != nil {
		return resolvedCredentialScope{}, err
	}
	resolved.concurrency, err = boundedOptionalInt(input.ConcurrencyLimit, grant.Envelope.ConcurrencyLimit)
	if err != nil {
		return resolvedCredentialScope{}, err
	}
	return resolved, nil
}

func boundedOptionalInt(requested, ceiling *int) (*int, error) {
	if requested == nil {
		return nil, nil
	}
	if ceiling != nil && *requested > *ceiling {
		return nil, fmt.Errorf("%w: credential limit widens grant", ErrConflict)
	}
	value := *requested
	return &value, nil
}

func boundedInt(requested, ceiling *int, fallback int) (int, error) {
	effectiveCeiling := fallback
	if ceiling != nil {
		effectiveCeiling = *ceiling
	}
	result := effectiveCeiling
	if requested != nil {
		result = *requested
	}
	if result > effectiveCeiling {
		return 0, fmt.Errorf("%w: credential limit widens grant", ErrConflict)
	}
	return result, nil
}

func normalizeAuthorizationError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrForbidden) || errors.Is(err, ErrAuthorizationChanged) {
		return err
	}
	return err
}

func classifyDatabaseError(operation string, err error) error {
	if isUniqueViolation(err) {
		return fmt.Errorf("%w: %s", ErrConflict, operation)
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func isConstraintViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}

func operationSemanticHash(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("%w: operation semantic payload", ErrInvalid)
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func credentialOperationIntent(kind string, input CredentialInput) any {
	return struct {
		Kind                   string   `json:"kind"`
		GrantRef               string   `json:"grant_ref"`
		Name                   string   `json:"name"`
		AllowedIPs             string   `json:"allowed_ips"`
		AllowedModels          []string `json:"allowed_models"`
		AllowedEndpoints       []string `json:"allowed_endpoints"`
		LegacyPolicyLabels     []string `json:"legacy_policy_labels,omitempty"`
		LegacyBudgetLimitCents *int64   `json:"legacy_budget_limit_cents,omitempty"`
		RPMLimit               *int     `json:"rpm_limit,omitempty"`
		RPSLimit               *int     `json:"rps_limit,omitempty"`
		ConcurrencyLimit       *int     `json:"concurrency_limit,omitempty"`
	}{
		Kind: kind, GrantRef: input.GrantRef, Name: input.Name, AllowedIPs: input.AllowedIPs,
		AllowedModels: input.AllowedModels, AllowedEndpoints: input.AllowedEndpoints,
		LegacyPolicyLabels: input.LegacyPolicyLabels, LegacyBudgetLimitCents: input.LegacyBudgetLimitCents,
		RPMLimit: input.RPMLimit, RPSLimit: input.RPSLimit,
		ConcurrencyLimit: input.ConcurrencyLimit,
	}
}

func writeAuditAndOutboxTx(ctx context.Context, tx pgx.Tx, orgID, projectID, actorUserID int64,
	action, resource, resourceID, requestID, clientRequestID string, detail map[string]any, now time.Time,
) error {
	return accessevidence.WriteAuditAndOutbox(ctx, tx, orgID, projectID, actorUserID,
		action, resource, resourceID, requestID, clientRequestID, detail, now)
}
