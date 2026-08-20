package limitpolicy

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
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

func TestModelExecutionAdmissionProjectsOnlyRateLimits(t *testing.T) {
	t.Parallel()
	consumedPolicy := Policy{
		Ref: "policy:model-execution", Name: "model execution", Version: 1,
		Mode: ModeEnforce, FailureMode: FailureFailClosed,
		RPSLimit: 2, RPMLimit: 20, TPMLimit: 2_000, ConcurrencyLimit: 3,
	}
	canonicalOnly := Policy{
		Ref: "policy:canonical-only", Name: "canonical rate", Version: 1,
		Mode: ModeEnforce, FailureMode: FailureFailClosed, RPSLimit: 1, RPMLimit: 10,
	}
	consumedContract := rateAdmissionContractForPolicy(consumedPolicy, consumedPolicy.Ref, rateAdmissionKindRedis)
	projected, valid := projectPoliciesForAdmission(
		[]Policy{consumedPolicy, canonicalOnly}, AdmissionPhaseModelExecution, []rateAdmissionContract{consumedContract},
	)
	if !valid {
		t.Fatal("exact consumed rate contract was rejected")
	}
	if len(projected) != 2 {
		t.Fatalf("projected policies=%d, want 2", len(projected))
	}
	if projected[0].RPSLimit != 0 || projected[0].RPMLimit != 0 ||
		projected[0].TPMLimit != consumedPolicy.TPMLimit || projected[0].ConcurrencyLimit != consumedPolicy.ConcurrencyLimit {
		t.Fatalf("model execution projection=%+v", projected[0].limits())
	}
	if projected[1].RPSLimit != canonicalOnly.RPSLimit || projected[1].RPMLimit != canonicalOnly.RPMLimit {
		t.Fatalf("unconsumed canonical rate was projected out: %+v", projected[1].limits())
	}
	if !admissionPhaseIncludesTokens(AdmissionPhaseModelExecution) ||
		admissionPhaseIncludesRate(AdmissionPhaseModelExecution) {
		t.Fatal("model execution phase must retain tokens and exclude rate")
	}
}

func TestModelExecutionRejectsSameBucketRateContractDrift(t *testing.T) {
	t.Parallel()
	orgID := int64(7)
	base := Policy{
		Ref: "policy:contract", Name: "contract", Version: 1, OrgID: &orgID,
		Mode: ModeEnforce, FailureMode: FailureFailClosed, RPSLimit: 10, RPMLimit: 100,
		RuntimeBucket: "runtime:stable",
	}
	contract := rateAdmissionContractForPolicy(base, base.RuntimeBucket, rateAdmissionKindRedis)
	for _, test := range []struct {
		name   string
		mutate func(*Policy)
	}{
		{name: "policy ref", mutate: func(value *Policy) { value.Ref += "-other" }},
		{name: "version", mutate: func(value *Policy) { value.Version++ }},
		{name: "selector", mutate: func(value *Policy) { model := "other-model"; value.Model = model }},
		{name: "rps", mutate: func(value *Policy) { value.RPSLimit++ }},
		{name: "rpm", mutate: func(value *Policy) { value.RPMLimit++ }},
		{name: "mode", mutate: func(value *Policy) { value.Mode = ModeShadow }},
		{name: "failure mode", mutate: func(value *Policy) { value.FailureMode = FailureLastKnownSafe }},
		{name: "builtin", mutate: func(value *Policy) { value.Builtin = true }},
	} {
		t.Run(test.name, func(t *testing.T) {
			current := base
			test.mutate(&current)
			if projected, valid := projectPoliciesForAdmission(
				[]Policy{current}, AdmissionPhaseModelExecution, []rateAdmissionContract{contract},
			); valid || projected != nil {
				t.Fatalf("same-bucket %s drift was accepted: %+v", test.name, projected)
			}
		})
	}
}

