#!/usr/bin/env bash
# Same selectors for CI and local runs; dedicated synthetic PostgreSQL/Redis only.
set -euo pipefail
cd "$(dirname "$0")/.."
log_dir=
check_only=false
while (($#)); do
  case "$1" in
    --check) check_only=true; shift ;;
    --log-dir) log_dir=${2:?missing log directory}; shift 2 ;;
    *) echo "usage: $0 [--check] [--log-dir NEW_DIRECTORY]" >&2; exit 2 ;;
  esac
done
: "${TEST_DATABASE_URL:?set the dedicated synthetic test database}"
: "${REDIS_ADDR:?set the dedicated synthetic Redis address}"
: "${REDIS_DB:?set the dedicated Redis database number explicitly}"
[[ ${REQUIRE_TEST_DATABASE:-} == 1 ]] || { echo 'REQUIRE_TEST_DATABASE=1 is required' >&2; exit 2; }
command -v go >/dev/null
# Check the selected toolchain against go.mod before compilation; no downloads.
GOPROXY=off go list -m >/dev/null
[[ $(go env CGO_ENABLED) == 1 ]] || { echo 'race tests require CGO_ENABLED=1' >&2; exit 2; }
read -r -a compiler <<< "$(go env CC)"
command -v "${compiler[0]}" >/dev/null || {
  echo 'Missing C compiler for race tests; on the approved Alpine fixture run apk update, then apk add --simulate gcc musl-dev before installation.' >&2
  exit 2
}
if [[ -n $log_dir ]]; then
  command -v "${PYTHON:-python3}" >/dev/null
  "${PYTHON:-python3}" -B scripts/summarize-go-test.py --help >/dev/null
fi
if [[ $check_only == true ]]; then
  go version
  echo 'configuration_preflight=PASS groups=consolidated,source-media,agentcap,limitpolicy,workloadadmission'
  echo 'Database/Redis reachability, schema state and environment authorization still require the batch preflight.'
  exit 0
fi
if [[ -n $log_dir ]]; then
  mkdir "$log_dir"  # Never reuse or overwrite an earlier evidence directory.
  log_dir=$(cd "$log_dir" && pwd)
fi
run_group() {
  local group=$1 status=0 summary_status=0
  shift
  if [[ -z $log_dir ]]; then
    "$@"
    return
  fi
  local log="$log_dir/$group.jsonl"
  # Add structured output only in local log mode; preserve every test argument.
  "${@:1:2}" -json "${@:3}" > "$log" 2>&1 || status=$?
  "${PYTHON:-python3}" -B scripts/summarize-go-test.py "$log" --exit-code "$status" --group "$group" --runner scripts/test-backend-integration.sh || summary_status=$?
  if ((status != 0)); then return "$status"; fi
  return "$summary_status"
}

