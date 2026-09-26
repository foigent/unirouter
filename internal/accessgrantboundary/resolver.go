// Package accessgrantboundary owns the single live authorization read used by
// root API-key authentication and Agent capability attenuation. It deliberately
// does not issue credentials or decide grants.
package accessgrantboundary

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/uniroute/unirouter/internal/accessscope"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/executioncontext"
	"github.com/uniroute/unirouter/internal/models"
)

var ErrInactive = errors.New("access grant boundary is inactive")

const CapabilityWorkloadKind = "agent"

type Snapshot struct {
	APIKey                  models.ApiKey
	OrgStatus               string
	ProjectStatus           string
	OwnerStatus             string
	OwnerRole               string
	OwnerOrganizationRole   string
	MembershipRole          string
	WorkloadStatus          string
	WorkloadKind            string
	WorkloadIdentity        string
	WorkloadEnvironment     string
	WorkloadProductionUse   bool
	AccessCredentialStatus  string
	GrantRevision           int64
	GrantSubjectType        string
	GrantSubjectUserID      int64
	GrantSubjectWorkloadID  int64
	GrantSponsorUserID      int64
	SponsorOrgID            int64
	SponsorStatus           string
	SponsorOrganizationRole string
	SponsorMembershipRole   string
	GrantProjectRole        string
	GrantEnvironment        string
	GrantProductionUse      bool
	GrantCurrentlyActive    bool
}

func (s Snapshot) CapabilityManagementAllowed() bool {
	if !s.APIKey.GrantBound {
		return true
	}
	return s.GrantSubjectType == "workload_application" &&
		s.WorkloadStatus == "active" &&
		s.WorkloadIdentity == "unirouter.workload-identity.v2" &&
		s.WorkloadKind == CapabilityWorkloadKind &&
		s.WorkloadEnvironment == s.GrantEnvironment &&
		s.WorkloadProductionUse == s.GrantProductionUse
}

