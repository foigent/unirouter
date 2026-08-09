package limitpolicy

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/uniroute/unirouter/internal/accessscope"
	"github.com/uniroute/unirouter/internal/testutil"
	"github.com/uniroute/unirouter/internal/workloadadmission"
)

func newPolicyTestService(t *testing.T, options Options, policies ...Policy) (*Service, *miniredis.Miniredis) {
	return newPolicyTestServiceWithClient(t, options, false, policies...)
}

func newPolicyBudgetTestService(t *testing.T, options Options, policies ...Policy) (*Service, *miniredis.Miniredis) {
	return newPolicyTestServiceWithClient(t, options, true, policies...)
}

func newPolicyTestServiceWithClient(t *testing.T, options Options, boundedClient bool, policies ...Policy) (*Service, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	clientOptions := &redis.Options{Addr: mr.Addr()}
	if boundedClient {
		clientOptions.ContextTimeoutEnabled = true
		clientOptions.MaxRetries = -1
	}
	client := redis.NewClient(clientOptions)
	t.Cleanup(func() { _ = client.Close() })
	if boundedClient {
		loadCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		for _, script := range []*redis.Script{reserveScript, extendScript, terminalScript} {
			if err := script.Load(loadCtx, client).Err(); err != nil {
				t.Fatalf("preload limit-policy miniredis script: %v", err)
			}
		}
	}
	service, err := NewService(nil, client, options)
	if err != nil {
		t.Fatal(err)
	}
	for index := range policies {
		if policies[index].Ref == "" {
			policies[index].Ref = fmt.Sprintf("policy:%d", policies[index].ID)
		}
		if policies[index].RuntimeBucket == "" {
			policies[index].RuntimeBucket = policies[index].Ref
		}
		if policies[index].Version == 0 {
			policies[index].Version = 1
		}
		if policies[index].Name == "" {
			policies[index].Name = policies[index].Ref
		}
	}
	service.snapshot.Store(buildPolicySnapshot(policies, nil, false, time.Now()))
	return service, mr
}

func TestReadinessMirrorsFailClosedAdmissionPreconditions(t *testing.T) {
	zero := int64(0)
	tests := []struct {
		name   string
		mutate func(*Subject)
		reason string
	}{
		{"credential rps", func(subject *Subject) {
			subject.AccessCredentialID = 17
			subject.CredentialRef = "credential_17"
			subject.BaseRPSLimit = 0
		}, ReasonRPSExceeded},
		{"credential rpm", func(subject *Subject) {
			subject.AccessCredentialID = 17
			subject.CredentialRef = "credential_17"
			subject.BaseRPMLimit = 0
		}, ReasonRPMExceeded},
		{"credential concurrency", func(subject *Subject) {
			subject.AccessCredentialID = 17
			subject.CredentialRef = "credential_17"
			subject.CredentialConcurrencyLimit = &zero
		}, ReasonConcurrency},
		{"grant rps", func(subject *Subject) { subject.AccessGrantID = 19; subject.GrantRPSLimit = &zero }, ReasonRPSExceeded},
		{"grant rpm", func(subject *Subject) { subject.AccessGrantID = 19; subject.GrantRPMLimit = &zero }, ReasonRPMExceeded},
		{"grant concurrency", func(subject *Subject) { subject.AccessGrantID = 19; subject.GrantConcurrencyLimit = &zero }, ReasonConcurrency},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, _ := newPolicyTestService(t, testOptions())
			subject := admission("readiness-zero", 1).Subject
			test.mutate(&subject)
			projection, err := service.Readiness(context.Background(), subject)
			if err != nil || projection.State != ReadinessBlocked || projection.Reason != test.reason {
				t.Fatalf("readiness = %#v, %v; want blocked %s", projection, err, test.reason)
			}
		})
	}

	t.Run("stale snapshot", func(t *testing.T) {
		options := testOptions()
		options.SnapshotMaxAge = 10 * time.Millisecond
		service, _ := newPolicyTestService(t, options)
		service.db = &testutil.MockDB{}
		service.snapshot.Store(buildPolicySnapshot(nil, nil, true, time.Now().Add(-time.Second)))
		projection, err := service.Readiness(context.Background(), admission("readiness-stale", 1).Subject)
		if err != nil || projection.State != ReadinessUnavailable || projection.Reason != ReasonSnapshotStale {
			t.Fatalf("readiness = %#v, %v; want unavailable stale snapshot", projection, err)
		}
	})

	t.Run("policy chain", func(t *testing.T) {
		options := testOptions()
		options.MaxMatchingPolicies = 3
		service, _ := newPolicyTestService(t, options,
			Policy{ID: 31, Mode: ModeEnforce, FailureMode: FailureFailClosed, RPMLimit: 10},
			Policy{ID: 32, Mode: ModeEnforce, FailureMode: FailureFailClosed, RPMLimit: 10},
		)
		projection, err := service.Readiness(context.Background(), admission("readiness-chain", 1).Subject)
		if err != nil || projection.State != ReadinessUnavailable || projection.Reason != ReasonPolicyChainLarge {
			t.Fatalf("readiness = %#v, %v; want unavailable policy chain", projection, err)
		}
	})

	t.Run("redis unavailable", func(t *testing.T) {
		service, server := newPolicyTestService(t, testOptions())
		server.Close()
		projection, err := service.Readiness(context.Background(), admission("readiness-redis", 1).Subject)
		if err != nil || projection.State != ReadinessUnavailable || projection.Reason != ReasonStateUnavailable {
			t.Fatalf("readiness = %#v, %v; want unavailable Redis state", projection, err)
		}
	})
}