func TestModelExecutionRetainsBuiltinZeroRateForReceiptDrift(t *testing.T) {
	t.Parallel()
	service := &Service{}
	findRuntime := func(t *testing.T, policies []Policy, runtimeRef string) Policy {
		t.Helper()
		for _, policy := range policies {
			if policy.RuntimeBucket == runtimeRef {
				return policy
			}
		}
		t.Fatalf("runtime policy %q was omitted", runtimeRef)
		return Policy{}
	}

	t.Run("credential positive to explicit deny", func(t *testing.T) {
		earlySubject := Subject{
			OrgID: 7, ProjectID: 11, APIKeyID: 13,
			AccessCredentialID: 17, CredentialRef: "credential-family-17",
			BaseRPSLimit: 20, BaseRPMLimit: 200, Model: "canonical-model",
		}
		const runtimeRef = "builtin:access-credential:credential-family-17"
		earlyPolicy := findRuntime(t, service.matchingPoliciesForModels(earlySubject, nil, false), runtimeRef)
		contract := rateAdmissionContractForPolicy(earlyPolicy, runtimeRef, rateAdmissionKindRedis)

		currentSubject := earlySubject
		currentSubject.BaseRPSLimit = 0
		currentSubject.BaseRPMLimit = 0
		current := service.matchingPoliciesForModels(currentSubject, []string{"public-alias"}, true)
		if projected, valid := projectPoliciesForAdmission(current, AdmissionPhaseModelExecution, []rateAdmissionContract{contract}); valid || projected != nil {
			t.Fatalf("credential explicit deny drift was accepted: %+v", projected)
		}
	})

	t.Run("grant positive to explicit deny", func(t *testing.T) {
		rps, rpm := int64(20), int64(200)
		earlySubject := Subject{
			OrgID: 7, ProjectID: 11, APIKeyID: 13, AccessGrantID: 19,
			BaseRPSLimit: 100, BaseRPMLimit: 1_000,
			GrantRPSLimit: &rps, GrantRPMLimit: &rpm, Model: "canonical-model",
		}
		const runtimeRef = "builtin:access-grant:19"
		earlyPolicy := findRuntime(t, service.matchingPoliciesForModels(earlySubject, nil, false), runtimeRef)
		contract := rateAdmissionContractForPolicy(earlyPolicy, runtimeRef, rateAdmissionKindRedis)

		zeroRPS, zeroRPM := int64(0), int64(0)
		currentSubject := earlySubject
		currentSubject.GrantRPSLimit = &zeroRPS
		currentSubject.GrantRPMLimit = &zeroRPM
		current := service.matchingPoliciesForModels(currentSubject, []string{"public-alias"}, true)
		if projected, valid := projectPoliciesForAdmission(current, AdmissionPhaseModelExecution, []rateAdmissionContract{contract}); valid || projected != nil {
			t.Fatalf("grant explicit deny drift was accepted: %+v", projected)
		}
	})
}

func TestUnscopedRateExcludesModelSelectorsWithoutSentinel(t *testing.T) {
	t.Parallel()
	orgID := int64(7)
	global := Policy{
		Ref: "policy:org-rate", Name: "organization rate", Version: 1, OrgID: &orgID,
		Mode: ModeEnforce, FailureMode: FailureFailClosed,
		RPSLimit: 2, RPMLimit: 20, TPMLimit: 2_000, ConcurrencyLimit: 3,
	}
	modelSpecific := global
	modelSpecific.Ref = "policy:model-rate"
	modelSpecific.Model = "a-real-public-model"
	subject := Subject{OrgID: orgID, ProjectID: 11, APIKeyID: 13, Model: ""}
	if !global.Matches(subject) || modelSpecific.Matches(subject) {
		t.Fatal("empty unscoped model did not exclude model-specific selector")
	}
	service := &Service{}
	service.snapshot.Store(buildPolicySnapshot([]Policy{global, modelSpecific}, nil, false, time.Now()))
	for _, matched := range service.matchingPoliciesForModels(subject, nil, false) {
		if matched.Ref == modelSpecific.Ref {
			t.Fatal("unscoped matcher selected a caller-model policy")
		}
	}
	projected, valid := projectPoliciesForAdmission([]Policy{global}, AdmissionPhaseUnscopedRate, nil)
	if !valid {
		t.Fatal("unscoped rate projection was rejected")
	}
	if len(projected) != 1 || projected[0].RPSLimit != global.RPSLimit || projected[0].RPMLimit != global.RPMLimit ||
		projected[0].TPMLimit != 0 || projected[0].ConcurrencyLimit != 0 {
		t.Fatalf("unscoped rate projection=%+v", projected)
	}
	if !admissionPhaseIncludesRate(AdmissionPhaseUnscopedRate) || admissionPhaseIncludesTokens(AdmissionPhaseUnscopedRate) {
		t.Fatal("unscoped rate phase must retain rate and exclude token/concurrency execution")
	}
}

