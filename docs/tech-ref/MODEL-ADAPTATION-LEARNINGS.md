# UniRouter Model Adaptation Learnings

This is the sole maintained long-term learning file for model adaptation.
Current code, Baseline/ADR, Target state and provider documentation remain the
authorities; Git retains event history. Entries stay only while their invariant
and executable enforcement remain applicable.

### MAR-001 Validate capability composition under effective defaults

- Invariant: A Profile may expose only request combinations that its exact provider surface can execute; individually valid capabilities do not prove their composition under omitted fields or provider defaults.
- Scope/Trigger: Any model Profile that combines reasoning defaults or toggles with tools, tool choice, structured output, streaming, media, cache or search.
- Enforcement: Typed normalization rejects impossible combinations before Freeze or credential use, and full prepare/admission/provider-payload regressions cover both omitted defaults and explicit overrides.
- Authority: `internal/modelprofile/contracts.go` DeepSeek reasoning v5 contracts; `internal/gateway/deepseek_v4_tool_choice_reasoning_contract_test.go`; `internal/modelprofile/deepseek_v4_flash_tool_history_contract_test.go`; `internal/modelprofile/deepseek_v4_pro_tool_history_contract_test.go`.
- Status: active

### MAR-002 Downgrade must not orphan newly introduced durable identities

- Invariant: A schema downgrade must refuse before mutation whenever the target predecessor cannot interpret a durable Profile attachment or immutable evidence introduced by the newer schema.
- Scope/Trigger: Append-only Profile, protocol, probe-contract or evidence identities added together with a migration compatibility ceiling.
- Enforcement: The down migration holds the shared lifecycle/table locks, permits an empty pre-attachment rollback, and rejects retained model attachments, probes or approvals; isolated PostgreSQL regression verifies rollback atomicity.
- Authority: `migrations/000092_deepseek_reasoning_tool_choice_contract.down.sql`; `internal/admin/model_application_contract_integration_test.go` test `TestCanonicalModelApplicationCurrentCapabilityRollsBackThroughM92Integration`; `internal/migrate/model_execution_migration_lock_contract_test.go`.
- Status: active
