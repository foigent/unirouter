package modelauthz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/accessscope"
	"github.com/uniroute/unirouter/internal/db"
)

type Evaluator struct {
	db          db.DBQuerier
	environment string
	now         func() time.Time
}

const maxBatchEvaluationInputs = 100

func NewEvaluator(database db.DBQuerier, environment string) (*Evaluator, error) {
	if database == nil {
		return nil, fmt.Errorf("model authorization evaluator requires a database")
	}
	environment = strings.TrimSpace(strings.ToLower(environment))
	if environment == "" {
		environment = "development"
	}
	if !environmentPat.MatchString(environment) {
		return nil, fmt.Errorf("model authorization environment is invalid")
	}
	return &Evaluator{db: database, environment: environment, now: time.Now}, nil
}

func (e *Evaluator) Environment() string {
	if e == nil {
		return ""
	}
	return e.environment
}

func (e *Evaluator) Evaluate(ctx context.Context, input Input) (Decision, error) {
	if e == nil || e.db == nil || input.OrgID <= 0 || strings.TrimSpace(input.Endpoint) == "" || strings.TrimSpace(input.RequestedModel) == "" {
		return Decision{}, fmt.Errorf("%w: evaluation_input", ErrInvalid)
	}
	input.Environment = e.environment
	if input.Now.IsZero() {
		input.Now = e.now().UTC()
	}
	return e.EvaluateWithQuerier(ctx, e.db, input)
}

// EvaluateAny evaluates a bounded set of alternative physical/logical
// credentials against one authoritative policy read. Readiness uses it to
// answer whether at least one currently effective credential can call without
// issuing one database query per denied credential.
func (e *Evaluator) EvaluateAny(ctx context.Context, inputs []Input) (Decision, error) {
	if e == nil || e.db == nil || len(inputs) == 0 || len(inputs) > maxBatchEvaluationInputs {
		return Decision{}, fmt.Errorf("%w: evaluation_inputs", ErrInvalid)
	}
	now := e.now().UTC()
	orgID := inputs[0].OrgID
	for index := range inputs {
		if inputs[index].OrgID <= 0 || inputs[index].OrgID != orgID ||
			strings.TrimSpace(inputs[index].Endpoint) == "" || strings.TrimSpace(inputs[index].RequestedModel) == "" {
			return Decision{}, fmt.Errorf("%w: evaluation_inputs", ErrInvalid)
		}
		inputs[index].Environment = e.environment
		if inputs[index].Now.IsZero() {
			inputs[index].Now = now
		}
	}
	policies, err := e.loadActiveForInputs(ctx, inputs)
	if err != nil {
		return Decision{}, err
	}
	var denied Decision
	for _, input := range inputs {
		decision := EvaluatePolicies(input, policiesForInput(input, policies))
		if decision.Allowed {
			return decision, nil
		}
		denied = decision
	}
	return denied, nil
}

// EvaluateWithQuerier repeats the same policy evaluation in a caller-owned
// transaction. Durable dispatch uses it after locking the current logical
// credential/Grant boundary and before recording the Provider execution fence.
func (e *Evaluator) EvaluateWithQuerier(ctx context.Context, q db.DBQuerier, input Input) (Decision, error) {
	if e == nil || q == nil || input.OrgID <= 0 || strings.TrimSpace(input.Endpoint) == "" || strings.TrimSpace(input.RequestedModel) == "" {
		return Decision{}, fmt.Errorf("%w: evaluation_input", ErrInvalid)
	}
	input.Environment = e.environment
	if input.Now.IsZero() {
		input.Now = e.now().UTC()
	}
	policies, err := e.loadActiveWithQuerier(ctx, q, input)
	if err != nil {
		return Decision{}, err
	}
	return EvaluatePolicies(input, policies), nil
}

// FilterModels applies one authoritative policy snapshot to a catalog. It is
// used by GET /v1/models so catalog visibility cannot exceed call admission.
func (e *Evaluator) FilterModels(ctx context.Context, input Input, candidates []string) ([]string, error) {
	if e == nil || e.db == nil || input.OrgID <= 0 || strings.TrimSpace(input.Endpoint) == "" {
		return nil, fmt.Errorf("%w: evaluation_input", ErrInvalid)
	}
	input.Environment = e.environment
	if input.Now.IsZero() {
		input.Now = e.now().UTC()
	}
	policies, err := e.loadActive(ctx, input)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		input.RequestedModel = candidate
		input.ResolvedModel = candidate
		if EvaluatePolicies(input, policies).Allowed {
			result = append(result, candidate)
		}
	}
	return result, nil
}