func TestAuthorizedAliasAndCanonicalSelectorsUnionExactlyOnce(t *testing.T) {
	t.Parallel()
	orgID := int64(7)
	aliasPolicy := Policy{
		Ref: "policy:public-alias", Name: "alias", Version: 1, OrgID: &orgID,
		Model: "public-alias", Mode: ModeEnforce, FailureMode: FailureFailClosed, TPMLimit: 1_000,
		RuntimeBucket: "runtime:alias",
	}
	canonicalPolicy := Policy{
		Ref: "policy:canonical", Name: "canonical", Version: 1, OrgID: &orgID,
		Model: "canonical-model", Mode: ModeEnforce, FailureMode: FailureFailClosed, ConcurrencyLimit: 2,
		RuntimeBucket: "runtime:canonical",
	}
	service := &Service{}
	service.snapshot.Store(buildPolicySnapshot([]Policy{aliasPolicy, canonicalPolicy}, nil, false, time.Now()))
	matched := service.matchingPoliciesForModels(Subject{
		OrgID: orgID, ProjectID: 11, APIKeyID: 13, Model: "canonical-model",
		BaseRPSLimit: 100, BaseRPMLimit: 1_000,
	}, []string{"public-alias"}, true)
	counts := make(map[string]int)
	for _, policy := range matched {
		counts[policy.Ref]++
	}
	if counts[aliasPolicy.Ref] != 1 || counts[canonicalPolicy.Ref] != 1 {
		t.Fatalf("alias/canonical selector union was not exact: %#v", counts)
	}
}

func TestUnscopedRateFingerprintDoesNotPersistCallerModel(t *testing.T) {
	t.Parallel()
	request := AdmissionRequest{
		OperationID: "op_neutral_fingerprint:rate",
		Subject: Subject{
			OrgID: 7, ProjectID: 11, APIKeyID: 13,
			BaseRPSLimit: 100, BaseRPMLimit: 1_000,
		},
		RequestedModel: "private-model-a",
		Phase:          AdmissionPhaseUnscopedRate,
	}
	first := operationFingerprint(request, "builtin:api-key-rate:13")
	request.RequestedModel = "private-model-b"
	second := operationFingerprint(request, "builtin:api-key-rate:13")
	if first != second {
		t.Fatal("unscoped operation state retained a caller-model-derived fingerprint")
	}
}