test_pattern='^(Test(ConnectorManager|ConnectorRedeem|PostgresPhaseJournal|XInsightConnector(CP1|CP2A|RedeemLifecycle)).*'
test_pattern+='|Test(ApplyDeterministicReset|ApplyStopsBeforeRedisMutation|SecondIntegrationResetSupportMigration|FirstCustomerBootstrap|ResetPreviewCLI).*'
test_pattern+='|Test(PostgresStore|ProjectionStore|Enterprise|Allocation|PRD8RuntimeChange|PRD8RuntimeControl|PRD8GatewayModelAuthorization).*'
test_pattern+='|TestAdminSessionRevocationMigration(ConstraintsIntegration|UnusedDownAndEvidenceGuardsIntegration)'
test_pattern+='|TestAdmin(SetPassword|Create)CLI.*Integration'
test_pattern+='|TestGatewayAuth_PersonalProjectAuthorityRevocation_Integration'
test_pattern+='|TestV1(DeliveryAttemptBinding|StreamLeaseFencing|LocalLoginIdentity|ProviderBackedFrozenReservation|StreamFence|CheckpointCannotReviveLease|StreamGenerationFences|GenericRecoveryRejects|Pricing(Authority|UnitContract)|MicroPrecisionLedger|CommercialReleaseControlMigrationAndConcurrentResourceLimit).*'
test_pattern+='|TestUpgradeRecordAppliedPinsRuntimeDigestSequenceAndSchema'
test_pattern+='|TestRuntimeGenerationCLI.*Integration'
test_pattern+='|TestMaterializationProjectFenceOrdersVisibilityByCommit|TestMaterializationRollbackAndReplayRemainSingleFinalItem|TestRetentionUsesPublicationOrderNotTerminalTime|TestRetentionProjectFencePreventsFloorJumpOnConcurrentRollback'
test_pattern+='|TestBillingTerminalMaterializationFailureIsRetriedWithoutPartialFeedState|TestCompletedExecutionFeedPurposeCredentialIsLegacyBinaryFailClosed|TestCompletedExecutionFeedMigrationDownRemainsAvailableWithoutFeedState|TestCompletedExecutionFeedProjectScopeSurvivesGuardrailRollbackThenFeedRollback'
test_pattern+='|TestCompletedExecutionFeedKeyControlPlaneLifecycleIntegration|TestUpgradeLegacyRollbackPreflightRequiresFeedCredentialDisablement'
test_pattern+='|Test(RoutingPolicyPostgresStateTransitions|P6F08RoutingPolicyV1).*'
test_pattern+='|TestEventService_CheckProjectBudgetTenantScope_Integration'
test_pattern+='|TestAgentCapabilityPostgresAuthorityAndRevocationIntegration'
test_pattern+='|TestModelOperation(HTTPSOriginGoSQLGoldenVectors|ProductionQualification(DatabaseGuards|DownRejects(FrozenEvidence|FutureM76)))Integration'
test_pattern+='|Test(ModelDeploymentLifecycleReceipt|DeploymentLifecycleReceipt|InitialDeploymentLifecycleReceipt).*Integration'
test_pattern+='|Test(RepeatedModelSuccessor|RepeatedSuccessor|ModelSuccessorCutoverAtomic|NeverActivatedStagingModelSuccessor|RetirementRejectsPreparedPlan).*Integration'
test_pattern+='|TestAtomicModelReleaseAuthoring.*Integration'
test_pattern+='|TestSealedModelExecutionContract.*Integration'
test_pattern+='|TestDataReleaseAuthoringPinsDefinitionWithoutRewritingModelOrHistory|TestSealedCapabilityDecisionSurvivesSelectorButHonorsDisable'
test_pattern+='|TestAuthorizeOwnerSurvivesCommitAmbiguityAcrossCompatibleExpandIntegration'
test_pattern+='|TestEnsureBaseline.*Integration'
test_pattern+='|TestCanonicalConnectionLicenseAuthority.*Integration'
test_pattern+='|TestObservationProviderCostReconciliationMigrationIntegration'
test_pattern+='|TestVersionedModelExecutionProfilesMigrationIntegration'
test_pattern+='|TestModelExecutionPlanFreezeFence.*Integration|TestExactPublicExecutionBindings.*Integration'
test_pattern+='|TestModelCapabilityQualification.*Integration'
test_pattern+='|TestGLMM113ActualArtifactsGuardedRoundTripIntegration'
test_pattern+='|TestPlannerAndCutoverSelectTheSamePreferredCapabilityApprovalIntegration'
test_pattern+='|TestPreDispatchValidatesBoundCapabilityApprovalInsteadOfLatestIntegration'
test_pattern+='|TestOfficialRateSourceCurrencyDraftMigrationGuardsUSDReviewEvidence'
test_pattern+='|TestOfficialRateDraftImportIsAtomicEvidenceBackedAndNeverActive'
test_pattern+='|TestApply(AndCheckLegacyCompatibilityAuthorityAreIdempotentAndReadOnly|SealsCurrentLegacyStableAndCanaryBeforeVerifyCheck|FailsClosedWhenCurrentLegacyCanaryProjectionDrifts|AtExactCanonicalConnectionLimitRemainsIdempotentIntegration)'
test_pattern+='|TestPublicModelExecutionAuthority.*Integration'
test_pattern+='|TestPublicResponsesExactProfile.*Integration|TestPublicClaude.*Integration'
test_pattern+='|TestPublicCoordinator.*Integration'
test_pattern+='|TestSourceFacts.*Integration|TestSourceFacts(Expand|Down).*'
test_pattern+='|TestSource(Observations|Policy|Dispatch).*|TestSourceMediaMainStartupBoundaries|TestSourceBudget.*'
test_pattern+='|TestContentControl.*Integration|TestContentPolicy.*Integration|TestContentCaptureMigration.*'
test_pattern+='|TestM115ToM121CompatibleExpandPreservesExistingKey'
test_pattern+='|TestHistoricalM115RetirementPermitsOnlyProvedSuffix|TestRetirementRacesFastAndSlowRenewWithoutReusingStoppedIdentity'
test_pattern+='|TestContentHTTP.*PG|TestContentPublic.*PG'
test_pattern+='|TestLegacyReleaseAdmissionAllowExecutesStagedChannelIntegration'
test_pattern+='|TestOwnedProviderAttemptAndTerminalHealthFenceAreReplaySafe'
test_pattern+='|TestFreezeReplayRejectsChangedExecutionSnapshot|TestStreamOutputHold.*Integration|TestProviderCompletionRelayHoldSurvivesRecoveryIntegration'
test_pattern+='|TestPendingAdmission(IndependentRequestsAndRecovery|DurableEvidenceFailsClosed|ExplicitRisksStillBlockMixedScope|StreamRequiresDurableFundedUsage|PreparationFundingFailureStillBlocks|LogicalCredentialRotationKeepsRiskScope)'
test_pattern+='|TestP6F(06|07|08|09|10|11|20|21|40|41).*)$'
readonly test_pattern

