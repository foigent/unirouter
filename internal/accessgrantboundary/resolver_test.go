package accessgrantboundary

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/accessscope"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestResolveMaterializesDefaultGrantRatesAndPreservesExplicitZero(t *testing.T) {
	projectID, ownerID, grantID, credentialID := int64(11), int64(17), int64(23), int64(29)
	base := testutil.AccessGrantBoundaryFixture{
		KeyID: 31, OrgID: 7, ProjectID: &projectID, OwnerUserID: &ownerID,
		KeyType: "personal", Name: "default-rate credential", Prefix: "ur-default-rate",
		KeyStatus: "active", AllowedModels: "*",
		AllowedEndpoints: "/v1/chat/completions,/v1/embeddings,/v1/models",
		RPMLimit:         120, RPSLimit: 20, AccessGrantID: &grantID,
		AccessCredentialID: &credentialID, CredentialRef: "cred_default_rate_fixture",
		OrgStatus: "active", ProjectStatus: "active", OwnerStatus: "active",
		OwnerRole: "developer", OwnerOrganizationRole: "member", MembershipRole: "project_user",
		GrantRevision: 1, GrantSubjectType: "user", GrantSubjectUserID: ownerID,
		GrantProjectRole: "project_user", GrantEnvironment: "personal", GrantCurrentlyActive: true,
		GrantScopeMode: "project_default", GrantModels: []byte(`[]`), GrantEndpoints: []byte(`[]`),
	}

	resolve := func(t *testing.T, fixture testutil.AccessGrantBoundaryFixture) Snapshot {
		t.Helper()
		database := &testutil.MockDB{QueryRowFn: func(_ context.Context, _ string, _ ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: fixture.Scan}
		}}
		snapshot, err := Resolve(context.Background(), database, fixture.KeyID)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		return snapshot
	}

	t.Run("nil means canonical project default", func(t *testing.T) {
		snapshot := resolve(t, base)
		if snapshot.APIKey.GrantRPMLimit == nil || *snapshot.APIKey.GrantRPMLimit != accessscope.ProjectDefaultRPMLimit {
			t.Fatalf("grant RPM=%v, want %d", snapshot.APIKey.GrantRPMLimit, accessscope.ProjectDefaultRPMLimit)
		}
		if snapshot.APIKey.GrantRPSLimit == nil || *snapshot.APIKey.GrantRPSLimit != accessscope.ProjectDefaultRPSLimit {
			t.Fatalf("grant RPS=%v, want %d", snapshot.APIKey.GrantRPSLimit, accessscope.ProjectDefaultRPSLimit)
		}
	})

	t.Run("explicit zero remains a hard zero", func(t *testing.T) {
		zero := int64(0)
		fixture := base
		fixture.GrantRPMLimit = &zero
		fixture.GrantRPSLimit = &zero
		snapshot := resolve(t, fixture)
		if snapshot.APIKey.GrantRPMLimit == nil || *snapshot.APIKey.GrantRPMLimit != 0 ||
			snapshot.APIKey.GrantRPSLimit == nil || *snapshot.APIKey.GrantRPSLimit != 0 {
			t.Fatalf("explicit zero was widened: rpm=%v rps=%v", snapshot.APIKey.GrantRPMLimit, snapshot.APIKey.GrantRPSLimit)
		}
	})
}
