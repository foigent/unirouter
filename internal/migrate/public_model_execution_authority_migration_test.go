package migrate

import (
	"os"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/modeloperation"
)

func TestPublicModelExecutionAuthorityMigrationFreezesPerReleaseItemContracts(t *testing.T) {
	upBytes, err := os.ReadFile("../../migrations/000077_public_model_execution_authority.up.sql")
	if err != nil {
		t.Fatalf("read migration 77 up: %v", err)
	}
	up := string(upBytes)
	beginOffset := strings.Index(up, "BEGIN;")
	advisoryOffset := strings.Index(up, modelExecutionMigrationAdvisoryLock)
	lockOffset := strings.Index(up, "LOCK TABLE ")
	gatewayProfileOffset := strings.Index(up, "ALTER TABLE gateway_endpoints")
	contractOffset := strings.Index(up, "CREATE TABLE model_operation_public_execution_contract_extensions")
	if beginOffset < 0 || advisoryOffset <= beginOffset || lockOffset <= advisoryOffset ||
		gatewayProfileOffset <= lockOffset || contractOffset <= gatewayProfileOffset {
		t.Fatalf("migration 77 lock/profile/create order invalid: begin=%d advisory=%d lock=%d profile=%d contract=%d",
			beginOffset, advisoryOffset, lockOffset, gatewayProfileOffset, contractOffset)
	}
	lockEnd := strings.Index(up[lockOffset:], "IN SHARE ROW EXCLUSIVE MODE;")
	if lockEnd < 0 || !containsSQLIdentifier(up[lockOffset:lockOffset+lockEnd], "gateway_endpoints") {
		t.Fatal("migration 77 must stabilize Gateway Endpoint writers before expanding the profile check")
	}
	for _, required := range []string{
		"DROP CONSTRAINT gateway_endpoints_profile_check",
		"'endpoint.openai-chat.v1'",
		"'endpoint.openai-responses.contract.v1'",
		"'endpoint.anthropic-messages.v1'",
		"DROP CONSTRAINT access_requests_scope_check",
		"DROP CONSTRAINT access_grants_identity_check",
		"DROP CONSTRAINT access_grant_revisions_scope_check",
		"OLD.state IN ('rejected','outcome_unknown','response_unusable')",
		"model_operation_public_execution_contract_extensions_base_fk",
		"REFERENCES model_operation_execution_support_bindings",
		"model_operation_current_public_execution_contracts",
		"unirouter.public-nonstream-delivery.v1",
		"unirouter.provider-final-token-charge.v1",
		"unirouter.public-at-most-once.v1",
		"CREATE FUNCTION require_runtime_release_initially_unsealed()",
		"runtime route manifest must be inserted unsealed",
		"CREATE TABLE model_operation_public_execution_bindings",
		"UNIQUE (release_id,ordinal)",
		"public_execution_binding_id,release_id,ordinal,deployment_id,operation_id",
		"runtime.public_execution_binding.v1",
		"public execution binding proof is stale, incomplete, or mismatched",
		"DEFERRABLE INITIALLY DEFERRED",
		"public execution binding requires its exact release item",
		"ADD COLUMN public_execution_binding_id BIGINT",
		"ADD COLUMN delivery_contract_version VARCHAR(100)",
		"ADD COLUMN route_public_execution_binding_id BIGINT",
		"billing_provider_attempts_public_binding_fk",
		"unirouter.execution-route.v2",
		"ALTER TABLE call_logs",
		"ALTER TABLE request_trace_events",
		"NEW.route_public_execution_binding_id",
		"attempt.route_public_execution_binding_id",
		"model_operation_public_execution_qualifications",
		"public_executable",
		"Responses/Messages runtime manifest lacks current M77 public execution qualification",
	} {
		if !strings.Contains(up, required) {
			t.Fatalf("migration 77 up is missing %q", required)
		}
	}
	if strings.Contains(up, "credential_binding_id BIGINT NOT NULL") ||
		strings.Contains(up, "credential_link_created_at TIMESTAMPTZ") ||
		strings.Contains(up, "probe_id BIGINT NOT NULL") {
		t.Fatal("M77 structural binding must not freeze M75 approval credential/probe/link authority")
	}
	if strings.Contains(up, "UPDATE runtime_route_revision_items") {
		t.Fatal("M77 must never backfill or upgrade an M75 release item")
	}
	if strings.Count(up, modelExecutionMigrationAdvisoryLock) != 1 {
		t.Fatal("migration 77 up must acquire the shared lifecycle advisory lock exactly once")
	}
	exactScopeJSON := `["/v1/chat/completions","/v1/embeddings","/v1/responses","/v1/messages","/v1/models","/v1/media/jobs"]`
	if count := strings.Count(up, exactScopeJSON); count != 3 {
		t.Fatalf("M77 must extend exactly the request, grant, and grant-revision endpoint checks; count=%d", count)
	}
	for _, definition := range modeloperation.All() {
		if !strings.Contains(exactScopeJSON, `"`+definition.Path+`"`) {
			t.Fatalf("M77 SQL public scope vocabulary is missing catalog path %q", definition.Path)
		}
	}
	if count := strings.Count(up,
		"route_evidence_version='unirouter.execution-route.v1'\n                AND route_model_operation_id NOT IN ('openai.responses.v1','anthropic.messages.v1')",
	); count != 3 {
		t.Fatalf("M77 must reject V1 public operations in attempt, call-log, and trace checks; count=%d", count)
	}
}

