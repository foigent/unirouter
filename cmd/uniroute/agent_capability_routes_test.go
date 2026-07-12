package main

import (
	"os"
	"strings"
	"testing"
)

func TestMainWiresExtractedAgentCapabilityRoutes(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	mainSource := string(source)
	for _, fragment := range []string{
		"registerAgentCapabilityRoutes(mux, agentCapabilityHandler",
		"authenticate: gatewayAuth",
		"rateLimit:    rateLimiter",
	} {
		if !strings.Contains(mainSource, fragment) {
			t.Fatalf("main.go Agent capability registrar wiring missing %q", fragment)
		}
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
