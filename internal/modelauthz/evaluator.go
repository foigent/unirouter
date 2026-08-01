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

	"github.com/uniroute/unirouter/internal/db"
)

type Evaluator struct {
	db          db.DBQuerier
	environment string
	now         func() time.Time
}

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
	policies, err := e.loadActive(ctx, input)
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
   AND ((subject_type = 'api_key' AND subject_ref = $4)
     OR (subject_type = 'project' AND subject_ref = $5)
     OR (subject_type = 'user' AND subject_ref = $6)
     OR (subject_type = 'agent' AND subject_ref = $7))
 ORDER BY subject_type, subject_ref, authorization_ref`,
		input.OrgID,
		e.environment,
		input.Now.UTC(),
		positiveRef(input.APIKeyID),
		positiveRef(input.ProjectID),
		positiveRef(input.OwnerUserID),
		strings.TrimSpace(input.AgentID),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: query active policies: %v", ErrUnavailable, err)
	}
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

func EvaluatePolicies(input Input, policies []Policy) Decision {
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