func (e *Evaluator) loadActive(ctx context.Context, input Input) ([]Policy, error) {
	return e.loadActiveWithQuerier(ctx, e.db, input)
}

func (e *Evaluator) loadActiveWithQuerier(ctx context.Context, q db.DBQuerier, input Input) ([]Policy, error) {
	rows, err := q.Query(ctx, `
SELECT id, org_id, authorization_ref, subject_type, subject_ref, environment,
       effect, allowed_models, allowed_endpoints, not_before, expires_at,
       status, revision, reason, created_by, activated_by, revoked_by,
       created_at, activated_at, revoked_at, updated_at
  FROM model_call_authorizations
 WHERE org_id = $1
   AND status = 'active'
   AND (environment = '*' OR environment = $2)
   AND (not_before IS NULL OR not_before <= $3)
   AND (expires_at IS NULL OR expires_at > $3)
   AND ((subject_type = 'api_key' AND subject_ref = $4)
     OR (subject_type = 'access_credential' AND subject_ref = $5)
     OR (subject_type = 'project' AND subject_ref = $6)
     OR (subject_type = 'user' AND subject_ref = $7)
     OR (subject_type = 'agent' AND subject_ref = $8))
 ORDER BY subject_type, subject_ref, authorization_ref`,
		input.OrgID,
		e.environment,
		input.Now.UTC(),
		positiveRef(input.APIKeyID),
		positiveRef(input.AccessCredentialID),
		positiveRef(input.ProjectID),
		positiveRef(input.OwnerUserID),
		strings.TrimSpace(input.AgentID),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: query active policies: %v", ErrUnavailable, err)
	}
	return scanActivePolicies(rows)
}

func (e *Evaluator) loadActiveForInputs(ctx context.Context, inputs []Input) ([]Policy, error) {
	apiKeys := make([]string, 0, len(inputs))
	credentials := make([]string, 0, len(inputs))
	projects := make([]string, 0, len(inputs))
	users := make([]string, 0, len(inputs))
	agents := make([]string, 0, len(inputs))
	seen := make(map[string]struct{}, len(inputs)*5)
	appendUnique := func(values *[]string, kind, value string) {
		if value == "" {
			return
		}
		key := kind + ":" + value
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		*values = append(*values, value)
	}
	for _, input := range inputs {
		appendUnique(&apiKeys, SubjectAPIKey, positiveRef(input.APIKeyID))
		appendUnique(&credentials, SubjectAccessCredential, positiveRef(input.AccessCredentialID))
		appendUnique(&projects, SubjectProject, positiveRef(input.ProjectID))
		appendUnique(&users, SubjectUser, positiveRef(input.OwnerUserID))
		appendUnique(&agents, SubjectAgent, strings.TrimSpace(input.AgentID))
	}
	rows, err := e.db.Query(ctx, `
SELECT id, org_id, authorization_ref, subject_type, subject_ref, environment,
       effect, allowed_models, allowed_endpoints, not_before, expires_at,
       status, revision, reason, created_by, activated_by, revoked_by,
       created_at, activated_at, revoked_at, updated_at
  FROM model_call_authorizations
 WHERE org_id = $1
   AND status = 'active'
   AND (environment = '*' OR environment = $2)
   AND (not_before IS NULL OR not_before <= $3)
   AND (expires_at IS NULL OR expires_at > $3)
   AND ((subject_type = 'api_key' AND subject_ref=ANY($4::text[]))
     OR (subject_type = 'access_credential' AND subject_ref=ANY($5::text[]))
     OR (subject_type = 'project' AND subject_ref=ANY($6::text[]))
     OR (subject_type = 'user' AND subject_ref=ANY($7::text[]))
     OR (subject_type = 'agent' AND subject_ref=ANY($8::text[])))
 ORDER BY subject_type, subject_ref, authorization_ref`, inputs[0].OrgID, e.environment,
		inputs[0].Now.UTC(), apiKeys, credentials, projects, users, agents)
	if err != nil {
		return nil, fmt.Errorf("%w: query active policies: %v", ErrUnavailable, err)
	}
	return scanActivePolicies(rows)
}