func TestRateAdmissionReceiptTrustBoundary(t *testing.T) {
	t.Parallel()
	service := &Service{nodeID: "receipt-node-a"}
	otherService := &Service{nodeID: "receipt-node-b"}
	const mainOperationID = "op_receipt_binding"
	rateOperationID, valid := RateAdmissionOperationID(mainOperationID)
	if !valid {
		t.Fatal("invalid test operation identity")
	}
	subject := Subject{
		OrgID: 7, ProjectID: 11, APIKeyID: 13,
		AccessCredentialID: 17, CredentialRef: "credential-family-17",
		AccessGrantID: 19, Model: "canonical-model",
	}
	policy := Policy{
		Ref: "policy:shared", Name: "shared", Version: 3,
		Mode: ModeEnforce, FailureMode: FailureFailClosed, RPSLimit: 7, RPMLimit: 70,
	}
	contract := rateAdmissionContractForPolicy(policy, "runtime:shared", rateAdmissionKindRedis)
	receipt := service.newRateAdmissionReceipt(rateOperationID, "public-alias", subject, []rateAdmissionContract{contract})
	if receipt.isZero() {
		t.Fatal("service did not mint a receipt")
	}

	validRequest := AdmissionRequest{
		OperationID: mainOperationID, Subject: subject,
		RequestedModel: "public-alias",
		EstimatedInput: 1, ReservedOutput: 1,
		Phase: AdmissionPhaseModelExecution, RateReceipt: receipt,
	}
	if !service.validateRateAdmissionReceipt(&validRequest) {
		t.Fatal("valid service-authenticated receipt was rejected")
	}

	cloneReceipt := func(source RateAdmissionReceipt) RateAdmissionReceipt {
		clone := source
		clone.contracts = append([]rateAdmissionContract(nil), source.contracts...)
		clone.authenticator = append([]byte(nil), source.authenticator...)
		return clone
	}
	requestWith := func(candidate RateAdmissionReceipt) AdmissionRequest {
		request := validRequest
		request.RateReceipt = candidate
		return request
	}

	zero := requestWith(RateAdmissionReceipt{})
	if service.validateRateAdmissionReceipt(&zero) {
		t.Fatal("zero receipt was accepted")
	}
	forgedReceipt := cloneReceipt(receipt)
	forgedReceipt.authenticator = make([]byte, sha256.Size)
	forged := requestWith(forgedReceipt)
	if service.validateRateAdmissionReceipt(&forged) {
		t.Fatal("manually constructed receipt was accepted")
	}
	otherOperation := requestWith(cloneReceipt(receipt))
	otherOperation.OperationID = "op_receipt_other"
	if service.validateRateAdmissionReceipt(&otherOperation) {
		t.Fatal("receipt crossed main operation identity")
	}
	tamperedRateOperation := cloneReceipt(receipt)
	tamperedRateOperation.rateOperationID = "op_receipt_other:rate"
	tamperedOperationRequest := requestWith(tamperedRateOperation)
	if service.validateRateAdmissionReceipt(&tamperedOperationRequest) {
		t.Fatal("receipt crossed rate operation identity")
	}

	for _, test := range []struct {
		name   string
		mutate func(*Subject)
	}{
		{name: "organization", mutate: func(candidate *Subject) { candidate.OrgID++ }},
		{name: "project", mutate: func(candidate *Subject) { candidate.ProjectID++ }},
		{name: "api key", mutate: func(candidate *Subject) { candidate.APIKeyID++ }},
		{name: "credential", mutate: func(candidate *Subject) { candidate.AccessCredentialID++ }},
		{name: "credential ref", mutate: func(candidate *Subject) { candidate.CredentialRef += "-other" }},
		{name: "grant", mutate: func(candidate *Subject) { candidate.AccessGrantID++ }},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := requestWith(cloneReceipt(receipt))
			test.mutate(&request.Subject)
			if service.validateRateAdmissionReceipt(&request) {
				t.Fatalf("receipt crossed %s identity", test.name)
			}
		})
	}

	otherNode := requestWith(cloneReceipt(receipt))
	if otherService.validateRateAdmissionReceipt(&otherNode) {
		t.Fatal("receipt crossed service authenticator identity")
	}
	tamperedContracts := cloneReceipt(receipt)
	tamperedContracts.contracts = append(tamperedContracts.contracts, rateAdmissionContract{
		runtimeRef: "runtime:canonical-only", policyRef: "policy:canonical-only", version: 1,
		selectorHash: "selector", rpsLimit: 1, mode: ModeEnforce, failureMode: FailureFailClosed,
		kind: rateAdmissionKindRedis,
	})
	tamperedContractsRequest := requestWith(tamperedContracts)
	if service.validateRateAdmissionReceipt(&tamperedContractsRequest) {
		t.Fatal("receipt accepted a caller-added rate bucket")
	}
	for _, test := range []struct {
		name   string
		mutate func(*rateAdmissionContract)
	}{
		{name: "runtime ref", mutate: func(value *rateAdmissionContract) { value.runtimeRef += "-other" }},
		{name: "policy ref", mutate: func(value *rateAdmissionContract) { value.policyRef += "-other" }},
		{name: "version", mutate: func(value *rateAdmissionContract) { value.version++ }},
		{name: "selector", mutate: func(value *rateAdmissionContract) { value.selectorHash += "a" }},
		{name: "rps", mutate: func(value *rateAdmissionContract) { value.rpsLimit++ }},
		{name: "rpm", mutate: func(value *rateAdmissionContract) { value.rpmLimit++ }},
		{name: "mode", mutate: func(value *rateAdmissionContract) { value.mode = ModeShadow }},
		{name: "failure mode", mutate: func(value *rateAdmissionContract) { value.failureMode = FailureLastKnownSafe }},
		{name: "builtin", mutate: func(value *rateAdmissionContract) { value.builtin = !value.builtin }},
		{name: "admission kind", mutate: func(value *rateAdmissionContract) { value.kind = rateAdmissionKindFallback }},
	} {
		t.Run("contract "+test.name, func(t *testing.T) {
			candidate := cloneReceipt(receipt)
			test.mutate(&candidate.contracts[0])
			request := requestWith(candidate)
			if service.validateRateAdmissionReceipt(&request) {
				t.Fatalf("receipt accepted tampered %s", test.name)
			}
		})
	}
	tamperedRequestedModel := requestWith(cloneReceipt(receipt))
	tamperedRequestedModel.RequestedModel = "other-public-model"
	if service.validateRateAdmissionReceipt(&tamperedRequestedModel) {
		t.Fatal("receipt crossed requested public model identity")
	}
	nonModelExecution := requestWith(cloneReceipt(receipt))
	nonModelExecution.Phase = AdmissionPhaseFull
	if service.validateRateAdmissionReceipt(&nonModelExecution) {
		t.Fatal("receipt was accepted outside model execution")
	}
}

