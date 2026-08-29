package migrate

import (
	"os"
	"strings"
	"testing"
)

const modelExecutionMigrationAdvisoryLock = "SELECT pg_advisory_xact_lock(86420517831702491);"

func TestModelExecutionMigrationsJoinSharedLifecycleLockBeforeTableLocks(t *testing.T) {
	tests := []struct {
		name           string
		file           string
		firstProtected string
		lockedTables   []string
	}{
		{
			name:           "69 up",
			file:           "000069_model_deployment_expand.up.sql",
			firstProtected: "CREATE TABLE provider_connections",
			lockedTables: []string{
				"channels", "channel_keys", "model_registry", "runtime_change_releases",
				"call_logs", "billing_provider_attempts", "schema_migrations",
			},
		},
		{
			name:           "69 down",
			file:           "000069_model_deployment_expand.down.sql",
			firstProtected: "DO $$",
			lockedTables: []string{
				"provider_connections", "channels", "provider_credential_bindings",
				"channel_keys", "model_deployments", "model_registry",
				"model_deployment_operations", "model_deployment_capabilities",
				"model_deployment_probes", "channel_model_probes",
				"credential_binding_deployments", "runtime_route_revision_items",
				"call_logs", "billing_provider_attempts", "schema_migrations",
			},
		},
		{
			name:           "70 up",
			file:           "000070_runtime_deployment_route_manifest.up.sql",
			firstProtected: "ALTER TABLE runtime_change_releases",
			lockedTables: []string{
				"runtime_change_releases", "runtime_change_slots",
				"model_deployment_operations", "model_deployment_capabilities",
				"provider_connections", "channels", "model_deployments",
				"provider_credential_bindings", "channel_keys",
				"credential_binding_deployments", "runtime_route_revision_items",
				"model_deployment_probes", "model_registry", "schema_migrations",
			},
		},
		{
			name:           "70 down",
			file:           "000070_runtime_deployment_route_manifest.down.sql",
			firstProtected: "DO $$",
			lockedTables: []string{
				"runtime_change_releases", "runtime_change_slots",
				"runtime_route_revision_items", "model_deployment_operations",
				"model_deployment_capabilities", "model_deployments",
				"provider_connections", "provider_credential_bindings",
				"credential_binding_deployments", "model_deployment_probes",
				"schema_migrations",
			},
		},
		{
			name:           "71 up",
			file:           "000071_model_execution_evidence.up.sql",
			firstProtected: "ALTER TABLE billing_provider_attempts",
			lockedTables:   []string{"billing_provider_attempts", "schema_migrations"},
		},
		{
			name:           "71 down",
			file:           "000071_model_execution_evidence.down.sql",
			firstProtected: "DO $$",
			lockedTables:   []string{"billing_provider_attempts", "schema_migrations"},
		},
		{
			name:           "72 up",
			file:           "000072_execution_evidence_projection.up.sql",
			firstProtected: "ALTER TABLE call_logs",
			lockedTables:   []string{"call_logs", "request_trace_events", "schema_migrations"},
		},
		{
			name:           "72 down",
			file:           "000072_execution_evidence_projection.down.sql",
			firstProtected: "DO $$",
			lockedTables:   []string{"call_logs", "request_trace_events", "schema_migrations"},
		},
		{
			name:           "79 up",
			file:           "000079_canonical_connection_license_authority.up.sql",
			firstProtected: "hashtextextended('unirouter.commercial_limit.active_channels'",
			lockedTables: []string{
				"provider_connections", "channels", "schema_migrations",
			},
		},
		{
			name:           "79 down",
			file:           "000079_canonical_connection_license_authority.down.sql",
			firstProtected: "hashtextextended('unirouter.commercial_limit.active_channels'",
			lockedTables: []string{
				"provider_connections", "channels", "schema_migrations",
			},
		},
		{
			name:           "80 up",
			file:           "000080_observation_provider_cost_reconciliation.up.sql",
			firstProtected: "ALTER TABLE billing_observation_receipts",
			lockedTables: []string{
				"freeze_idempotency", "billing_provider_attempts",
				"provider_cost_reconciliations", "billing_observation_receipts",
				"schema_migrations",
			},
		},
		{
			name:           "80 down",
			file:           "000080_observation_provider_cost_reconciliation.down.sql",
			firstProtected: "DO $$",
			lockedTables: []string{
				"freeze_idempotency", "billing_provider_attempts",
				"provider_cost_reconciliations", "billing_observation_receipts",
				"schema_migrations",
			},
		},
		{
			name:           "81 up",
			file:           "000081_versioned_model_execution_profiles.up.sql",
			firstProtected: "ALTER TABLE model_registry",
			lockedTables:   []string{"model_registry", "schema_migrations"},
		},
		{
			name:           "81 down",
			file:           "000081_versioned_model_execution_profiles.down.sql",
			firstProtected: "DO $$",
			lockedTables:   []string{"model_registry", "schema_migrations"},
		},
		{
			name:           "82 up",
			file:           "000082_sealed_model_execution_contracts.up.sql",
			firstProtected: "ALTER TABLE runtime_change_releases",
			lockedTables:   []string{"runtime_change_releases", "model_registry", "schema_migrations"},
		},
		{
			name:           "82 down",
			file:           "000082_sealed_model_execution_contracts.down.sql",
			firstProtected: "DO $$",
			lockedTables:   []string{"runtime_change_releases", "model_registry", "schema_migrations"},
		},
		{
			name:           "88 up",
			file:           "000088_model_capability_execution_qualification.up.sql",
			firstProtected: "ALTER TABLE model_deployment_capabilities",
			lockedTables: []string{
				"model_deployment_capabilities", "model_deployment_operations",
				"model_deployments", "provider_connections", "provider_credential_bindings",
				"credential_binding_deployments", "model_registry", "users", "orgs",
				"billing_provider_attempts", "schema_migrations",
			},
		},
		{
			name:           "88 down",
			file:           "000088_model_capability_execution_qualification.down.sql",
			firstProtected: "DO $$",
			lockedTables: []string{
				"model_capability_routing_approvals", "model_capability_probes",
				"model_deployment_capabilities", "billing_provider_attempts", "schema_migrations",
			},
		},
		{
			name:           "89 up",
			file:           "000089_deepseek_chat_sse_usage_contract_v2.up.sql",
			firstProtected: "DROP TRIGGER model_capability_probe_contract_bindings_immutable",
			lockedTables: []string{
				"model_capability_probe_contract_bindings", "model_capability_current_probe_contracts",
				"model_deployment_capabilities", "schema_migrations",
			},
		},
		{
			name:           "89 down",
			file:           "000089_deepseek_chat_sse_usage_contract_v2.down.sql",
			firstProtected: "DO $$",
			lockedTables: []string{
				"model_capability_probe_contract_bindings", "model_capability_current_probe_contracts",
				"model_deployment_capabilities", "model_capability_probes", "schema_migrations",
			},
		},
		{
			name:           "90 up",
			file:           "000090_deepseek_chat_reasoning_capabilities.up.sql",
			firstProtected: "DROP TRIGGER model_capability_probe_contract_bindings_immutable",
			lockedTables: []string{
				"model_capability_probe_contract_bindings", "model_capability_current_probe_contracts",
				"model_deployment_capabilities", "model_deployment_operations", "model_deployments",
				"provider_connections", "model_registry", "schema_migrations",
			},
		},
		{
			name:           "90 down",
			file:           "000090_deepseek_chat_reasoning_capabilities.down.sql",
			firstProtected: "DO $$",
			lockedTables: []string{
				"model_capability_probe_contract_bindings", "model_capability_current_probe_contracts",
				"model_deployment_capabilities", "model_deployment_operations", "model_deployments",
				"provider_connections", "model_registry", "model_capability_probes",
				"model_capability_routing_approvals", "schema_migrations",
			},
		},
		{
			name:           "91 up",
			file:           "000091_deepseek_chat_tools_json_capabilities.up.sql",
			firstProtected: "DROP TRIGGER model_capability_probe_contract_bindings_immutable",
			lockedTables: []string{
				"model_capability_probe_contract_bindings", "model_capability_current_probe_contracts",
				"model_deployment_capabilities", "model_deployment_operations", "model_deployments",
				"provider_connections", "model_registry", "schema_migrations",
			},
		},
		{
			name:           "91 down",
			file:           "000091_deepseek_chat_tools_json_capabilities.down.sql",
			firstProtected: "DO $$",
			lockedTables: []string{
				"model_capability_probe_contract_bindings", "model_capability_current_probe_contracts",
				"model_deployment_capabilities", "model_deployment_operations", "model_deployments",
				"provider_connections", "model_registry", "model_capability_probes",
				"model_capability_routing_approvals", "schema_migrations",
			},
		},
		{
			name:           "98 up",
			file:           "000098_kimi_k26_nonstream_usage_probe_v2.up.sql",
			firstProtected: "DROP TRIGGER model_capability_probe_contract_bindings_immutable",
			lockedTables: []string{
				"model_capability_probe_contract_bindings", "model_capability_current_probe_contracts",
				"model_deployment_capabilities", "model_deployment_operations", "model_deployments",
				"provider_connections", "model_registry", "schema_migrations",
			},
		},
		{
			name:           "98 down",
			file:           "000098_kimi_k26_nonstream_usage_probe_v2.down.sql",
			firstProtected: "DO $$",
			lockedTables: []string{
				"model_capability_probe_contract_bindings", "model_capability_current_probe_contracts",
				"model_deployment_capabilities", "model_deployment_operations", "model_deployments",
				"provider_connections", "model_registry", "model_capability_probes",
				"model_capability_routing_approvals", "schema_migrations",
			},
		},
		{
			name:           "96 up",
			file:           "000096_kimi_k26_stream_probe_contract_v2.up.sql",
			firstProtected: "DROP TRIGGER model_capability_probe_contract_bindings_immutable",
			lockedTables: []string{
				"model_capability_probe_contract_bindings", "model_capability_current_probe_contracts",
				"model_deployment_capabilities", "model_deployment_operations", "model_deployments",
				"provider_connections", "model_registry", "schema_migrations",
			},
		},
		{
			name:           "96 down",
			file:           "000096_kimi_k26_stream_probe_contract_v2.down.sql",
			firstProtected: "DO $$",
			lockedTables: []string{
				"model_capability_probe_contract_bindings", "model_capability_current_probe_contracts",
				"model_deployment_capabilities", "model_deployment_operations", "model_deployments",
				"provider_connections", "model_registry", "model_capability_probes",
				"model_capability_routing_approvals", "schema_migrations",
			},
		},
		{
			name:           "97 up",
			file:           "000097_kimi_k26_stream_choice_usage_probe_v3.up.sql",
			firstProtected: "DROP TRIGGER model_capability_probe_contract_bindings_immutable",
			lockedTables: []string{
				"model_capability_probe_contract_bindings", "model_capability_current_probe_contracts",
				"model_deployment_capabilities", "model_deployment_operations", "model_deployments",
				"provider_connections", "model_registry", "schema_migrations",
			},
		},
		{
			name:           "97 down",
			file:           "000097_kimi_k26_stream_choice_usage_probe_v3.down.sql",
			firstProtected: "DO $$",
			lockedTables: []string{
				"model_capability_probe_contract_bindings", "model_capability_current_probe_contracts",
				"model_deployment_capabilities", "model_deployment_operations", "model_deployments",
				"provider_connections", "model_registry", "model_capability_probes",
				"model_capability_routing_approvals", "schema_migrations",
			},
		},
		{
			name:           "95 up",
			file:           "000095_kimi_k26_chat_core_qualification.up.sql",
			firstProtected: "DROP TRIGGER model_capability_probe_contract_bindings_immutable",
			lockedTables: []string{
				"model_capability_probe_contract_bindings", "model_capability_current_probe_contracts",
				"model_deployment_capabilities", "model_deployment_operations", "model_deployments",
				"provider_connections", "model_registry", "schema_migrations",
			},
		},
		{
			name:           "95 down",
			file:           "000095_kimi_k26_chat_core_qualification.down.sql",
			firstProtected: "DO $$",
			lockedTables: []string{
				"model_capability_probe_contract_bindings", "model_capability_current_probe_contracts",
				"model_deployment_capabilities", "model_deployment_operations", "model_deployments",
				"provider_connections", "model_registry", "model_capability_probes",
				"model_capability_routing_approvals", "schema_migrations",
			},
		},
		{
			name:           "94 up",
			file:           "000094_deepseek_v4_pro_chat_core_qualification.up.sql",
			firstProtected: "DROP TRIGGER model_capability_probe_contract_bindings_immutable",
			lockedTables: []string{
				"model_capability_probe_contract_bindings", "model_capability_current_probe_contracts",
				"model_deployment_capabilities", "model_deployment_operations", "model_deployments",
				"provider_connections", "model_registry", "schema_migrations",
			},
		},
		{
			name:           "94 down",
			file:           "000094_deepseek_v4_pro_chat_core_qualification.down.sql",
			firstProtected: "DO $$",
			lockedTables: []string{
				"model_capability_probe_contract_bindings", "model_capability_current_probe_contracts",
				"model_deployment_capabilities", "model_deployment_operations", "model_deployments",
				"provider_connections", "model_registry", "model_capability_probes",
				"model_capability_routing_approvals", "schema_migrations",
			},
		},
		{
			name:           "93 up",
			file:           "000093_deepseek_function_tool_probe_contract_v2.up.sql",
			firstProtected: "DROP TRIGGER model_capability_probe_contract_bindings_immutable",
			lockedTables: []string{
				"model_capability_probe_contract_bindings", "model_capability_current_probe_contracts",
				"model_deployment_capabilities", "model_deployment_operations", "model_deployments",
				"provider_connections", "model_registry", "schema_migrations",
			},
		},
		{
			name:           "93 down",
			file:           "000093_deepseek_function_tool_probe_contract_v2.down.sql",
			firstProtected: "DO $$",
			lockedTables: []string{
				"model_capability_probe_contract_bindings", "model_capability_current_probe_contracts",
				"model_deployment_capabilities", "model_deployment_operations", "model_deployments",
				"provider_connections", "model_registry", "model_capability_probes",
				"model_capability_routing_approvals", "schema_migrations",
			},
		},
		{
			name:           "92 up",
			file:           "000092_deepseek_reasoning_tool_choice_contract.up.sql",
			firstProtected: "DROP TRIGGER model_capability_probe_contract_bindings_immutable",
			lockedTables: []string{
				"model_capability_probe_contract_bindings", "model_capability_current_probe_contracts",
				"model_deployment_capabilities", "model_deployment_operations", "model_deployments",
				"provider_connections", "model_registry", "schema_migrations",
			},
		},
		{
			name:           "92 down",
			file:           "000092_deepseek_reasoning_tool_choice_contract.down.sql",
			firstProtected: "DO $$",
			lockedTables: []string{
				"model_capability_probe_contract_bindings", "model_capability_current_probe_contracts",
				"model_deployment_capabilities", "model_deployment_operations", "model_deployments",
				"provider_connections", "model_registry", "model_capability_probes",
				"model_capability_routing_approvals", "schema_migrations",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body, err := os.ReadFile("../../migrations/" + test.file)
			if err != nil {
				t.Fatalf("read %s: %v", test.file, err)
			}
			wire := string(body)
			beginOffset := strings.Index(wire, "BEGIN;")
			advisoryOffset := strings.Index(wire, modelExecutionMigrationAdvisoryLock)
			lockOffset := strings.Index(wire, "LOCK TABLE ")
			protectedOffset := strings.Index(wire, test.firstProtected)
			if beginOffset < 0 || advisoryOffset <= beginOffset || lockOffset <= advisoryOffset || protectedOffset <= lockOffset {
				t.Fatalf("%s lock order invalid: begin=%d advisory=%d table-lock=%d protected=%d",
					test.file, beginOffset, advisoryOffset, lockOffset, protectedOffset)
			}
			if strings.Count(wire, modelExecutionMigrationAdvisoryLock) != 1 {
				t.Fatalf("%s must join the shared migration lifecycle lock exactly once", test.file)
			}
			if between := strings.TrimSpace(wire[beginOffset+len("BEGIN;") : advisoryOffset]); between != "" {
				t.Fatalf("%s advisory lock must be the first statement after BEGIN: %q", test.file, between)
			}

			lockEndRelative := strings.Index(wire[lockOffset:], "IN SHARE ROW EXCLUSIVE MODE;")
			if lockEndRelative < 0 {
				t.Fatalf("%s is missing the reviewed writer-stabilization lock mode", test.file)
			}
			lockClause := wire[lockOffset : lockOffset+lockEndRelative]
			for _, table := range test.lockedTables {
				if !containsSQLIdentifier(lockClause, table) {
					t.Fatalf("%s stabilization lock is missing table %s", test.file, table)
				}
			}
		})
	}
}