// Resolve rereads the complete current key/grant/subject boundary. Invalid,
// expired, suspended, cross-subject, or cross-project bindings return
// ErrInactive without exposing which predicate failed.
func Resolve(ctx context.Context, q db.DBQuerier, keyID int64) (Snapshot, error) {
	var snapshot Snapshot
	var projectID, ownerUserID, workloadID sql.NullInt64
	var accessGrantID, accessCredentialID sql.NullInt64
	var keyBudget, keyConcurrency sql.NullInt64
	var credentialConcurrency sql.NullInt64
	var credentialBudget sql.NullInt64
	var grantRPM, grantRPS, grantConcurrency sql.NullInt64
	var credentialRef, credentialName, credentialAllowedIPs, credentialAllowedModels string
	var credentialAllowedEndpoints, credentialPolicyLabels string
	var credentialRPM, credentialRPS int
	var modelsJSON, endpointsJSON []byte
	err := q.QueryRow(ctx, `
		SELECT ak.id,ak.org_id,ak.project_id,ak.owner_user_id,ak.workload_application_id,
		       COALESCE(NULLIF(ak.key_type,''),'shared'),COALESCE(ak.purpose,''),ak.name,
		       ak.key_prefix,ak.status,COALESCE(ak.allowed_ips,''),COALESCE(ak.allowed_models,''),
		       COALESCE(ak.allowed_endpoints,''),COALESCE(ak.policy_labels,''),
		       ak.rpm_limit,ak.qps_limit,ak.concurrency_limit,ak.budget_limit,
		       ak.auth_epoch,ak.access_grant_id,ak.access_credential_id,COALESCE(ak.credential_ref,''),
		       o.status,COALESCE(p.status,''),COALESCE(u.status,''),COALESCE(u.role,''),
		       COALESCE(u.organization_role,''),
		       COALESCE(pm.role,''),COALESCE(app.status,''),COALESCE(app.kind,''),
		       COALESCE(app.identity_contract_version,''),COALESCE(app.environment,''),COALESCE(app.production_use,FALSE),
		       COALESCE(ag.revision,0),COALESCE(ag.subject_type,''),
		       COALESCE(ag.subject_user_id,0),COALESCE(ag.subject_workload_application_id,0),
		       COALESCE(ag.sponsor_user_id,0),COALESCE(sponsor.org_id,0),COALESCE(sponsor.status,''),
		       COALESCE(sponsor.organization_role,''),COALESCE(sponsor_pm.role,''),
		       COALESCE(ag.project_role_snapshot,''),COALESCE(ag.environment,''),COALESCE(ag.production_use,FALSE),
		       COALESCE(ag.status='active' AND ag.valid_from<=NOW()
		         AND (ag.valid_until IS NULL OR ag.valid_until>NOW()),FALSE),
		       COALESCE(ag.scope_mode,''),COALESCE(ag.allowed_models,'[]'::jsonb),
		       COALESCE(ag.allowed_endpoints,'[]'::jsonb),ag.rpm_limit,ag.rps_limit,
		       ag.concurrency_limit,
		       COALESCE(ac.status,''),COALESCE(ac.name,''),COALESCE(ac.allowed_ips,''),
		       COALESCE(ac.allowed_models,''),COALESCE(ac.allowed_endpoints,''),COALESCE(ac.policy_labels,''),
		       COALESCE(ac.rpm_limit,0),COALESCE(ac.rps_limit,0),ac.concurrency_limit,
		       ac.legacy_budget_limit_cents
		  FROM api_keys ak
		  JOIN orgs o ON o.id=ak.org_id
		  LEFT JOIN projects p ON p.id=ak.project_id AND p.org_id=ak.org_id
		  LEFT JOIN users u ON u.id=ak.owner_user_id AND u.org_id=ak.org_id
		  LEFT JOIN project_members pm ON pm.project_id=ak.project_id AND pm.user_id=ak.owner_user_id
		  LEFT JOIN workload_applications app
		    ON app.id=ak.workload_application_id AND app.org_id=ak.org_id AND app.project_id=ak.project_id
		  LEFT JOIN access_grants ag
		    ON ag.id=ak.access_grant_id AND ag.org_id=ak.org_id AND ag.project_id=ak.project_id
		  LEFT JOIN users sponsor
		    ON sponsor.id=ag.sponsor_user_id AND sponsor.org_id=ag.org_id
		  LEFT JOIN project_members sponsor_pm
		    ON sponsor_pm.project_id=ag.project_id AND sponsor_pm.user_id=ag.sponsor_user_id
		  LEFT JOIN access_credentials ac
		    ON ac.id=ak.access_credential_id AND ac.credential_ref=ak.credential_ref
		   AND ac.access_grant_id=ak.access_grant_id AND ac.org_id=ak.org_id AND ac.project_id=ak.project_id
		 WHERE ak.id=$1`, keyID).Scan(
		&snapshot.APIKey.ID, &snapshot.APIKey.OrgID, &projectID, &ownerUserID, &workloadID,
		&snapshot.APIKey.KeyType, &snapshot.APIKey.Purpose, &snapshot.APIKey.Name,
		&snapshot.APIKey.KeyPrefix, &snapshot.APIKey.Status, &snapshot.APIKey.IPWhitelist,
		&snapshot.APIKey.AllowedModels, &snapshot.APIKey.AllowedEndpoints, &snapshot.APIKey.PolicyLabels,
		&snapshot.APIKey.RPMLimit, &snapshot.APIKey.QPSLimit, &keyConcurrency, &keyBudget,
		&snapshot.APIKey.AuthEpoch, &accessGrantID, &accessCredentialID, &credentialRef,
		&snapshot.OrgStatus, &snapshot.ProjectStatus, &snapshot.OwnerStatus, &snapshot.OwnerRole,
		&snapshot.OwnerOrganizationRole,
		&snapshot.MembershipRole, &snapshot.WorkloadStatus, &snapshot.WorkloadKind,
		&snapshot.WorkloadIdentity, &snapshot.WorkloadEnvironment, &snapshot.WorkloadProductionUse,
		&snapshot.GrantRevision, &snapshot.GrantSubjectType,
		&snapshot.GrantSubjectUserID, &snapshot.GrantSubjectWorkloadID,
		&snapshot.GrantSponsorUserID, &snapshot.SponsorOrgID, &snapshot.SponsorStatus,
		&snapshot.SponsorOrganizationRole, &snapshot.SponsorMembershipRole,
		&snapshot.GrantProjectRole, &snapshot.GrantEnvironment, &snapshot.GrantProductionUse,
		&snapshot.GrantCurrentlyActive,
		&snapshot.APIKey.GrantScopeMode, &modelsJSON, &endpointsJSON,
		&grantRPM, &grantRPS, &grantConcurrency,
		&snapshot.AccessCredentialStatus, &credentialName, &credentialAllowedIPs, &credentialAllowedModels,
		&credentialAllowedEndpoints, &credentialPolicyLabels, &credentialRPM, &credentialRPS,
		&credentialConcurrency, &credentialBudget,
	)
	if err != nil {
		return Snapshot{}, err
	}
	if projectID.Valid {
		value := projectID.Int64
		snapshot.APIKey.ProjectID = &value
	}
	if ownerUserID.Valid {
		value := ownerUserID.Int64
		snapshot.APIKey.OwnerUserID = &value
	}
	if workloadID.Valid {
		value := workloadID.Int64
		snapshot.APIKey.WorkloadApplicationID = &value
	}
	if keyBudget.Valid {
		value := int(keyBudget.Int64)
		snapshot.APIKey.BudgetLimit = &value
	}
	if accessGrantID.Valid {
		value := accessGrantID.Int64
		snapshot.APIKey.AccessGrantID = &value
		snapshot.APIKey.GrantBound = true
	}
	if accessCredentialID.Valid {
		value := accessCredentialID.Int64
		snapshot.APIKey.AccessCredentialID = &value
		snapshot.APIKey.CredentialRef = credentialRef
	}
	snapshot.APIKey.CredentialConcurrencyLimit = intPointer(keyConcurrency)
	snapshot.APIKey.GrantRPMLimit = intPointer(grantRPM)
	snapshot.APIKey.GrantRPSLimit = intPointer(grantRPS)
	if snapshot.APIKey.GrantBound {
		// NULL is the persisted representation of the canonical project-default
		// Grant rates, never an omitted enforcement layer. Materialize the
		// effective values at the shared live boundary so every older physical
		// Credential, Agent capability, synchronous provider admission, and media
		// admission sees the same 60 RPM / 10 RPS ceiling. Explicit zero remains
		// an exact denial.
		if snapshot.APIKey.GrantRPMLimit == nil {
			value := accessscope.ProjectDefaultRPMLimit
			snapshot.APIKey.GrantRPMLimit = &value
		}
		if snapshot.APIKey.GrantRPSLimit == nil {
			value := accessscope.ProjectDefaultRPSLimit
			snapshot.APIKey.GrantRPSLimit = &value
		}
	}
	snapshot.APIKey.GrantConcurrencyLimit = intPointer(grantConcurrency)

	if !active(snapshot) {
		return Snapshot{}, ErrInactive
	}
	if snapshot.APIKey.GrantBound {
		if snapshot.APIKey.AccessCredentialID == nil || snapshot.APIKey.CredentialRef == "" ||
			snapshot.AccessCredentialStatus != "active" || credentialName != snapshot.APIKey.Name ||
			credentialAllowedIPs != snapshot.APIKey.IPWhitelist || credentialAllowedModels != snapshot.APIKey.AllowedModels ||
			credentialAllowedEndpoints != snapshot.APIKey.AllowedEndpoints || credentialPolicyLabels != snapshot.APIKey.PolicyLabels ||
			credentialRPM != snapshot.APIKey.RPMLimit || credentialRPS != snapshot.APIKey.QPSLimit ||
			!sameNullInt(keyConcurrency, credentialConcurrency) || !sameNullInt(keyBudget, credentialBudget) {
			return Snapshot{}, ErrInactive
		}
		var grantModels, grantEndpoints []string
		if json.Unmarshal(modelsJSON, &grantModels) != nil || json.Unmarshal(endpointsJSON, &grantEndpoints) != nil {
			return Snapshot{}, ErrInactive
		}
		switch snapshot.APIKey.GrantScopeMode {
		case accessscope.ModeProjectDefault:
			if len(grantModels) != 0 || len(grantEndpoints) != 0 {
				return Snapshot{}, ErrInactive
			}
			snapshot.APIKey.AllowedEndpoints, snapshot.APIKey.EndpointDenied = intersect(
				snapshot.APIKey.AllowedEndpoints, accessscope.ProjectDefaultEndpoints(),
			)
		case accessscope.ModeExplicit:
			if !accessscope.Validate(accessscope.ModeExplicit, grantModels, grantEndpoints) {
				return Snapshot{}, ErrInactive
			}
			snapshot.APIKey.AllowedModels, snapshot.APIKey.ModelScopeDenied = intersect(snapshot.APIKey.AllowedModels, grantModels)
			snapshot.APIKey.AllowedEndpoints, snapshot.APIKey.EndpointDenied = intersect(snapshot.APIKey.AllowedEndpoints, grantEndpoints)
		default:
			return Snapshot{}, ErrInactive
		}
	}
	identity := snapshot.ExecutionIdentity()
	snapshot.APIKey.ExecutionIdentity = &identity
	return snapshot, nil
}

