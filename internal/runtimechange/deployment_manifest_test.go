package runtimechange

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func deploymentIntPointer(value int) *int { return &value }

func TestNormalizeDeploymentDraftRequiresCompleteExplicitManifest(t *testing.T) {
	request, err := NormalizeDeploymentDraft(DeploymentDraftRequest{
		ReleaseRef:    " RCR_Deployment_V2 ",
		PublicModelID: 41,
		Reason:        " canonical deployment route ",
		DeploymentRoutes: []DeploymentRoute{{
			DeploymentID:               71,
			ModelOperationID:           " openai.chat_completions.v1 ",
			ExpectedConnectionRevision: 3,
			ExpectedDeploymentRevision: 5,
			ExpectedOperationRevision:  7,
			ProtocolProfileID:          "openai.chat_completions.v1",
			ProtocolProfileVersion:     "1.0.0",
			Admission:                  " ALLOW ",
			Priority:                   deploymentIntPointer(20),
			Weight:                     deploymentIntPointer(100),
		}},
	})
	if err != nil {
		t.Fatalf("NormalizeDeploymentDraft() error = %v", err)
	}
	if request.ReleaseRef != "rcr_deployment_v2" || request.Reason != "canonical deployment route" ||
		request.DeploymentRoutes[0].ModelOperationID != "openai.chat_completions.v1" ||
		request.DeploymentRoutes[0].Admission != "allow" {
		t.Fatalf("unexpected normalized request: %#v", request)
	}

	invalid := []DeploymentDraftRequest{
		{PublicModelID: 41, Reason: "empty route"},
		{PublicModelID: 41, Reason: "deny is patch semantics", DeploymentRoutes: []DeploymentRoute{{
			DeploymentID: 71, ModelOperationID: "openai.chat_completions.v1",
			ExpectedConnectionRevision: 3, ExpectedDeploymentRevision: 5, ExpectedOperationRevision: 7,
			ProtocolProfileID: "openai.chat_completions.v1", ProtocolProfileVersion: "1.0.0", Admission: "deny",
		}}},
		{PublicModelID: 41, Reason: "duplicate", DeploymentRoutes: []DeploymentRoute{
			{DeploymentID: 71, ModelOperationID: "openai.chat_completions.v1", ExpectedConnectionRevision: 3, ExpectedDeploymentRevision: 5, ExpectedOperationRevision: 7, ProtocolProfileID: "openai.chat_completions.v1", ProtocolProfileVersion: "1.0.0", Admission: "allow"},
			{DeploymentID: 71, ModelOperationID: "openai.chat_completions.v1", ExpectedConnectionRevision: 3, ExpectedDeploymentRevision: 5, ExpectedOperationRevision: 7, ProtocolProfileID: "openai.chat_completions.v1", ProtocolProfileVersion: "1.0.0", Admission: "allow"},
		}},
		{PublicModelID: 41, Reason: "legacy operation is compatibility-only", DeploymentRoutes: []DeploymentRoute{{
			DeploymentID: 71, ModelOperationID: "legacy.any.v1", ExpectedConnectionRevision: 3,
			ExpectedDeploymentRevision: 5, ExpectedOperationRevision: 7,
			ProtocolProfileID: "legacy.channel_model_probe", ProtocolProfileVersion: "v1", Admission: "allow",
		}}},
		{PublicModelID: 41, Reason: "unknown operation is not canonical", DeploymentRoutes: []DeploymentRoute{{
			DeploymentID: 71, ModelOperationID: "vendor.unknown.v1", ExpectedConnectionRevision: 3,
			ExpectedDeploymentRevision: 5, ExpectedOperationRevision: 7,
			ProtocolProfileID: "vendor.profile.v1", ProtocolProfileVersion: "1.0.0", Admission: "allow",
		}}},
		{PublicModelID: 41, Reason: "legacy probe profile is not an execution contract", DeploymentRoutes: []DeploymentRoute{{
			DeploymentID: 71, ModelOperationID: "openai.chat_completions.v1", ExpectedConnectionRevision: 3,
			ExpectedDeploymentRevision: 5, ExpectedOperationRevision: 7,
			ProtocolProfileID: "legacy.channel_model_probe", ProtocolProfileVersion: "v1", Admission: "allow",
		}}},
	}
	for index, candidate := range invalid {
		if _, err := NormalizeDeploymentDraft(candidate); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid[%d] error = %v", index, err)
		}
	}
}

