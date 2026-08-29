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
- Enforcement: The down migration holds the shared lifecycle/table locks and rejects only durable identities the predecessor cannot interpret: Profile migrations guard retained attachments/probes/approvals, while probe-contract-only migrations guard retained evidence or projection-identity drift but permit unchanged pre-existing Profile attachments. A pre-mutation drift check and a pre-deletion no-residual postcondition preserve rollback atomicity in isolated PostgreSQL regressions.
- Authority: `migrations/000092_deepseek_reasoning_tool_choice_contract.down.sql`; `migrations/000093_deepseek_function_tool_probe_contract_v2.down.sql`; `migrations/000094_deepseek_v4_pro_chat_core_qualification.down.sql`; `internal/admin/model_application_contract_integration_test.go` test `TestCanonicalModelApplicationCurrentCapabilityRollsBackThroughM92Integration`; `internal/migrate/model_capability_execution_qualification_migration_integration_test.go`; `internal/migrate/deepseek_v4_pro_qualification_migration_integration_test.go`; `internal/migrate/model_execution_migration_lock_contract_test.go`.
- Status: active

### MAR-003 Share provider wire normalization between probe and runtime

- Invariant: A safe provider wire variation with unchanged canonical semantics is normalized by one exact-scoped decoder used by both runtime and probes; probes add semantic assertions but must not duplicate the provider envelope parser or prove software-artifact correctness.
- Scope/Trigger: Typed provider probes and runtime delivery encounter the same provider-specific response variation, especially function tools, structured output, streaming and media.
- Enforcement: The exact-scoped DeepSeek V4 normalizer accepts only a lexical non-negative child tool-call index, removes it from canonical delivery, and retains unknown siblings. The route-aware delivery guard rejects any residual child index and existing protocol inspection rejects unknown siblings under the reviewed v4 tuple and Flash's retained generic tuple. Probe v2 calls the same normalizer before its sentinel/usage checks; adapter, guardrail, replay and immutable current-contract regressions cover Flash and Pro without creating a new canonical response identity.
- Authority: `internal/modelcapabilityqualification/deepseek_tools_json.go`; `internal/modelcapabilityqualification/deepseek_tools_json_test.go`; `internal/gateway/nonstream_output_guardrail_test.go`; `migrations/000093_deepseek_function_tool_probe_contract_v2.up.sql`.
- Status: active

### MAR-004 Treat a terminal SSE error as an error after HTTP success

- Invariant: An HTTP 200 that later carries a terminal SSE error must not be projected as an empty successful stream; request identity and safe billing finality remain visible without exposing private content.
- Scope/Trigger: Streaming clients and UI workbenches that consume gateway SSE and may receive an error after response headers or partial output.
- Enforcement: The stream consumer records the terminal error event separately from content and renders its bounded code/message, request ID and billing status; a real `ReadableStream` regression forbids the `(empty stream)` success projection.
- Authority: `frontend/src/pages/DeveloperConsolePage.tsx`; `frontend/src/pages/__tests__/DeveloperConsolePage.test.tsx` test `surfaces a terminal SSE error instead of reporting an empty stream`.
- Status: active

### MAR-005 Keep operation protocol identity independent from capability evidence

- Invariant: Capability, catalog and live-evidence state cannot create a deployment operation protocol identity; a typed override must come from one explicit immutable identity declaration, while an invalid or ambiguous declaration fails closed instead of falling back to a generic protocol. Admin capability eligibility must likewise be projected by the service rather than recreated as a frontend Profile/protocol allowlist.
- Scope/Trigger: Canonical onboarding or attachment of a reviewed Profile to Chat, Responses, Messages or another typed operation surface.
- Enforcement: The server first resolves and validates the canonical base operation, then applies only a model-profile-owned exact override. `found=false` returns no fallback identity and preserves the caller-owned Chat, Responses or Embeddings base; resolver errors terminate onboarding. Unverified or legacy Profiles cannot carry typed declarations, and Chat contract registration alone grants no binding authority. The canonical operation projection returns exact `capability_probe_contracts`; the UI renders that projection and has positive Pro/Flash plus unbound-generic regressions instead of owning provider tuples.
- Authority: `internal/modelprofile/operation_protocol_identity.go`; `internal/modelprofile/operation_protocol_identity_test.go`; `internal/admin/model_application_contract.go`; `internal/admin/model_application_contract_test.go`; `internal/admin/model_application_contract_integration_test.go`; `frontend/src/features/modelEndpoints/ModelEndpointDetailsSheet.tsx`; `frontend/src/pages/__tests__/ModelEndpointsPage.test.tsx`.
- Status: active

### MAR-006 Keep customer sponsorship separate from platform operation

- Invariant: A platform operator may administer a workload-access flow but cannot substitute its own identity for the customer project member who supplies business sponsorship; credential scope can only narrow an approved Grant.
- Scope/Trigger: Platform-admin model validation that creates a workload Application, access request or application credential in another customer organization.
- Enforcement: The UI freezes the exact Application scope, loads only active members from its project, requires an explicit Sponsor, ignores late cross-scope responses and sends a server-normalized model allowlist. Offboarding preserves the backend's structured active-sponsorship diagnosis and directs the operator to revoke the old authorization after any independently sponsored replacement; it never implies that a new Grant detaches the old Sponsor. The backend remains authoritative for Sponsor membership, Grant containment, credential scope and atomic offboarding.
- Authority: `frontend/src/pages/ApplicationsV2Page.tsx`; `frontend/src/pages/UsersPage.tsx`; `frontend/src/pages/__tests__/WorkloadApplicationsPage.test.tsx`; `frontend/src/pages/__tests__/UsersPage.test.tsx`; `frontend/src/utils/modelList.ts`; `frontend/src/utils/__tests__/modelList.test.ts`; `internal/accessprovisioning/service.go`; `internal/accessgrantboundary/resolver.go`; `internal/admin/workload_application_handlers.go`; `internal/admin/sponsor_transition.go`.
- Status: active