func TestRateAdmissionReceiptIssuedOnlyAfterMatchingFinalize(t *testing.T) {
	t.Parallel()
	service := &Service{nodeID: "receipt-finalize-node"}
	const mainOperationID = "op_receipt_finalize"
	rateOperationID, valid := RateAdmissionOperationID(mainOperationID)
	if !valid {
		t.Fatal("invalid test operation identity")
	}
	subject := Subject{OrgID: 7, ProjectID: 11, APIKeyID: 13, Model: "canonical-model"}
	policy := Policy{Ref: "policy:shared", Name: "shared", Version: 1, Mode: ModeEnforce, FailureMode: FailureFailClosed, RPSLimit: 1}
	reservation := &Reservation{
		OperationID: rateOperationID,
		rateReceipt: service.newRateAdmissionReceipt(rateOperationID, "public-alias", subject,
			[]rateAdmissionContract{rateAdmissionContractForPolicy(policy, "runtime:shared", rateAdmissionKindRedis)}),
	}
	if _, issued := reservation.rateAdmissionReceiptFor(mainOperationID); issued {
		t.Fatal("receipt was issued before finalization")
	}
	if _, err := service.FinalizeRateAdmission(context.Background(), reservation, "op_receipt_other"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("cross-operation finalize error=%v", err)
	}
	if reservation.rateFinalized {
		t.Fatal("failed finalize authorized the receipt")
	}
	receipt, err := service.FinalizeRateAdmission(context.Background(), reservation, mainOperationID)
	if err != nil || receipt.isZero() {
		t.Fatalf("matching finalize receipt=%+v err=%v", receipt, err)
	}
	receipt.contracts[0].runtimeRef = "runtime:caller-mutation"
	receipt.authenticator[0] ^= 0xff
	second, issued := reservation.rateAdmissionReceiptFor(mainOperationID)
	if !issued || len(second.contracts) != 1 || second.contracts[0].runtimeRef != "runtime:shared" ||
		hmac.Equal(second.authenticator, receipt.authenticator) {
		t.Fatal("issued receipt did not use defensive copies")
	}
}

func TestSplitGatewayAdmissionCountsRateOnceAndRetainsExecutionLimits(t *testing.T) {
	service, _ := newPolicyTestService(t, testOptions())
	subject := Subject{
		OrgID: 7, ProjectID: 11, APIKeyID: 13, Model: "public-chat",
		BaseRPSLimit: 1, BaseRPMLimit: 100,
	}
	const operationID = "op_split_gateway"
	rateOperationID, valid := RateAdmissionOperationID(operationID)
	if !valid {
		t.Fatal("invalid test operation identity")
	}
	earlySubject := subject
	earlySubject.Model = ""
	rate, err := service.Reserve(context.Background(), AdmissionRequest{
		OperationID: rateOperationID, Subject: earlySubject,
		RequestedModel: subject.Model, Phase: AdmissionPhaseUnscopedRate,
	})
	if err != nil {
		t.Fatalf("early rate admission: %v", err)
	}
	receipt, err := service.FinalizeRateAdmission(context.Background(), rate, operationID)
	if err != nil {
		t.Fatalf("finalize early rate admission: %v", err)
	}

	execution, err := service.Reserve(context.Background(), AdmissionRequest{
		OperationID: operationID, Subject: subject,
		RequestedModel: subject.Model,
		EstimatedInput: 10, ReservedOutput: 10, LeaseTTL: time.Minute,
		Phase: AdmissionPhaseModelExecution, RateReceipt: receipt,
	})
	if err != nil {
		t.Fatalf("model execution double-counted rate or conflicted: %v", err)
	}
	secondRate := AdmissionRequest{
		OperationID: "op_split_gateway_second:rate", Subject: earlySubject,
		RequestedModel: subject.Model, Phase: AdmissionPhaseUnscopedRate,
	}
	if _, err := service.Reserve(context.Background(), secondRate); !errors.Is(err, ErrRPSExceeded) {
		t.Fatalf("next HTTP attempt did not observe consumed RPS: %v", err)
	}
	if err := service.CancelBeforeDispatch(context.Background(), execution); err != nil {
		t.Fatalf("release model execution admission: %v", err)
	}
}