func TestNormalizeDeploymentDraftDoesNotMutateCaller(t *testing.T) {
	priority, weight := 20, 100
	original := DeploymentDraftRequest{
		ReleaseRef:    " RCR_Deployment_V2 ",
		PublicModelID: 41,
		Reason:        " canonical deployment route ",
		DeploymentRoutes: []DeploymentRoute{{
			DeploymentID:               71,
			ModelOperationID:           " openai.chat_completions.v1 ",
			ExpectedConnectionRevision: 3,
			ExpectedDeploymentRevision: 5,
			ExpectedOperationRevision:  7,
			ProtocolProfileID:          "openai.chat_completions.v1",
			ProtocolProfileVersion:     "1.0.0",
			Admission:                  " ALLOW ",
			Priority:                   &priority,
			Weight:                     &weight,
		}},
	}
	normalized, err := NormalizeDeploymentDraft(original)
	if err != nil {
		t.Fatal(err)
	}
	if original.ReleaseRef != " RCR_Deployment_V2 " ||
		original.Reason != " canonical deployment route " ||
		original.DeploymentRoutes[0].ModelOperationID != " openai.chat_completions.v1 " ||
		original.DeploymentRoutes[0].Admission != " ALLOW " {
		t.Fatalf("normalization mutated caller: %#v", original)
	}
	if normalized.DeploymentRoutes[0].Priority == original.DeploymentRoutes[0].Priority {
		t.Fatal("normalized priority still aliases caller")
	}
	if normalized.DeploymentRoutes[0].Weight == original.DeploymentRoutes[0].Weight {
		t.Fatal("normalized weight still aliases caller")
	}
	*normalized.DeploymentRoutes[0].Priority = 21
	*normalized.DeploymentRoutes[0].Weight = 101
	if priority != 20 || weight != 100 {
		t.Fatalf("normalized pointers mutated caller: priority=%d weight=%d", priority, weight)
	}
}

func TestDeploymentRouteHashIsOrderedAndLossless(t *testing.T) {
	routes := []DeploymentRoute{
		{DeploymentID: 7, ModelOperationID: "openai.chat_completions.v1", ExpectedConnectionRevision: 3, ExpectedDeploymentRevision: 4, ExpectedOperationRevision: 1, ProtocolProfileID: "openai.chat_completions.v1", ProtocolProfileVersion: "1.0.0", Admission: "allow"},
		{DeploymentID: 8, ModelOperationID: "openai.responses.v1", ExpectedConnectionRevision: 5, ExpectedDeploymentRevision: 6, ExpectedOperationRevision: 2, ProtocolProfileID: "openai.responses.v1", ProtocolProfileVersion: "1.0.0", Admission: "allow", Weight: deploymentIntPointer(200)},
	}
	first := hashJSON(routes)
	cloned := append([]DeploymentRoute(nil), routes...)
	if got := hashJSON(cloned); got != first || !reflect.DeepEqual(cloned, routes) {
		t.Fatalf("canonical route hash drifted: %q != %q", got, first)
	}
	cloned[0], cloned[1] = cloned[1], cloned[0]
	if got := hashJSON(cloned); got == first {
		t.Fatal("ordered manifest hash did not change after route order changed")
	}
	cloned = append([]DeploymentRoute(nil), routes...)
	cloned[1].ExpectedDeploymentRevision++
	if got := hashJSON(cloned); got == first {
		t.Fatal("manifest hash did not bind the deployment revision")
	}
}

func TestLegacyTwoTransactionDeploymentAuthoringIsFailClosedBeforeDatabaseAccess(t *testing.T) {
	manager := &Manager{}
	_, err := manager.PrepareDeploymentDraft(t.Context(), DeploymentDraftRequest{}, 0, 0)
	if !errors.Is(err, ErrConflict) ||
		!strings.Contains(err.Error(), "use AuthorSealedDeploymentRelease") {
		t.Fatalf("PrepareDeploymentDraft error = %v", err)
	}
	_, err = manager.SealDeploymentDraft(t.Context(), "", 0, 0, 0)
	if !errors.Is(err, ErrConflict) ||
		!strings.Contains(err.Error(), "use AuthorSealedDeploymentRelease") {
		t.Fatalf("SealDeploymentDraft error = %v", err)
	}
}

func TestLegacyBridgeWritersAreFailClosedBeforeDatabaseAccess(t *testing.T) {
	manager := &Manager{}
	_, err := manager.CutoverDeploymentStable(t.Context(), "", 0, 0, 0, nil)
	if !errors.Is(err, ErrConflict) ||
		!strings.Contains(err.Error(), "ApplyDeploymentLifecycle with cutover_legacy") {
		t.Fatalf("CutoverDeploymentStable error = %v", err)
	}
	_, err = manager.RollbackDeploymentStableToLegacy(t.Context(), "", 0, 0, 0)
	if !errors.Is(err, ErrConflict) ||
		!strings.Contains(err.Error(), "ApplyDeploymentLifecycle with rollback_legacy") {
		t.Fatalf("RollbackDeploymentStableToLegacy error = %v", err)
	}
}