func TestModelExecutionGuardedDownsLockBeforeLedgerAndEvidenceChecks(t *testing.T) {
	for _, file := range []string{
		"000069_model_deployment_expand.down.sql",
		"000070_runtime_deployment_route_manifest.down.sql",
		"000071_model_execution_evidence.down.sql",
		"000072_execution_evidence_projection.down.sql",
		"000079_canonical_connection_license_authority.down.sql",
		"000080_observation_provider_cost_reconciliation.down.sql",
		"000081_versioned_model_execution_profiles.down.sql",
		"000082_sealed_model_execution_contracts.down.sql",
		"000088_model_capability_execution_qualification.down.sql",
		"000089_deepseek_chat_sse_usage_contract_v2.down.sql",
		"000090_deepseek_chat_reasoning_capabilities.down.sql",
		"000091_deepseek_chat_tools_json_capabilities.down.sql",
		"000092_deepseek_reasoning_tool_choice_contract.down.sql",
		"000093_deepseek_function_tool_probe_contract_v2.down.sql",
		"000094_deepseek_v4_pro_chat_core_qualification.down.sql",
		"000095_kimi_k26_chat_core_qualification.down.sql",
		"000096_kimi_k26_stream_probe_contract_v2.down.sql",
		"000097_kimi_k26_stream_choice_usage_probe_v3.down.sql",
		"000098_kimi_k26_nonstream_usage_probe_v2.down.sql",
	} {
		t.Run(file, func(t *testing.T) {
			body, err := os.ReadFile("../../migrations/" + file)
			if err != nil {
				t.Fatalf("read %s: %v", file, err)
			}
			wire := string(body)
			lockOffset := strings.Index(wire, "LOCK TABLE ")
			ledgerOffset := strings.Index(wire, "FROM schema_migrations")
			guardOffset := strings.Index(wire, "DO $$")
			if lockOffset < 0 || ledgerOffset <= lockOffset || guardOffset <= lockOffset {
				t.Fatalf("%s must stabilize writers before its later-migration and evidence guards", file)
			}
		})
	}
}

func containsSQLIdentifier(clause, identifier string) bool {
	for _, field := range strings.FieldsFunc(clause, func(r rune) bool {
		return !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9')
	}) {
		if field == identifier {
			return true
		}
	}
	return false
}