// ExecutionIdentity projects this exact admitted boundary without another
// authority lookup. It deliberately omits names, email and credential material.
func (s Snapshot) ExecutionIdentity() executioncontext.Identity {
	identity := executioncontext.Identity{ActualHuman: "unknown", AuthorityRead: "live_boundary"}
	if s.APIKey.OwnerUserID != nil {
		identity.OwnerUserID = executioncontext.ID(*s.APIKey.OwnerUserID)
	}
	if s.APIKey.AccessGrantID != nil {
		identity.GrantID = executioncontext.ID(*s.APIKey.AccessGrantID)
	}
	identity.GrantRevision = executioncontext.ID(s.GrantRevision)
	identity.GrantSubjectType = s.GrantSubjectType
	identity.GrantSubjectUserID = executioncontext.ID(s.GrantSubjectUserID)
	identity.GrantSubjectWorkloadID = executioncontext.ID(s.GrantSubjectWorkloadID)
	identity.SponsorUserID = executioncontext.ID(s.GrantSponsorUserID)
	return identity
}

func active(s Snapshot) bool {
	key := s.APIKey
	if key.ID <= 0 || key.Status != "active" || s.OrgStatus != "active" {
		return false
	}
	if key.ProjectID != nil && s.ProjectStatus != "active" {
		return false
	}
	if key.OwnerUserID != nil && (s.OwnerStatus != "active" || s.OwnerOrganizationRole == "org_viewer" ||
		(key.ProjectID != nil && !callingMembership(s.MembershipRole))) {
		return false
	}
	if key.WorkloadApplicationID != nil && s.WorkloadStatus != "active" {
		return false
	}
	if !key.GrantBound {
		return true
	}
	if s.GrantRevision <= 0 || key.ProjectID == nil || s.AccessCredentialStatus != "active" ||
		key.AccessCredentialID == nil || key.CredentialRef == "" || !s.GrantCurrentlyActive ||
		!callingMembership(s.GrantProjectRole) {
		return false
	}
	// The access_grants join is intentionally scoped by organization/project.
	// A missing or inactive/expired grant yields zero revision in this snapshot.
	if s.GrantSubjectType == "user" {
		return key.OwnerUserID != nil && *key.OwnerUserID == s.GrantSubjectUserID &&
			key.WorkloadApplicationID == nil && s.GrantSponsorUserID == 0 && callingMembership(s.MembershipRole)
	}
	return s.GrantSubjectType == "workload_application" && key.WorkloadApplicationID != nil &&
		*key.WorkloadApplicationID == s.GrantSubjectWorkloadID && key.OwnerUserID == nil &&
		s.WorkloadIdentity == "unirouter.workload-identity.v2" && s.WorkloadEnvironment == s.GrantEnvironment &&
		s.WorkloadProductionUse == s.GrantProductionUse &&
		validWorkloadSponsor(s)
}