func scanActivePolicies(rows pgx.Rows) ([]Policy, error) {
	defer rows.Close()
	policies := make([]Policy, 0)
	for rows.Next() {
		var policy Policy
		var modelsJSON, endpointsJSON []byte
		if err := rows.Scan(
			&policy.ID, &policy.OrgID, &policy.AuthorizationRef,
			&policy.SubjectType, &policy.SubjectRef, &policy.Environment,
			&policy.Effect, &modelsJSON, &endpointsJSON,
			&policy.NotBefore, &policy.ExpiresAt, &policy.Status,
			&policy.Revision, &policy.Reason, &policy.CreatedBy,
			&policy.ActivatedBy, &policy.RevokedBy, &policy.CreatedAt,
			&policy.ActivatedAt, &policy.RevokedAt, &policy.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("%w: scan active policy: %v", ErrUnavailable, err)
		}
		if json.Unmarshal(modelsJSON, &policy.AllowedModels) != nil || json.Unmarshal(endpointsJSON, &policy.AllowedEndpoints) != nil {
			return nil, fmt.Errorf("%w: corrupt policy", ErrUnavailable)
		}
		policies = append(policies, policy)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: iterate active policies: %v", ErrUnavailable, err)
	}
	return policies, nil
}

func policiesForInput(input Input, policies []Policy) []Policy {
	result := make([]Policy, 0, len(policies))
	for _, policy := range policies {
		matches := false
		switch policy.SubjectType {
		case SubjectAPIKey:
			matches = policy.SubjectRef == positiveRef(input.APIKeyID)
		case SubjectAccessCredential:
			matches = policy.SubjectRef == positiveRef(input.AccessCredentialID)
		case SubjectProject:
			matches = policy.SubjectRef == positiveRef(input.ProjectID)
		case SubjectUser:
			matches = policy.SubjectRef == positiveRef(input.OwnerUserID)
		case SubjectAgent:
			matches = policy.SubjectRef == strings.TrimSpace(input.AgentID)
		}
		if matches {
			result = append(result, policy)
		}
	}
	return result
}

func EvaluatePolicies(input Input, policies []Policy) Decision {
	input.Endpoint = accessscope.CanonicalExecutionEndpoint(input.Endpoint)
	decision := Decision{
		ContractVersion: ContractVersion,
		Allowed:         true,
		Reason:          "legacy_authority_unchanged",
		PolicyRefs:      []string{},
		Environment:     input.Environment,
		Endpoint:        input.Endpoint,
		RequestedModel:  input.RequestedModel,
		ResolvedModel:   input.ResolvedModel,
		RuntimeMutation: "none",
	}
	type allowGroup struct {
		refs    []string
		allowed bool
	}
	groups := make(map[string]*allowGroup)
	for _, policy := range policies {
		if policy.Status != StatusActive || !slices.Contains(policy.AllowedEndpoints, input.Endpoint) {
			continue
		}
		groupKey := policy.SubjectType + ":" + policy.SubjectRef
		if policy.Effect == EffectDeny && matchesModel(policy.AllowedModels, input.RequestedModel, input.ResolvedModel) {
			decision.Allowed = false
			decision.Reason = "explicit_deny"
			decision.PolicyRefs = []string{policy.AuthorizationRef}
			return decision
		}
		if policy.Effect != EffectAllow {
			continue
		}
		group := groups[groupKey]
		if group == nil {
			group = &allowGroup{}
			groups[groupKey] = group
		}
		group.refs = append(group.refs, policy.AuthorizationRef)
		if matchesModel(policy.AllowedModels, input.RequestedModel, input.ResolvedModel) {
			group.allowed = true
		}
	}
	if len(groups) == 0 {
		return decision
	}
	refs := make([]string, 0)
	for _, group := range groups {
		refs = append(refs, group.refs...)
		if !group.allowed {
			decision.Allowed = false
			decision.Reason = "allow_scope_excludes_model"
		}
	}
	slices.Sort(refs)
	decision.PolicyRefs = slices.Compact(refs)
	if decision.Allowed {
		decision.Reason = "explicit_allow_intersection"
	}
	return decision
}

func matchesModel(models []string, requested, resolved string) bool {
	for _, model := range models {
		if model == "*" || model == requested || (resolved != "" && model == resolved) {
			return true
		}
	}
	return false
}

func positiveRef(value int64) string {
	if value <= 0 {
		return ""
	}
	return strconv.FormatInt(value, 10)
}

func IsUnavailable(err error) bool {
	return errors.Is(err, ErrUnavailable)
}
