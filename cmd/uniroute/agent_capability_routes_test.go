package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/capapi"
)

func TestMainWiresExtractedAgentCapabilityRoutes(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	mainSource := string(source)
	for _, fragment := range []string{
		"registerAgentCapabilityRoutes(mux, agentCapabilityHandler",
		"authorize:    agentLicenseGate",
		"authenticate: gatewayAuth",
		"rateLimit:    rateLimiter",
	} {
		if !strings.Contains(mainSource, fragment) {
			t.Fatalf("main.go Agent capability registrar wiring missing %q", fragment)
		}
	}
}

func TestAgentCapabilityRoutesAuthenticateBeforeLicenseAndRateAdmission(t *testing.T) {
	t.Parallel()
	var order []string
	wrap := func(name string, callNext bool) agentCapabilityRouteGuard {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name)
				if callNext {
					next.ServeHTTP(w, r)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			})
		}
	}
	mux := http.NewServeMux()
	registerAgentCapabilityRoutes(mux, capapi.NewHandler(nil), agentCapabilityRouteGuards{
		authenticate: wrap("authenticate", true),
		authorize:    wrap("license", true),
		rateLimit:    wrap("rate", false),
	})
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/agent-capabilities/exchange", nil))

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
	if want := []string{"authenticate", "license", "rate"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("guard order = %v, want %v", order, want)
	}
}

func TestMainWiresAgentAncestorBudgetAroundGatewayBilling(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	mainSource := string(source)
	for _, fragment := range []string{
		"agentBudgetBilling := capbilling.New(billingSvc, agentCapabilityService)",
		"sharedPolicyBilling := policybilling.New(agentBudgetBilling, limitPolicyService)",
		"gateway.NewRelay(postgres.Pool, redisClient.Client, router, sharedPolicyBilling",
		"billingSvc.RunSettlementRecoveryLoop",
	} {
		if !strings.Contains(mainSource, fragment) {
			t.Fatalf("main.go Agent ancestor budget wiring missing %q", fragment)
		}
	}
}