func TestAliasCannotBypassCanonicalModelRatePolicy(t *testing.T) {
	canonicalModel := "canonical-chat"
	service, _ := newPolicyTestService(t, testOptions(), Policy{
		ID: 77, Ref: "policy:canonical-chat-rate", Name: "canonical chat rate", Version: 1,
		Model: canonicalModel, Mode: ModeEnforce, FailureMode: FailureFailClosed, RPSLimit: 1,
	})
	aliasSubject := Subject{
		OrgID: 7, ProjectID: 11, APIKeyID: 13, Model: "public-chat-alias",
		BaseRPSLimit: 2, BaseRPMLimit: 100,
	}

	var firstExecution *Reservation
	for attempt := 1; attempt <= 2; attempt++ {
		operationID := fmt.Sprintf("op_alias_canonical_%d", attempt)
		earlySubject := aliasSubject
		earlySubject.Model = ""
		rate, err := service.Reserve(context.Background(), AdmissionRequest{
			OperationID: operationID + ":rate", Subject: earlySubject,
			RequestedModel: aliasSubject.Model, Phase: AdmissionPhaseUnscopedRate,
		})
		if err != nil {
			t.Fatalf("attempt %d early alias rate: %v", attempt, err)
		}
		receipt, err := service.FinalizeRateAdmission(context.Background(), rate, operationID)
		if err != nil {
			t.Fatalf("attempt %d finalize early rate: %v", attempt, err)
		}
		canonicalSubject := aliasSubject
		canonicalSubject.Model = canonicalModel
		execution, err := service.Reserve(context.Background(), AdmissionRequest{
			OperationID: operationID, Subject: canonicalSubject,
			RequestedModel: aliasSubject.Model,
			EstimatedInput: 10, ReservedOutput: 10, LeaseTTL: time.Minute,
			Phase: AdmissionPhaseModelExecution, RateReceipt: receipt,
		})
		if attempt == 1 {
			if err != nil {
				t.Fatalf("first canonical execution double-counted shared rate: %v", err)
			}
			firstExecution = execution
			if err := service.CancelBeforeDispatch(context.Background(), firstExecution); err != nil {
				t.Fatalf("release first canonical execution: %v", err)
			}
			continue
		}
		if !errors.Is(err, ErrRPSExceeded) || execution != nil {
			t.Fatalf("canonical model-specific rate was bypassed through alias: reservation=%+v err=%v", execution, err)
		}
	}
}

func TestUnscopedGatewayRateNeverConsumesRequestedModelPolicy(t *testing.T) {
	modelPolicy := Policy{
		ID: 78, Ref: "policy:unauthorized-model", Name: "unauthorized model", Version: 1,
		Model: "private-model", Mode: ModeEnforce, FailureMode: FailureFailClosed, RPSLimit: 1,
	}
	service, _ := newPolicyTestService(t, testOptions(), modelPolicy)
	subject := Subject{OrgID: 7, ProjectID: 11, APIKeyID: 13, BaseRPSLimit: 100, BaseRPMLimit: 1_000}
	for attempt := 1; attempt <= 2; attempt++ {
		operationID := fmt.Sprintf("op_unauthorized_neutral_%d", attempt)
		rate, err := service.Reserve(context.Background(), AdmissionRequest{
			OperationID: operationID + ":rate", Subject: subject,
			RequestedModel: "private-model", Phase: AdmissionPhaseUnscopedRate,
		})
		if err != nil {
			t.Fatalf("attempt %d touched model policy during neutral admission: %v", attempt, err)
		}
		if _, err := service.FinalizeRateAdmission(context.Background(), rate, operationID); err != nil {
			t.Fatalf("attempt %d finalize neutral admission: %v", attempt, err)
		}
	}
	count, err := service.rdb.ZCard(context.Background(), runtimeKeys(modelPolicy.Ref)[0]).Result()
	if err != nil || count != 0 {
		t.Fatalf("unauthorized model RPS population=%d err=%v, want untouched", count, err)
	}
}