func validWorkloadSponsor(s Snapshot) bool {
	if s.GrantSponsorUserID <= 0 || s.SponsorOrgID != s.APIKey.OrgID || s.SponsorStatus != "active" {
		return false
	}
	if s.SponsorOrganizationRole == "platform_admin" || s.SponsorOrganizationRole == "org_admin" {
		return true
	}
	return callingMembership(s.SponsorMembershipRole)
}

func sameNullInt(left, right sql.NullInt64) bool {
	return left.Valid == right.Valid && (!left.Valid || left.Int64 == right.Int64)
}

func callingMembership(role string) bool {
	switch role {
	case "project_admin", "project_user", "project_developer", "developer":
		return true
	default:
		return false
	}
}

func intersect(raw string, grant []string) (string, bool) {
	base := split(raw)
	grantSet := make(map[string]struct{}, len(grant))
	for _, value := range grant {
		if value = strings.TrimSpace(value); value != "" {
			grantSet[value] = struct{}{}
		}
	}
	if len(grantSet) == 0 {
		return "", true
	}
	_, wildcard := base["*"]
	if len(base) == 0 || wildcard {
		values := make([]string, 0, len(grantSet))
		for value := range grantSet {
			values = append(values, value)
		}
		slices.Sort(values)
		return strings.Join(values, ","), false
	}
	values := make([]string, 0)
	for value := range base {
		if _, ok := grantSet[value]; ok {
			values = append(values, value)
		}
	}
	slices.Sort(values)
	if len(values) == 0 {
		return "", true
	}
	return strings.Join(values, ","), false
}

func split(raw string) map[string]struct{} {
	result := make(map[string]struct{})
	for _, value := range strings.Split(raw, ",") {
		if value = strings.TrimSpace(value); value != "" {
			result[value] = struct{}{}
		}
	}
	return result
}

func intPointer(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	result := int(value.Int64)
	return &result
}