func TestPublicModelExecutionAuthorityDownGuardsFrozenAuthorityBeforeDrop(t *testing.T) {
	downBytes, err := os.ReadFile("../../migrations/000077_public_model_execution_authority.down.sql")
	if err != nil {
		t.Fatalf("read migration 77 down: %v", err)
	}
	down := string(downBytes)
	beginOffset := strings.Index(down, "BEGIN;")
	advisoryOffset := strings.Index(down, modelExecutionMigrationAdvisoryLock)
	lockOffset := strings.Index(down, "LOCK TABLE ")
	guardOffset := strings.Index(down, "DO $$")
	gatewayProfileOffset := strings.Index(down, "ALTER TABLE gateway_endpoints")
	dropOffset := strings.Index(down, "DROP TABLE model_operation_public_execution_bindings")
	if beginOffset < 0 || advisoryOffset <= beginOffset || lockOffset <= advisoryOffset ||
		guardOffset <= lockOffset || gatewayProfileOffset <= guardOffset || dropOffset <= gatewayProfileOffset {
		t.Fatalf("migration 77 down order invalid: begin=%d advisory=%d lock=%d guard=%d profile=%d drop=%d",
			beginOffset, advisoryOffset, lockOffset, guardOffset, gatewayProfileOffset, dropOffset)
	}
	lockEnd := strings.Index(down[lockOffset:], "IN SHARE ROW EXCLUSIVE MODE;")
	if lockEnd < 0 || !containsSQLIdentifier(down[lockOffset:lockOffset+lockEnd], "gateway_endpoints") {
		t.Fatal("migration 77 down must stabilize Gateway Endpoint writers before checking profile evidence")
	}
	for _, required := range []string{
		"version>'000077_public_model_execution_authority'",
		"immutable public execution bindings exist",
		"frozen M77 route membership exists",
		"immutable V2 execution evidence exists",
		"durable public endpoint scope evidence exists",
		"Anthropic Messages Gateway Endpoint profile evidence exists",
		"WHERE endpoint_profile='endpoint.anthropic-messages.v1'",
		"DROP CONSTRAINT gateway_endpoints_profile_check",
		"requested_endpoints ?| ARRAY['/v1/responses','/v1/messages']",
		"allowed_endpoints ?| ARRAY['/v1/responses','/v1/messages']",
		"OLD.state IN ('rejected','outcome_unknown')",
		"DROP CONSTRAINT billing_provider_attempts_public_binding_fk",
		"DROP COLUMN route_public_execution_binding_id",
		"DROP CONSTRAINT runtime_route_revision_items_public_binding_fk",
		"DROP FUNCTION IF EXISTS require_runtime_release_initially_unsealed()",
		"DROP VIEW model_operation_public_execution_qualifications",
		"DELETE FROM schema_migrations",
	} {
		if !strings.Contains(down, required) {
			t.Fatalf("migration 77 down is missing %q", required)
		}
	}
	legacyScopeJSON := `["/v1/chat/completions","/v1/embeddings","/v1/models","/v1/media/jobs"]`
	if count := strings.Count(down, legacyScopeJSON); count != 3 {
		t.Fatalf("M77 down must restore exactly the three M59 endpoint checks; count=%d", count)
	}
	if strings.Contains(down, "OLD.state IN ('rejected','outcome_unknown','response_unusable')") {
		t.Fatal("M77 down must restore the exact M73 provider health fence")
	}
	legacyGatewayProfileCheck := `endpoint_profile IN (
            'endpoint.openai-chat.v1',
            'endpoint.openai-responses.contract.v1'
        )`
	if strings.Count(down, legacyGatewayProfileCheck) != 1 {
		t.Fatal("M77 down must restore the exact M55 Gateway Endpoint profile check")
	}
}

func TestCISelectsPublicModelExecutionAuthorityPostgresContracts(t *testing.T) {
	lane := consolidatedIntegrationSource(t)
	if !strings.Contains(lane, "TestPublicModelExecutionAuthority.*Integration") {
		t.Fatal("CI consolidated integration gate does not select M77 PostgreSQL contracts")
	}
	if !strings.Contains(lane, "TestPublicCoordinator.*Integration") ||
		!strings.Contains(lane, "./internal/modelexecution") {
		t.Fatal("CI consolidated integration gate does not run M77 public Coordinator PostgreSQL contracts")
	}
	if !strings.Contains(lane, "TestOwnedProviderAttemptAndTerminalHealthFenceAreReplaySafe") {
		t.Fatal("CI consolidated integration gate does not select the owned provider-attempt rollback fence")
	}
}
