package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLegacyModelMutationRetiredResponseContract(t *testing.T) {
	recorder := httptest.NewRecorder()
	legacyModelMutationRetired(
		recorder,
		httptest.NewRequest(http.MethodPost, "/api/v1/channels", strings.NewReader(`{"ignored":true}`)),
	)

	if recorder.Code != http.StatusGone {
		t.Fatalf("retired mutation status = %d, want %d", recorder.Code, http.StatusGone)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("retired mutation content type = %q", got)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("retired mutation cache control = %q", got)
	}
	var body struct {
		Error struct {
			Type string `json:"type"`
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode retired mutation response: %v", err)
	}
	if body.Error.Type != "gone_error" || body.Error.Code != legacyModelMutationRetiredCode {
		t.Fatalf("retired mutation response = %#v", body.Error)
	}
}

func TestLegacyModelMutationRouteMatrixUsesOnlyPermanentRetirementIngress(t *testing.T) {
	production := productionRouteSource(t)

	mainRegistrations := []string{
		`mux.Handle("POST /api/v1/channels", platformRoute(legacyModelMutationRetired))`,
		`mux.Handle("PUT /api/v1/channels/{id}", platformRoute(legacyModelMutationRetired))`,
		`mux.Handle("DELETE /api/v1/channels/{id}", platformRoute(legacyModelMutationRetired))`,
		`mux.Handle("POST /api/v1/channels/{id}/test", platformRoute(legacyModelMutationRetired))`,
		`mux.Handle("POST /api/v1/channel-keys", platformRoute(legacyModelMutationRetired))`,
		`mux.Handle("PUT /api/v1/channel-keys/{id}", platformRoute(legacyModelMutationRetired))`,
		`mux.Handle("POST /api/v1/channel-keys/{id}/rotate", platformRoute(legacyModelMutationRetired))`,
		`mux.Handle("DELETE /api/v1/channel-keys/{id}", platformRoute(legacyModelMutationRetired))`,
		`mux.Handle("POST /api/v1/model-endpoints/activate", platformRoute(legacyModelMutationRetired))`,
		`mux.Handle("POST /api/v1/model-endpoints/canary", platformRoute(legacyModelMutationRetired))`,
		`mux.Handle("POST /api/v1/model-endpoints/recover", platformRoute(legacyModelMutationRetired))`,
		`registerRuntimeChangeRoutes(mux, adminHandler, platformRoute, legacyModelMutationRetired)`,
	}
	for _, registration := range mainRegistrations {
		if !strings.Contains(production, registration) {
			t.Fatalf("legacy mutation retirement registration missing %q", registration)
		}
	}

	runtimeRegistrations := []string{
		`mux.Handle("POST /api/v1/runtime-changes", platformRoute(retiredMutation))`,
		`mux.Handle("POST /api/v1/runtime-changes/{ref}/validate", platformRoute(retiredMutation))`,
		`mux.Handle("POST /api/v1/runtime-changes/{ref}/activate-initial", platformRoute(retiredMutation))`,
		`mux.Handle("POST /api/v1/runtime-changes/{ref}/shadow", platformRoute(retiredMutation))`,
		`mux.Handle("POST /api/v1/runtime-changes/{ref}/canary", platformRoute(retiredMutation))`,
		`mux.Handle("POST /api/v1/runtime-changes/{ref}/promote", platformRoute(retiredMutation))`,
		`mux.Handle("POST /api/v1/runtime-changes/{ref}/rollback", platformRoute(retiredMutation))`,
		`mux.Handle("POST /api/v1/runtime-changes/{ref}/reject", platformRoute(retiredMutation))`,
	}
	for _, registration := range runtimeRegistrations {
		if !strings.Contains(production, registration) {
			t.Fatalf("legacy runtime-change retirement registration missing %q", registration)
		}
	}

	retainedReads := []string{
		`mux.Handle("GET /api/v1/channels", platformRoute(adminHandler.ListChannels))`,
		`mux.Handle("GET /api/v1/channels/{id}", platformRoute(adminHandler.GetChannel))`,
		`mux.Handle("GET /api/v1/channels/{id}/provider-models", licensedRoute(licensing.FeatureGateway, platformRoute)(adminHandler.ListChannelProviderModels))`,
		`mux.Handle("GET /api/v1/channel-keys", platformRoute(adminHandler.ListChannelKeys))`,
		`mux.Handle("GET /api/v1/model-endpoints", platformRoute(adminHandler.ListModelEndpoints))`,
		`mux.Handle("GET /api/v1/runtime-changes", platformRoute(handler.ListRuntimeChanges))`,
		`mux.Handle("GET /api/v1/runtime-changes/slot", platformRoute(handler.GetRuntimeChangeSlot))`,
		`mux.Handle("GET /api/v1/runtime-changes/{ref}", platformRoute(handler.GetRuntimeChange))`,
	}
	for _, registration := range retainedReads {
		if !strings.Contains(production, registration) {
			t.Fatalf("legacy read/history route was not retained %q", registration)
		}
	}
}

func TestLegacyModelMutationHandlersCannotBeRegisteredByProductionRoutes(t *testing.T) {
	production := productionRouteSource(t)
	for _, forbidden := range []string{
		"adminHandler.CreateChannel",
		"adminHandler.UpdateChannel",
		"adminHandler.DeleteChannel",
		"adminHandler.TestChannel",
		"adminHandler.CreateChannelKey",
		"adminHandler.UpdateChannelKey",
		"adminHandler.RotateChannelKey",
		"adminHandler.DeleteChannelKey",
		"adminHandler.ActivateModelEndpoint",
		"adminHandler.StartModelEndpointCanary",
		"adminHandler.RecoverModelEndpoint",
		"handler.CreateRuntimeChange",
		"handler.ValidateRuntimeChange",
		"handler.ActivateInitialRuntimeChange",
		"handler.AdvanceRuntimeChangeShadow",
		"handler.StartRuntimeChangeCanary",
		"handler.PromoteRuntimeChange",
		"handler.RollbackRuntimeChange",
		"handler.RejectRuntimeChange",
		"guardLegacyModelMutation",
		"legacyModelMutationDisabledCode",
	} {
		if strings.Contains(production, forbidden) {
			t.Fatalf("retired legacy mutation handler remains reachable in production source: %s", forbidden)
		}
	}
}
