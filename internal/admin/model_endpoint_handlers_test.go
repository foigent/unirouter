package admin

import (
	"testing"
	"time"
)

func TestCanonicalModelEndpointCompatibilityProjectionUsesDeploymentOperationIdentity(t *testing.T) {
	now := time.Now().UTC()
	projection := canonicalModelEndpointProjection(canonicalModelApplicationProjection{
		GeneratedAt: now,
		Applications: []canonicalModelApplication{{
			PublicModel: canonicalPublicModel{ModelName: "company-chat", DisplayName: "Company Chat", Lifecycle: "active", ApprovedCatalog: true},
			Deployment:  canonicalDeployment{ID: 31, UpstreamModel: "upstream-chat", AdminState: "enabled", Priority: 1, RoutingWeight: 100, CircuitState: "healthy"},
			Connection:  canonicalConnectionAxis{ID: 41, Name: "Primary", Provider: "openai", BaseURL: "https://provider.example", AdapterType: "openai_compatible", AdminState: "enabled"},
			Credential:  canonicalCredentialAxis{EligibleCount: 1},
			Operations: []canonicalOperationAxis{{
				OperationID: "openai.chat_completions.v1", SupportState: "enabled", EvidenceState: "live_probed", VerifiedAt: &now,
				ProbeState: "ready", Runtime: canonicalOperationRuntimeAxis{State: "stable", StableMembership: &canonicalRuntimeMembership{ReleaseRef: "release-1"}},
			}},
		}},
	})
	if projection.ContractVersion != modelEndpointProjectionVersion || len(projection.Models) != 1 || len(projection.Models[0].Targets) != 1 {
		t.Fatalf("unexpected projection: %+v", projection)
	}
	target := projection.Models[0].Targets[0]
	if target.ChannelID != 0 || target.RoutingEpoch != 0 || target.DeploymentID != 31 || target.ConnectionID != 41 ||
		target.ModelOperationID != "openai.chat_completions.v1" || !target.RouteCandidate {
		t.Fatalf("legacy identity leaked or canonical identity missing: %+v", target)
	}
}
