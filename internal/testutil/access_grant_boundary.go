package testutil

import (
	"database/sql"
	"fmt"
)

// AccessGrantBoundaryFixture mirrors the single live API-key/grant boundary
// row used by Gateway and Agent capability tests. Keeping the scan contract in
// one test helper prevents mocks from silently lagging behind the production
// authorization query.
type AccessGrantBoundaryFixture struct {
	KeyID, OrgID                          int64
	ProjectID, OwnerUserID, WorkloadID    *int64
	KeyType, Purpose, Name, Prefix        string
	KeyStatus, AllowedIPs, AllowedModels  string
	AllowedEndpoints                      string
	PolicyLabels                          string
	RPMLimit, RPSLimit                    int
	KeyBudgetCents                        *int64
	KeyConcurrencyLimit                   *int64
	AuthEpoch                             int64
	AccessGrantID                         *int64
	AccessCredentialID                    *int64
	CredentialRef                         string
	OrgStatus, ProjectStatus              string
	OwnerStatus, OwnerRole                string
	OwnerOrganizationRole                 string
	MembershipRole                        string
	WorkloadStatus, WorkloadKind          string
	WorkloadIdentity, WorkloadEnvironment string
	WorkloadProductionUse                 bool
	GrantRevision                         int64
	GrantSubjectType                      string
	GrantSubjectUserID                    int64
	GrantSubjectWorkloadID                int64
	GrantSponsorUserID                    int64
	SponsorOrgID                          int64
	SponsorStatus                         string
	SponsorOrganizationRole               string
	SponsorMembershipRole                 string
	GrantProjectRole, GrantEnvironment    string
	GrantProductionUse                    bool
	GrantCurrentlyActive                  bool
	GrantScopeMode                        string
	GrantModels, GrantEndpoints           []byte
	GrantRPMLimit, GrantRPSLimit          *int64
	GrantConcurrencyLimit                 *int64
	AccessCredentialStatus                string
	AccessCredentialConcurrencyLimit      *int64
	AccessCredentialLegacyBudgetLimit     *int64
}

func (f AccessGrantBoundaryFixture) Scan(dest ...any) error {
	if len(dest) != 62 {
		return fmt.Errorf("unexpected access-grant boundary scan width: %d", len(dest))
	}
	credentialID := f.AccessCredentialID
	credentialRef := f.CredentialRef
	credentialStatus := f.AccessCredentialStatus
	if f.AccessGrantID != nil && credentialID == nil {
		value := f.KeyID + 1000
		credentialID = &value
	}
	if credentialID != nil && credentialRef == "" {
		credentialRef = "cred_test_fixture"
	}
	if credentialID != nil && credentialStatus == "" {
		credentialStatus = "active"
	}
	sponsorUserID := f.GrantSponsorUserID
	sponsorOrgID := f.SponsorOrgID
	sponsorStatus := f.SponsorStatus
	sponsorOrganizationRole := f.SponsorOrganizationRole
	if f.AccessGrantID != nil && f.GrantSubjectType == "workload_application" && sponsorUserID == 0 {
		sponsorUserID = f.KeyID + 2000
		sponsorOrgID = f.OrgID
		sponsorStatus = "active"
		sponsorOrganizationRole = "org_admin"
	}
	credentialBudget := f.AccessCredentialLegacyBudgetLimit
	if credentialID != nil && credentialBudget == nil {
		credentialBudget = f.KeyBudgetCents
	}
	*dest[0].(*int64) = f.KeyID
	*dest[1].(*int64) = f.OrgID
	setNullInt64(dest[2].(*sql.NullInt64), f.ProjectID)
	setNullInt64(dest[3].(*sql.NullInt64), f.OwnerUserID)
	setNullInt64(dest[4].(*sql.NullInt64), f.WorkloadID)
	*dest[5].(*string) = f.KeyType
	*dest[6].(*string) = f.Purpose
	*dest[7].(*string) = f.Name
	*dest[8].(*string) = f.Prefix
	*dest[9].(*string) = f.KeyStatus
	*dest[10].(*string) = f.AllowedIPs
	*dest[11].(*string) = f.AllowedModels
	*dest[12].(*string) = f.AllowedEndpoints
	*dest[13].(*string) = f.PolicyLabels
	*dest[14].(*int) = f.RPMLimit
	*dest[15].(*int) = f.RPSLimit
	setNullInt64(dest[16].(*sql.NullInt64), f.KeyConcurrencyLimit)
	setNullInt64(dest[17].(*sql.NullInt64), f.KeyBudgetCents)
	*dest[18].(*int64) = f.AuthEpoch
	setNullInt64(dest[19].(*sql.NullInt64), f.AccessGrantID)
	setNullInt64(dest[20].(*sql.NullInt64), credentialID)
	*dest[21].(*string) = credentialRef
	*dest[22].(*string) = f.OrgStatus
	*dest[23].(*string) = f.ProjectStatus
	*dest[24].(*string) = f.OwnerStatus
	*dest[25].(*string) = f.OwnerRole
	*dest[26].(*string) = f.OwnerOrganizationRole
	*dest[27].(*string) = f.MembershipRole
	*dest[28].(*string) = f.WorkloadStatus
	*dest[29].(*string) = f.WorkloadKind
	*dest[30].(*string) = f.WorkloadIdentity
	*dest[31].(*string) = f.WorkloadEnvironment
	*dest[32].(*bool) = f.WorkloadProductionUse
	*dest[33].(*int64) = f.GrantRevision
	*dest[34].(*string) = f.GrantSubjectType
	*dest[35].(*int64) = f.GrantSubjectUserID
	*dest[36].(*int64) = f.GrantSubjectWorkloadID
	*dest[37].(*int64) = sponsorUserID
	*dest[38].(*int64) = sponsorOrgID
	*dest[39].(*string) = sponsorStatus
	*dest[40].(*string) = sponsorOrganizationRole
	*dest[41].(*string) = f.SponsorMembershipRole
	*dest[42].(*string) = f.GrantProjectRole
	*dest[43].(*string) = f.GrantEnvironment
	*dest[44].(*bool) = f.GrantProductionUse
	*dest[45].(*bool) = f.GrantCurrentlyActive
	*dest[46].(*string) = f.GrantScopeMode
	*dest[47].(*[]byte) = append([]byte(nil), f.GrantModels...)
	*dest[48].(*[]byte) = append([]byte(nil), f.GrantEndpoints...)
	setNullInt64(dest[49].(*sql.NullInt64), f.GrantRPMLimit)
	setNullInt64(dest[50].(*sql.NullInt64), f.GrantRPSLimit)
	setNullInt64(dest[51].(*sql.NullInt64), f.GrantConcurrencyLimit)
	*dest[52].(*string) = credentialStatus
	*dest[53].(*string) = f.Name
	*dest[54].(*string) = f.AllowedIPs
	*dest[55].(*string) = f.AllowedModels
	*dest[56].(*string) = f.AllowedEndpoints
	*dest[57].(*string) = f.PolicyLabels
	*dest[58].(*int) = f.RPMLimit
	*dest[59].(*int) = f.RPSLimit
	setNullInt64(dest[60].(*sql.NullInt64), f.AccessCredentialConcurrencyLimit)
	setNullInt64(dest[61].(*sql.NullInt64), credentialBudget)
	return nil
}

func setNullInt64(target *sql.NullInt64, value *int64) {
	*target = sql.NullInt64{}
	if value != nil {
		target.Int64 = *value
		target.Valid = true
	}
}