func testOptions() Options {
	options := DefaultOptions()
	options.GlobalConcurrency = 1_000
	options.OrgConcurrency = 1_000
	return options
}

func admission(operation string, tokens int64) AdmissionRequest {
	return AdmissionRequest{
		OperationID: operation,
		Subject: Subject{
			OrgID: 7, ProjectID: 11, APIKeyID: 13, Model: "company-chat",
			PolicyLabels: []string{"production", "customer-facing"},
			BaseRPSLimit: 1_000, BaseRPMLimit: 1_000,
		},
		EstimatedInput: tokens,
		ReservedOutput: tokens,
		LeaseTTL:       time.Minute,
	}
}

func TestBuiltinAccessGrantPolicyBindsOrganizationAndGrant(t *testing.T) {
	service := &Service{}
	rps, rpm, concurrency := int64(3), int64(30), int64(2)
	subject := Subject{
		OrgID: 7, ProjectID: 11, APIKeyID: 13, AccessGrantID: 17,
		BaseRPSLimit: 100, BaseRPMLimit: 1000,
		GrantRPSLimit: &rps, GrantRPMLimit: &rpm, GrantConcurrencyLimit: &concurrency,
	}
	policies := service.matchingPolicies(subject)
	if len(policies) != 2 {
		t.Fatalf("expected API-key and access-grant policies, got %d", len(policies))
	}
	grantPolicy := policies[1]
	if err := grantPolicy.Validate(); err != nil {
		t.Fatalf("built-in access-grant policy must satisfy the persisted policy contract: %v", err)
	}
	if grantPolicy.OrgID == nil || *grantPolicy.OrgID != subject.OrgID ||
		grantPolicy.AccessGrantID == nil || *grantPolicy.AccessGrantID != subject.AccessGrantID {
		t.Fatalf("unexpected access-grant selector: org=%v grant=%v", grantPolicy.OrgID, grantPolicy.AccessGrantID)
	}
	if !grantPolicy.Matches(subject) {
		t.Fatal("access-grant policy must match its exact organization and grant")
	}
	foreignOrganization := subject
	foreignOrganization.OrgID++
	if grantPolicy.Matches(foreignOrganization) {
		t.Fatal("access-grant policy must not match the same grant selector in another organization")
	}
	if got, want := grantPolicy.RuntimeBucket, "builtin:access-grant:17"; got != want {
		t.Fatalf("runtime bucket must remain fixed to the globally unique grant identity: got %q want %q", got, want)
	}
	foreignOrgID := foreignOrganization.OrgID
	foreignSelector := grantPolicy
	foreignSelector.OrgID = &foreignOrgID
	if grantPolicy.selectorHash() == foreignSelector.selectorHash() {
		t.Fatal("selector hash must bind the organization as well as the grant")
	}
}

