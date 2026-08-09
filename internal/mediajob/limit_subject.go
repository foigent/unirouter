package mediajob

import (
	"sort"
	"strings"

	"github.com/uniroute/unirouter/internal/limitpolicy"
	"github.com/uniroute/unirouter/internal/models"
)

func mediaLimitSubject(apiKey models.ApiKey, orgID, projectID int64, model string) limitpolicy.Subject {
	return limitpolicy.Subject{
		OrgID: orgID, ProjectID: projectID, APIKeyID: apiKey.ID,
		AccessCredentialID:         nullableMediaID(apiKey.AccessCredentialID),
		CredentialRef:              apiKey.CredentialRef,
		AccessGrantID:              nullableMediaID(apiKey.AccessGrantID),
		Model:                      strings.TrimSpace(model),
		PolicyLabels:               mediaPolicyLabels(apiKey.PolicyLabels),
		BaseRPSLimit:               int64(apiKey.QPSLimit),
		BaseRPMLimit:               int64(apiKey.RPMLimit),
		CredentialConcurrencyLimit: mediaInt64Pointer(apiKey.CredentialConcurrencyLimit),
		GrantRPSLimit:              mediaInt64Pointer(apiKey.GrantRPSLimit),
		GrantRPMLimit:              mediaInt64Pointer(apiKey.GrantRPMLimit),
		GrantConcurrencyLimit:      mediaInt64Pointer(apiKey.GrantConcurrencyLimit),
	}
}

func mediaPolicyLabels(raw string) []string {
	values := make([]string, 0)
	seen := make(map[string]struct{})
	for _, value := range strings.Split(raw, ",") {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}