run_group consolidated go test -tags=integration -race \
  ./internal/xinsightconnector ./internal/migrate \
  ./internal/resetexec ./cmd/uniroute \
  ./internal/orgdirectory ./internal/enterprisefinance ./internal/runtimechange ./internal/runtimegeneration \
  ./internal/customercontext ./internal/modelrouteauthority \
  ./internal/billing ./internal/modelexecution ./internal/modelrouting ./internal/middleware ./internal/completedfeed \
  ./internal/gateway ./internal/admin ./internal/outbox ./internal/terminalhook \
  ./internal/modelcapabilitypublication \
  -run "$test_pattern" -count=1 -timeout=20m

# Fixed-profile source, content policy and original media writers require their
# real transaction/retention tests; the untagged race suite does not
# select these files. Keep the pre-existing integration group intact.
run_group source-media go test -tags=integration -race ./internal/sourcefacts ./internal/mediajob ./internal/contentcapture \
  -count=1 -timeout=20m

# These packages intentionally share and reset the dedicated Redis
# test database, so package-level parallelism would corrupt their
# observations. Untagged tests already ran in the full race suite.
run_group agentcap go test -tags=integration -race ./internal/agentcap \
  -run '^TestAgentCapabilityPostgresAuthorityAndRevocationIntegration$' \
  -count=1 -timeout=20m
run_group limitpolicy go test -tags=integration -race ./internal/limitpolicy \
  -run '^TestP6F07PostgresRedisMultiInstanceIntegration$' \
  -count=1 -timeout=20m
run_group workloadadmission go test -tags=integration -race ./internal/workloadadmission \
  -run '^TestP6F21RedisMultiInstanceLeaseAndFaultIntegration$' \
  -count=1 -timeout=20m