func TestReadinessProjectsAuthoritativePoliciesWithoutRedisMutation(t *testing.T) {
	orgID, projectID := int64(7), int64(11)
	service, redisServer := newPolicyTestService(t, testOptions(), Policy{
		ID: 91, Ref: "policy:91", Name: "Project readiness", Version: 1,
		OrgID: &orgID, ProjectID: &projectID, Mode: ModeEnforce,
		FailureMode: FailureFailClosed, RPMLimit: 30,
	})
	before := redisServer.Keys()
	projection, err := service.Readiness(context.Background(), Subject{
		OrgID: orgID, ProjectID: projectID, APIKeyID: 13,
		Model: "company-chat", BaseRPSLimit: 10, BaseRPMLimit: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	if projection.MatchingPolicyCount != 2 || projection.EnforcedPolicyCount != 2 {
		t.Fatalf("unexpected safe projection: %#v", projection)
	}
	after := redisServer.Keys()
	if len(before) != len(after) {
		t.Fatalf("readiness mutated Redis: before=%v after=%v", before, after)
	}
}

func TestBuiltinAccessGrantPolicyMaterializesCanonicalDefaultRates(t *testing.T) {
	service := &Service{}
	subject := Subject{
		OrgID: 7, ProjectID: 11, APIKeyID: 13, AccessGrantID: 17,
		BaseRPSLimit: 100, BaseRPMLimit: 1000,
	}
	policies := service.matchingPolicies(subject)
	if len(policies) != 2 {
		t.Fatalf("expected credential/key and access-grant policies, got %d", len(policies))
	}
	grantPolicy := policies[1]
	if got, want := grantPolicy.RPMLimit, int64(accessscope.ProjectDefaultRPMLimit); got != want {
		t.Fatalf("nil Grant RPM materialized as %d, want %d", got, want)
	}
	if got, want := grantPolicy.RPSLimit, int64(accessscope.ProjectDefaultRPSLimit); got != want {
		t.Fatalf("nil Grant RPS materialized as %d, want %d", got, want)
	}
}

func TestMediaAndModelCallsShareLogicalCredentialAndGrantBuckets(t *testing.T) {
	service, _ := newPolicyTestService(t, testOptions())
	credentialConcurrency := int64(2)
	grantRPS, grantRPM, grantConcurrency := int64(2), int64(100), int64(1)
	subject := Subject{
		OrgID: 7, ProjectID: 11, APIKeyID: 13,
		AccessCredentialID: 17, CredentialRef: "credential-shared-runtime",
		AccessGrantID: 19, Model: "company-image",
		BaseRPSLimit: 2, BaseRPMLimit: 100,
		CredentialConcurrencyLimit: &credentialConcurrency,
		GrantRPSLimit:              &grantRPS, GrantRPMLimit: &grantRPM,
		GrantConcurrencyLimit: &grantConcurrency,
	}

	mediaRate := AdmissionRequest{
		OperationID: "media-rate-first", Subject: subject,
		LeaseTTL: time.Minute, Phase: AdmissionPhaseRate,
	}
	mediaReservation, err := service.Reserve(context.Background(), mediaRate)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Finalize(context.Background(), mediaReservation, 0); err != nil {
		t.Fatal(err)
	}

	chat := AdmissionRequest{
		OperationID: "chat-full-second", Subject: subject,
		EstimatedInput: 1, ReservedOutput: 1, LeaseTTL: time.Minute,
		Phase: AdmissionPhaseFull,
	}
	chatReservation, err := service.Reserve(context.Background(), chat)
	if err != nil {
		t.Fatalf("chat did not share the same available rate lane: %v", err)
	}
	if err := service.CancelBeforeDispatch(context.Background(), chatReservation); err != nil {
		t.Fatal(err)
	}

	rotatedMedia := mediaRate
	rotatedMedia.OperationID = "media-rate-rotated-version"
	rotatedMedia.Subject.APIKeyID = 99
	if _, err := service.Reserve(context.Background(), rotatedMedia); !errors.Is(err, ErrRPSExceeded) {
		t.Fatalf("physical-key rotation escaped logical family rate bucket: %v", err)
	}

	otherFamily := subject
	otherFamily.APIKeyID = 21
	otherFamily.AccessCredentialID = 23
	otherFamily.CredentialRef = "credential-other-family"
	otherFamily.BaseRPSLimit = 100
	otherFamilyRate := mediaRate
	otherFamilyRate.OperationID = "media-rate-other-family"
	otherFamilyRate.Subject = otherFamily
	if _, err := service.Reserve(context.Background(), otherFamilyRate); !errors.Is(err, ErrRPSExceeded) {
		t.Fatalf("second family escaped shared Grant rate bucket: %v", err)
	}
}

func TestMediaExecutionAndModelCallsShareGrantConcurrency(t *testing.T) {
	service, _ := newPolicyTestService(t, testOptions())
	credentialConcurrency := int64(1)
	grantConcurrency := int64(1)
	firstSubject := Subject{
		OrgID: 7, ProjectID: 11, APIKeyID: 13,
		AccessCredentialID: 17, CredentialRef: "credential-concurrency-one",
		AccessGrantID: 19, Model: "company-image",
		BaseRPSLimit: 100, BaseRPMLimit: 1_000,
		CredentialConcurrencyLimit: &credentialConcurrency,
		GrantConcurrencyLimit:      &grantConcurrency,
	}
	chat := AdmissionRequest{
		OperationID: "chat-concurrency-holder", Subject: firstSubject,
		EstimatedInput: 1, ReservedOutput: 1, LeaseTTL: time.Minute,
		Phase: AdmissionPhaseFull,
	}
	chatReservation, err := service.Reserve(context.Background(), chat)
	if err != nil {
		t.Fatal(err)
	}

	secondSubject := firstSubject
	secondSubject.APIKeyID = 21
	secondSubject.AccessCredentialID = 23
	secondSubject.CredentialRef = "credential-concurrency-two"
	mediaExecution := AdmissionRequest{
		OperationID: "media-execution-blocked", Subject: secondSubject,
		LeaseTTL: time.Minute, WorkloadClass: workloadadmission.ClassInteractive,
		Phase: AdmissionPhaseExecution,
	}
	if _, err := service.Reserve(context.Background(), mediaExecution); !errors.Is(err, ErrConcurrencyExceeded) {
		t.Fatalf("media execution escaped the synchronous Grant concurrency lane: %v", err)
	}
	if err := service.CancelBeforeDispatch(context.Background(), chatReservation); err != nil {
		t.Fatal(err)
	}
	mediaExecution.OperationID = "media-execution-after-release"
	mediaReservation, err := service.Reserve(context.Background(), mediaExecution)
	if err != nil {
		t.Fatalf("media execution remained blocked after terminal release: %v", err)
	}
	if err := service.CancelBeforeDispatch(context.Background(), mediaReservation); err != nil {
		t.Fatal(err)
	}
}

func TestSharedConcurrencyIsReplicaInvariant(t *testing.T) {
	options := testOptions()
	policy := Policy{ID: 1, Ref: "policy:concurrency", Name: "shared", Version: 1, Mode: ModeEnforce, FailureMode: FailureFailClosed, ConcurrencyLimit: 3}
	first, mr := newPolicyTestService(t, options, policy)
	secondClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = secondClient.Close() })
	second, err := NewService(nil, secondClient, options)
	if err != nil {
		t.Fatal(err)
	}
	second.snapshot.Store(&policySnapshot{policies: []Policy{policy}})
	// This test exercises atomic shared concurrency across clients, not Redis
	// script bootstrap. Preload once so concurrent first use cannot turn a
	// transient NOSCRIPT/EVAL bootstrap error into an unrelated reservation
	// result under race instrumentation.
	if err := reserveScript.Load(context.Background(), first.rdb).Err(); err != nil {
		t.Fatalf("preload shared concurrency script: %v", err)
	}

	var admitted atomic.Int64
	var denied atomic.Int64
	unexpectedErrors := make(chan error, 20)
	var wg sync.WaitGroup
	for index := 0; index < 20; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			service := first
			if index%2 == 1 {
				service = second
			}
			_, reserveErr := service.Reserve(context.Background(), admission(fmt.Sprintf("op-%02d", index), 1))
			switch {
			case reserveErr == nil:
				admitted.Add(1)
			case errors.Is(reserveErr, ErrConcurrencyExceeded):
				denied.Add(1)
			default:
				unexpectedErrors <- reserveErr
			}
		}(index)
	}
	wg.Wait()
	close(unexpectedErrors)
	unexpected := make([]error, 0, len(unexpectedErrors))
	for err := range unexpectedErrors {
		unexpected = append(unexpected, err)
	}
	if admitted.Load() != 3 || denied.Load() != 17 || len(unexpected) != 0 {
		t.Fatalf("admitted=%d denied=%d unexpected=%d errors=%v", admitted.Load(), denied.Load(), len(unexpected), unexpected)
	}
}