func TestAuthorizedAliasEnforcesEveryLimitDimension(t *testing.T) {
	tests := []struct {
		name      string
		policy    Policy
		wantErr   error
		denyFirst bool
		holdFirst bool
	}{
		{name: "rps", policy: Policy{RPSLimit: 1}, wantErr: ErrRPSExceeded},
		{name: "rpm", policy: Policy{RPMLimit: 1}, wantErr: ErrRPMExceeded},
		{name: "tpm", policy: Policy{TPMLimit: 1}, wantErr: ErrTPMExceeded, denyFirst: true},
		{name: "concurrency", policy: Policy{ConcurrencyLimit: 1}, wantErr: ErrConcurrencyExceeded, holdFirst: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy := test.policy
			policy.ID = 80
			policy.Ref = "policy:alias-" + test.name
			policy.Name = "alias " + test.name
			policy.Version = 1
			policy.Model = "public-alias"
			policy.Mode = ModeEnforce
			policy.FailureMode = FailureFailClosed
			service, _ := newPolicyTestService(t, testOptions(), policy)

			first, firstErr := reserveSplitGatewayModel(t, service, "op_alias_"+test.name+"_first", "public-alias", "canonical-model")
			if test.denyFirst {
				if !errors.Is(firstErr, test.wantErr) || first != nil {
					t.Fatalf("first alias admission reservation=%+v err=%v, want %v", first, firstErr, test.wantErr)
				}
				return
			}
			if firstErr != nil || first == nil {
				t.Fatalf("first alias admission reservation=%+v err=%v", first, firstErr)
			}
			if !test.holdFirst {
				if err := service.CancelBeforeDispatch(context.Background(), first); err != nil {
					t.Fatalf("release first alias admission: %v", err)
				}
			}

			second, secondErr := reserveSplitGatewayModel(t, service, "op_alias_"+test.name+"_second", "public-alias", "canonical-model")
			if !errors.Is(secondErr, test.wantErr) || second != nil {
				t.Fatalf("second alias admission reservation=%+v err=%v, want %v", second, secondErr, test.wantErr)
			}
			if test.holdFirst {
				if err := service.CancelBeforeDispatch(context.Background(), first); err != nil {
					t.Fatalf("release held alias admission: %v", err)
				}
			}
		})
	}
}

func TestModelExecutionUnionsAliasAndCanonicalPoliciesOnce(t *testing.T) {
	aliasPolicy := Policy{
		ID: 84, Ref: "policy:alias-tpm", Name: "alias tpm", Version: 1,
		Model: "public-alias", Mode: ModeEnforce, FailureMode: FailureFailClosed, TPMLimit: 100,
	}
	canonicalPolicy := Policy{
		ID: 85, Ref: "policy:canonical-concurrency", Name: "canonical concurrency", Version: 1,
		Model: "canonical-model", Mode: ModeEnforce, FailureMode: FailureFailClosed, ConcurrencyLimit: 2,
	}
	service, _ := newPolicyTestService(t, testOptions(), aliasPolicy, canonicalPolicy)
	reservation, err := reserveSplitGatewayModel(t, service, "op_alias_canonical_union", "public-alias", "canonical-model")
	if err != nil || reservation == nil {
		t.Fatalf("union admission reservation=%+v err=%v", reservation, err)
	}
	counts := make(map[string]int)
	for _, item := range reservation.Items {
		counts[item.PolicyRef]++
	}
	if counts[aliasPolicy.Ref] != 1 || counts[canonicalPolicy.Ref] != 1 {
		t.Fatalf("alias/canonical policy union was not exact: %#v", counts)
	}
	if err := service.CancelBeforeDispatch(context.Background(), reservation); err != nil {
		t.Fatal(err)
	}
}

func TestRateReceiptPolicyDriftFailsBeforeAnySecondReservation(t *testing.T) {
	orgID := int64(7)
	runtimeBucket := "policy:drift:selector:stable"
	firstPolicy := Policy{
		ID: 86, Ref: "policy:drift", Name: "drift", Version: 1, OrgID: &orgID,
		Mode: ModeShadow, FailureMode: FailureLastKnownSafe, RPSLimit: 100, RuntimeBucket: runtimeBucket,
	}
	service, _ := newPolicyTestService(t, testOptions(), firstPolicy)
	subject := Subject{OrgID: orgID, ProjectID: 11, APIKeyID: 13, BaseRPSLimit: 100, BaseRPMLimit: 1_000}
	const operationID = "op_rate_contract_drift"
	rate, err := service.Reserve(context.Background(), AdmissionRequest{
		OperationID: operationID + ":rate", Subject: subject,
		RequestedModel: "public-alias", Phase: AdmissionPhaseUnscopedRate,
	})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := service.FinalizeRateAdmission(context.Background(), rate, operationID)
	if err != nil {
		t.Fatal(err)
	}

	currentPolicy := firstPolicy
	currentPolicy.Version = 2
	currentPolicy.Mode = ModeEnforce
	currentPolicy.FailureMode = FailureFailClosed
	currentPolicy.RPSLimit = 1
	service.snapshot.Store(buildPolicySnapshot([]Policy{currentPolicy}, nil, false, time.Now()))
	lateSubject := subject
	lateSubject.Model = "canonical-model"
	reservation, err := service.Reserve(context.Background(), AdmissionRequest{
		OperationID: operationID, Subject: lateSubject, RequestedModel: "public-alias",
		EstimatedInput: 1, ReservedOutput: 1, LeaseTTL: time.Minute,
		Phase: AdmissionPhaseModelExecution, RateReceipt: receipt,
	})
	if !errors.Is(err, ErrStateUnavailable) || reservation != nil {
		t.Fatalf("drift reservation=%+v err=%v, want fail-closed", reservation, err)
	}
	count, countErr := service.rdb.ZCard(context.Background(), runtimeKeys(runtimeBucket)[0]).Result()
	if countErr != nil || count != 1 {
		t.Fatalf("same-bucket drift RPS population=%d err=%v, want one early member", count, countErr)
	}
	exists, existsErr := service.rdb.Exists(context.Background(), operationRuntimeKeys(runtimeBucket, operationID)[6]).Result()
	if existsErr != nil || exists != 0 {
		t.Fatalf("drift created a second operation reservation: exists=%d err=%v", exists, existsErr)
	}
}

func reserveSplitGatewayModel(t *testing.T, service *Service, operationID, requestedModel, canonicalModel string) (*Reservation, error) {
	t.Helper()
	earlySubject := Subject{
		OrgID: 7, ProjectID: 11, APIKeyID: 13,
		BaseRPSLimit: 100, BaseRPMLimit: 1_000,
	}
	rate, err := service.Reserve(context.Background(), AdmissionRequest{
		OperationID: operationID + ":rate", Subject: earlySubject,
		RequestedModel: requestedModel, Phase: AdmissionPhaseUnscopedRate,
	})
	if err != nil {
		t.Fatalf("early neutral admission: %v", err)
	}
	receipt, err := service.FinalizeRateAdmission(context.Background(), rate, operationID)
	if err != nil {
		t.Fatalf("finalize early neutral admission: %v", err)
	}
	lateSubject := earlySubject
	lateSubject.Model = canonicalModel
	return service.Reserve(context.Background(), AdmissionRequest{
		OperationID: operationID, Subject: lateSubject, RequestedModel: requestedModel,
		EstimatedInput: 1, ReservedOutput: 1, LeaseTTL: time.Minute,
		Phase: AdmissionPhaseModelExecution, RateReceipt: receipt,
	})
}

func TestDecisionCollectorComposesSplitAdmissionVersions(t *testing.T) {
	t.Parallel()
	ctx, collector := ContextWithDecisionCollector(context.Background())
	recordDecision(ctx, Decision{PolicyRef: "rate"}, "rate-version")
	recordDecision(ctx, Decision{PolicyRef: "rate-second"}, "rate-version")
	_, version := collector.Snapshot()
	if version != "rate-version" {
		t.Fatalf("single phase version=%q", version)
	}
	recordDecision(ctx, Decision{PolicyRef: "execution"}, "execution-version")
	_, combined := collector.Snapshot()
	if combined == "" || combined == "rate-version" || combined == "execution-version" {
		t.Fatalf("split phase version was not composed: %q", combined)
	}
	if got := composedDecisionVersion(map[string]struct{}{
		"execution-version": {}, "rate-version": {},
	}); got != combined {
		t.Fatalf("composed version is not order invariant: %q vs %q", got, combined)
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