func TestExactRateAndAllMatchingPolicies(t *testing.T) {
	orgID := int64(7)
	keyID := int64(13)
	otherKey := int64(99)
	service, _ := newPolicyTestService(t, testOptions(),
		Policy{ID: 2, Ref: "policy:org-rpm", Name: "org", Version: 1, OrgID: &orgID, Mode: ModeEnforce, FailureMode: FailureFailClosed, RPMLimit: 10},
		Policy{ID: 3, Ref: "policy:key-rps", Name: "key", Version: 1, OrgID: &orgID, APIKeyID: &keyID, Mode: ModeEnforce, FailureMode: FailureFailClosed, RPSLimit: 2},
		Policy{ID: 4, Ref: "policy:other", Name: "other", Version: 1, OrgID: &orgID, APIKeyID: &otherKey, Mode: ModeEnforce, FailureMode: FailureFailClosed, RPSLimit: 1},
	)
	for index := 0; index < 2; index++ {
		reservation, err := service.Reserve(context.Background(), admission(fmt.Sprintf("rate-%d", index), 1))
		if err != nil {
			t.Fatal(err)
		}
		if err := service.CancelBeforeDispatch(context.Background(), reservation); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.Reserve(context.Background(), admission("rate-2", 1)); !errors.Is(err, ErrRPSExceeded) {
		t.Fatalf("third request error = %v, want ErrRPSExceeded", err)
	}
}

func TestExplicitPolicyDenialStillConsumesAPIKeyAttemptRate(t *testing.T) {
	service, _ := newPolicyTestService(t, testOptions(), Policy{
		ID: 40, Ref: "policy:deny-after-key-rate", Name: "shared", Version: 1,
		Mode: ModeEnforce, FailureMode: FailureFailClosed, ConcurrencyLimit: 1,
	})
	request := admission("key-rate-first", 1)
	request.Subject.BaseRPSLimit = 2
	request.Subject.BaseRPMLimit = 1_000
	first, err := service.Reserve(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	request.OperationID = "key-rate-second"
	if _, err := service.Reserve(context.Background(), request); !errors.Is(err, ErrConcurrencyExceeded) {
		t.Fatalf("second request = %v, want explicit concurrency denial", err)
	}
	request.OperationID = "key-rate-third"
	if _, err := service.Reserve(context.Background(), request); !errors.Is(err, ErrRPSExceeded) {
		t.Fatalf("third request = %v, want API-key attempt-rate denial", err)
	}
	if err := service.CancelBeforeDispatch(context.Background(), first); err != nil {
		t.Fatal(err)
	}
}

func TestP6F21WorkloadAdmissionRunsAfterAPIKeyAttemptRate(t *testing.T) {
	options := testOptions()
	options.GlobalConcurrency = 1
	options.OrgConcurrency = 1
	options.Workload.GlobalShortReserve = 0
	options.Workload.OrgShortReserve = 0
	// This fixture proves API-key-attempt/workload ordering, not a production
	// latency SLO. Keep the Go Lua emulator budget separate from real Redis
	// timing authority so race instrumentation cannot trigger fallback admission.
	options.Workload.Interactive = workloadadmission.ClassPolicy{Weight: 1, MaxWait: 250 * time.Millisecond}
	options.Workload.Stream = workloadadmission.ClassPolicy{Weight: 1, MaxWait: 250 * time.Millisecond}
	options.Workload.Embedding = workloadadmission.ClassPolicy{Weight: 1, MaxWait: 250 * time.Millisecond}
	options.Workload.StoreTimeout = 500 * time.Millisecond
	service, _ := newPolicyBudgetTestService(t, options)
	firstRequest := admission("workload-first", 1)
	firstRequest.Subject.BaseRPSLimit = 1_000
	firstRequest.Subject.BaseRPMLimit = 2
	firstRequest.WorkloadClass = workloadadmission.ClassInteractive
	// Lease expiry is not under test. Keep it well beyond the instrumented
	// emulator's execution budget so a slow fake Lua VM cannot turn capacity
	// denial into an unrelated expiry admission.
	firstRequest.LeaseTTL = 10 * time.Minute
	first, err := service.Reserve(context.Background(), firstRequest)
	if err != nil {
		t.Fatal(err)
	}

	secondRequest := firstRequest
	secondRequest.OperationID = "workload-second"
	ctx, collector := ContextWithDecisionCollector(context.Background())
	if _, err := service.Reserve(ctx, secondRequest); !errors.Is(err, ErrWorkloadCapacity) {
		t.Fatalf("second request=%v, want ErrWorkloadCapacity", err)
	}
	decisions, version := collector.Snapshot()
	if version == "" || len(decisions) < 2 {
		t.Fatalf("missing composed policy/workload evidence: version=%q decisions=%#v", version, decisions)
	}
	workloadDecision := decisions[len(decisions)-1]
	if workloadDecision.Reason != ReasonWorkloadCapacity || workloadDecision.WorkloadClass != workloadadmission.ClassInteractive || workloadDecision.WorkloadWeight != 1 {
		t.Fatalf("workload decision=%#v", workloadDecision)
	}

	thirdRequest := firstRequest
	thirdRequest.OperationID = "workload-third"
	if _, err := service.Reserve(context.Background(), thirdRequest); !errors.Is(err, ErrRPMExceeded) {
		t.Fatalf("workload-denied attempt did not consume API-key RPM: %v", err)
	}
	if err := service.CancelBeforeDispatch(context.Background(), first); err != nil {
		t.Fatal(err)
	}
}

func TestTPMReservationSettlementAndActualOverrun(t *testing.T) {
	service, _ := newPolicyTestService(t, testOptions(), Policy{
		ID: 5, Ref: "policy:tpm", Name: "tpm", Version: 1, Mode: ModeEnforce,
		FailureMode: FailureFailClosed, TPMLimit: 100,
	})
	first, err := service.Reserve(context.Background(), AdmissionRequest{
		OperationID: "tpm-first", Subject: admission("x", 1).Subject,
		EstimatedInput: 20, ReservedOutput: 60, LeaseTTL: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Reserve(context.Background(), admission("tpm-blocked", 15)); !errors.Is(err, ErrTPMExceeded) {
		t.Fatalf("reservation above remaining TPM = %v", err)
	}
	if err := service.Finalize(context.Background(), first, 30); err != nil {
		t.Fatal(err)
	}
	second, err := service.Reserve(context.Background(), admission("tpm-second", 20))
	if err != nil {
		t.Fatalf("settlement should replace 80 reserved tokens with 30 actual: %v", err)
	}
	if err := service.Finalize(context.Background(), second, 90); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Reserve(context.Background(), admission("tpm-after-overrun", 1)); !errors.Is(err, ErrTPMExceeded) {
		t.Fatalf("future request after actual overrun = %v, want ErrTPMExceeded", err)
	}
}

func TestP6F11TPMExtensionIsIdempotentAndFailsBeforeDisclosure(t *testing.T) {
	service, _ := newPolicyTestService(t, testOptions(), Policy{
		ID: 51, Ref: "policy:p6f11-tpm", Name: "p6f11-tpm", Version: 1, Mode: ModeEnforce,
		FailureMode: FailureFailClosed, TPMLimit: 100,
	})
	reservation, err := service.Reserve(context.Background(), AdmissionRequest{
		OperationID: "p6f11-extension", Subject: admission("x", 1).Subject,
		EstimatedInput: 20, ReservedOutput: 20, LeaseTTL: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Extend(context.Background(), reservation, 1, 30); err != nil {
		t.Fatalf("first extension: %v", err)
	}
	if err := service.Extend(context.Background(), reservation, 1, 30); err != nil {
		t.Fatalf("idempotent extension replay: %v", err)
	}
	if err := service.Extend(context.Background(), reservation, 1, 31); !errors.Is(err, ErrConflict) {
		t.Fatalf("same extension sequence with a different amount = %v, want ErrConflict", err)
	}
	if reservation.ExtensionSequence != 1 || len(reservation.Items) == 0 || reservation.Items[0].ReservedTokens != 70 {
		t.Fatalf("extension was not applied exactly once: %#v", reservation)
	}

	competing, err := service.Reserve(context.Background(), AdmissionRequest{
		OperationID: "p6f11-competing", Subject: admission("x", 1).Subject,
		EstimatedInput: 10, ReservedOutput: 10, LeaseTTL: time.Minute,
	})
	if err != nil {
		t.Fatalf("reserve remaining TPM: %v", err)
	}
	if err := service.Extend(context.Background(), reservation, 2, 20); !errors.Is(err, ErrTPMExceeded) {
		t.Fatalf("extension over shared TPM = %v, want ErrTPMExceeded", err)
	}
	if reservation.ExtensionSequence != 1 || reservation.Items[0].ReservedTokens != 70 {
		t.Fatalf("denied extension changed local authority: %#v", reservation)
	}
	if err := service.Finalize(context.Background(), reservation, 45); err != nil {
		t.Fatal(err)
	}
	if err := service.CancelBeforeDispatch(context.Background(), competing); err != nil {
		t.Fatal(err)
	}
}

func TestCancelBeforeDispatchReleasesTokensButRateAttemptsRemain(t *testing.T) {
	service, _ := newPolicyTestService(t, testOptions(), Policy{
		ID: 6, Ref: "policy:cancel", Name: "cancel", Version: 1, Mode: ModeEnforce,
		FailureMode: FailureFailClosed, RPMLimit: 2, TPMLimit: 10,
	})
	first, err := service.Reserve(context.Background(), admission("cancel-first", 5))
	if err != nil {
		t.Fatal(err)
	}
	if err := service.CancelBeforeDispatch(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	second, err := service.Reserve(context.Background(), admission("cancel-second", 5))
	if err != nil {
		t.Fatalf("pre-dispatch cancellation should release TPM reservation: %v", err)
	}
	if err := service.CancelBeforeDispatch(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Reserve(context.Background(), admission("cancel-third", 1)); !errors.Is(err, ErrRPMExceeded) {
		t.Fatalf("rate attempt should remain after cancellation: %v", err)
	}
}

func TestShadowModeRecordsWouldDenyWithoutBlocking(t *testing.T) {
	service, _ := newPolicyTestService(t, testOptions(), Policy{
		ID: 7, Ref: "policy:shadow", Name: "shadow", Version: 3, Mode: ModeShadow,
		FailureMode: FailureFailClosed, ConcurrencyLimit: 1,
	})
	first, err := service.Reserve(context.Background(), admission("shadow-first", 1))
	if err != nil {
		t.Fatal(err)
	}
	ctx, collector := ContextWithDecisionCollector(context.Background())
	second, err := service.Reserve(ctx, admission("shadow-second", 1))
	if err != nil {
		t.Fatalf("shadow policy must not block: %v", err)
	}
	decisions, version := collector.Snapshot()
	if version == "" {
		t.Fatal("missing policy-set version")
	}
	found := false
	for _, decision := range decisions {
		if decision.PolicyRef == "policy:shadow" {
			found = decision.Decision == DecisionWouldDeny && decision.Reason == ReasonConcurrency && decision.PolicyVersion == 3
		}
	}
	if !found {
		t.Fatalf("shadow decision not found: %#v", decisions)
	}
	_ = service.CancelBeforeDispatch(context.Background(), first)
	_ = service.CancelBeforeDispatch(context.Background(), second)
}

func TestLastKnownSafePolicyCannotBypassFailClosedWorkloadAdmission(t *testing.T) {
	options := testOptions()
	options.FallbackTTL = 2 * time.Second
	options.FallbackGlobalCap = 5
	service, mr := newPolicyTestService(t, options, Policy{
		ID: 8, Ref: "policy:lks", Name: "lks", Version: 1, Mode: ModeEnforce,
		FailureMode: FailureLastKnownSafe, ConcurrencyLimit: 10,
	})
	primed, err := service.Reserve(context.Background(), admission("lks-prime", 1))
	if err != nil {
		t.Fatal(err)
	}
	if err := service.CancelBeforeDispatch(context.Background(), primed); err != nil {
		t.Fatal(err)
	}
	mr.Close()
	ctx, collector := ContextWithDecisionCollector(context.Background())
	reservation, err := service.Reserve(ctx, admission("lks-once", 1))
	if !errors.Is(err, ErrStateUnavailable) || reservation != nil {
		t.Fatalf("shared Redis outage reservation=%#v err=%v, want fail-closed", reservation, err)
	}
	decisions, _ := collector.Snapshot()
	var policyFallback, workloadDenied bool
	for _, decision := range decisions {
		switch decision.PolicyRef {
		case "policy:lks":
			policyFallback = decision.Decision == DecisionAllow &&
				decision.Reason == ReasonLastKnownSafe && decision.Fallback
		case "builtin:workload-admission-v2":
			workloadDenied = decision.PolicyVersion == 2 &&
				decision.FailureMode == FailureFailClosed &&
				decision.Decision == DecisionDeny &&
				decision.Reason == ReasonStateUnavailable && !decision.Fallback
		}
	}
	if !policyFallback || !workloadDenied {
		t.Fatalf("composed outage decisions do not preserve policy evidence and fail-closed workload authority: %#v", decisions)
	}
}

func TestFallbackLeaseConsumptionAndCleanupAreBounded(t *testing.T) {
	service, _ := newPolicyTestService(t, testOptions())
	service.updateFallback("policy:bounded-fallback", time.Second)

	var admitted atomic.Int64
	var wait sync.WaitGroup
	for index := 0; index < 100; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if service.consumeFallback("policy:bounded-fallback") {
				admitted.Add(1)
			}
		}()
	}
	wait.Wait()
	if admitted.Load() != 1 {
		t.Fatalf("fallback admissions = %d, want exactly 1", admitted.Load())
	}

	service.updateFallback("policy:expired-fallback", time.Second)
	service.pruneFallback(time.Now().Add(2 * time.Second))
	if _, exists := service.fallback.Load("policy:expired-fallback"); exists {
		t.Fatal("expired fallback lease was retained in process memory")
	}
}

func TestFailClosedWithoutHealthyLease(t *testing.T) {
	service, mr := newPolicyTestService(t, testOptions(), Policy{
		ID: 9, Ref: "policy:closed", Name: "closed", Version: 1, Mode: ModeEnforce,
		FailureMode: FailureFailClosed, ConcurrencyLimit: 10,
	})
	mr.Close()
	if _, err := service.Reserve(context.Background(), admission("closed", 1)); !errors.Is(err, ErrStateUnavailable) {
		t.Fatalf("Redis outage = %v, want ErrStateUnavailable", err)
	}
}

func TestPolicyLabelsAreServerSubjectSelectors(t *testing.T) {
	orgID := int64(7)
	service, _ := newPolicyTestService(t, testOptions(), Policy{
		ID: 10, Ref: "policy:label", Name: "production", Version: 1, OrgID: &orgID,
		PolicyLabel: "production", Mode: ModeEnforce, FailureMode: FailureFailClosed, RPSLimit: 1,
	})
	first, err := service.Reserve(context.Background(), admission("label-first", 1))
	if err != nil {
		t.Fatal(err)
	}
	_ = service.CancelBeforeDispatch(context.Background(), first)
	if _, err := service.Reserve(context.Background(), admission("label-second", 1)); !errors.Is(err, ErrRPSExceeded) {
		t.Fatalf("matching label should enforce policy: %v", err)
	}
	nonmatching := admission("label-other", 1)
	nonmatching.Subject.PolicyLabels = []string{"staging"}
	if _, err := service.Reserve(context.Background(), nonmatching); err != nil {
		t.Fatalf("nonmatching label should not select policy: %v", err)
	}
}

func TestAuthoritativePolicyLabelsAlsoBindOperationIdentity(t *testing.T) {
	orgID := int64(7)
	policy := Policy{
		ID: 12, Ref: "policy:authoritative-label", Name: "production", Version: 1, OrgID: &orgID,
		PolicyLabel: "production", Mode: ModeEnforce, FailureMode: FailureFailClosed, RPMLimit: 100,
	}
	service, _ := newPolicyTestService(t, testOptions())
	service.snapshot.Store(buildPolicySnapshot(
		[]Policy{policy}, map[int64][]string{13: {"production"}}, true, time.Now(),
	))
	request := admission("authoritative-replay", 1)
	request.Subject.PolicyLabels = []string{"caller-value-one"}
	first, err := service.Reserve(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	request.Subject.PolicyLabels = []string{"caller-value-two"}
	replayed, err := service.Reserve(context.Background(), request)
	if err != nil {
		t.Fatalf("caller-supplied labels changed an authoritative operation identity: %v", err)
	}
	if !replayed.Idempotent {
		t.Fatal("authoritative replay was counted as a new admission")
	}
	if err := service.CancelBeforeDispatch(context.Background(), first); err != nil {
		t.Fatal(err)
	}
}

func TestOperationIdentityIsIdempotentAndBoundToSubject(t *testing.T) {
	service, _ := newPolicyTestService(t, testOptions(), Policy{
		ID: 11, Ref: "policy:identity", Name: "identity", Version: 1, Mode: ModeEnforce,
		FailureMode: FailureFailClosed, ConcurrencyLimit: 10,
	})
	request := admission("same-operation", 1)
	if _, err := service.Reserve(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Reserve(context.Background(), request); err != nil {
		t.Fatalf("identical replay should be idempotent: %v", err)
	}
	request.Subject.APIKeyID++
	if _, err := service.Reserve(context.Background(), request); !errors.Is(err, ErrConflict) {
		t.Fatalf("operation rebound to another key = %v, want ErrConflict", err)
	}
}

func TestRuntimeKeysUseOneRedisClusterHashTag(t *testing.T) {
	keys := operationRuntimeKeys("policy:cluster", "operation:one")
	if len(keys) != 8 {
		t.Fatalf("key count = %d", len(keys))
	}
	var tag string
	for _, key := range keys {
		open := -1
		close := -1
		for index, value := range key {
			if value == '{' {
				open = index
			}
			if value == '}' {
				close = index
				break
			}
		}
		if open < 0 || close <= open+1 {
			t.Fatalf("missing cluster hash tag: %s", key)
		}
		current := key[open+1 : close]
		if tag == "" {
			tag = current
		} else if current != tag {
			t.Fatalf("hash tag mismatch: %s vs %s", tag, current)
		}
	}
}

func TestSnapshotIndexAvoidsScanningUnrelatedTenantPolicies(t *testing.T) {
	policies := make([]Policy, 0, 1001)
	for index := int64(1); index <= 1000; index++ {
		orgID := index + 100
		policies = append(policies, Policy{
			ID: index, Ref: fmt.Sprintf("policy:%d", index), Name: "unrelated", Version: 1,
			OrgID: &orgID, Mode: ModeEnforce, FailureMode: FailureFailClosed, RPMLimit: 10,
		})
	}
	matchingOrg := int64(7)
	policies = append(policies, Policy{
		ID: 1001, Ref: "policy:1001", Name: "matching", Version: 1,
		OrgID: &matchingOrg, Mode: ModeEnforce, FailureMode: FailureFailClosed, RPMLimit: 10,
	})
	snapshot := buildPolicySnapshot(policies, nil, false, time.Now())
	candidates := snapshot.index.candidates(admission("index", 1).Subject)
	if len(candidates) != 1 || candidates[0].Ref != "policy:1001" {
		t.Fatalf("indexed candidates = %#v", candidates)
	}
}

func TestPolicyChainCardinalityIsBounded(t *testing.T) {
	options := testOptions()
	options.MaxMatchingPolicies = 3 // API-key, workload, and two configured policies exceed the bound
	service, _ := newPolicyTestService(t, options,
		Policy{ID: 20, Ref: "policy:20", Name: "one", Version: 1, Mode: ModeShadow, FailureMode: FailureFailClosed, RPMLimit: 10},
		Policy{ID: 21, Ref: "policy:21", Name: "two", Version: 1, Mode: ModeShadow, FailureMode: FailureFailClosed, RPMLimit: 10},
	)
	ctx, collector := ContextWithDecisionCollector(context.Background())
	if _, err := service.Reserve(ctx, admission("too-many-policies", 1)); !errors.Is(err, ErrStateUnavailable) {
		t.Fatalf("oversized policy chain = %v, want ErrStateUnavailable", err)
	}
	decisions, _ := collector.Snapshot()
	if len(decisions) != 1 || decisions[0].Reason != ReasonPolicyChainLarge {
		t.Fatalf("oversized chain decision = %#v", decisions)
	}
}

func TestBuiltinLimitChangesRotatePolicySetVersionWithoutResettingBuckets(t *testing.T) {
	firstOptions := testOptions()
	first, _ := newPolicyTestService(t, firstOptions)
	request := admission("builtin-version", 1)
	firstPolicies := first.matchingPolicies(request.Subject)

	secondOptions := firstOptions
	secondOptions.GlobalConcurrency++
	secondOptions.OrgConcurrency++
	second, _ := newPolicyTestService(t, secondOptions)
	secondRequest := request
	secondPolicies := second.matchingPolicies(secondRequest.Subject)

	if runtimePolicySetVersion(firstPolicies, first.workload.Version()) == runtimePolicySetVersion(secondPolicies, second.workload.Version()) {
		t.Fatal("built-in limit changes did not rotate the policy-set version")
	}
	if len(firstPolicies) != len(secondPolicies) {
		t.Fatalf("built-in policy count changed: %d vs %d", len(firstPolicies), len(secondPolicies))
	}
	for index := range firstPolicies {
		if firstPolicies[index].RuntimeBucket != secondPolicies[index].RuntimeBucket {
			t.Fatalf("limit-only change reset runtime bucket %q vs %q", firstPolicies[index].RuntimeBucket, secondPolicies[index].RuntimeBucket)
		}
	}
}

func TestStaleDatabaseSnapshotFailsClosed(t *testing.T) {
	options := testOptions()
	options.SnapshotMaxAge = 10 * time.Millisecond
	service, _ := newPolicyTestService(t, options)
	service.db = &testutil.MockDB{}
	service.snapshot.Store(buildPolicySnapshot(nil, nil, true, time.Now().Add(-time.Second)))
	ctx, collector := ContextWithDecisionCollector(context.Background())
	if _, err := service.Reserve(ctx, admission("stale-snapshot", 1)); !errors.Is(err, ErrStateUnavailable) {
		t.Fatalf("stale snapshot = %v, want ErrStateUnavailable", err)
	}
	decisions, _ := collector.Snapshot()
	if len(decisions) != 1 || decisions[0].Reason != ReasonSnapshotStale {
		t.Fatalf("stale snapshot decision = %#v", decisions)
	}
}

func TestSelectorChangesRotateRuntimePopulationButLimitChangesDoNot(t *testing.T) {
	orgA := int64(7)
	orgB := int64(8)
	base := Policy{ID: 30, Ref: "policy:30", Name: "scope", Version: 1, OrgID: &orgA, Mode: ModeEnforce, FailureMode: FailureFailClosed, RPMLimit: 10}
	limitChanged := base
	limitChanged.Version = 2
	limitChanged.RPMLimit = 20
	scopeChanged := base
	scopeChanged.Version = 2
	scopeChanged.OrgID = &orgB
	if policyRuntimeBucket(base) != policyRuntimeBucket(limitChanged) {
		t.Fatal("limit-only update reset the rolling policy population")
	}
	if policyRuntimeBucket(base) == policyRuntimeBucket(scopeChanged) {
		t.Fatal("selector change reused a cross-tenant runtime population")
	}
}
