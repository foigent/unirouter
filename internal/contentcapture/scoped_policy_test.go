package contentcapture

import (
	"errors"
	"testing"
)

func scopedPolicyTestInput() ScopedPolicyInput {
	return ScopedPolicyInput{
		Deployment:         testMetadataPolicy(),
		KeyType:            "shared",
		FullContentEnabled: true,
		StorageReady:       true,
		SupportedProtocols: []string{"openai.chat"},
		Limits:             testPolicyView().Limits,
	}
}

func userException(userID int64, config PolicyConfig) *UserPolicyException {
	return &UserPolicyException{
		UserID:             userID,
		Mode:               config.Mode,
		SupportedProtocols: config.SupportedProtocols,
		ObservationPoints:  config.ObservationPoints,
		RetentionSeconds:   config.RetentionSeconds,
		MaxSavedBytes:      config.MaxSavedBytes,
		AllowedPurposes:    config.AllowedPurposes,
		OnLimit:            config.OnLimit,
	}
}

func TestResolveScopedEffectiveMostSpecificException(t *testing.T) {
	input := scopedPolicyTestInput()
	enterprise := testFullPolicy()
	enterprise.RetentionSeconds = policyNumber(30)
	enterprise.QuotaBytes = policyNumber(500)
	project := testFullPolicy()
	project.RetentionSeconds = policyNumber(90)
	project.QuotaBytes = policyNumber(800)
	input.Enterprise = &enterprise

	got, err := ResolveScopedEffective(input)
	if err != nil || got.Scope != PolicyScopeEnterprise || *got.Config.QuotaBytes != 500 {
		t.Fatalf("enterprise default: %#v, %v", got, err)
	}
	input.Project = &project
	input.ProjectQuotaBytes = 900
	got, err = ResolveScopedEffective(input)
	if err != nil || got.Scope != PolicyScopeProject || *got.Config.QuotaBytes != 900 || *got.Config.RetentionSeconds != 90 {
		t.Fatalf("project exception should replace defaults: %#v, %v", got, err)
	}
	*got.Config.QuotaBytes = 1
	if *project.QuotaBytes != 800 {
		t.Fatal("result aliases stored project policy")
	}

	enterprise.Mode = ModeDisabled
	enterprise.RetentionSeconds, enterprise.QuotaBytes, enterprise.MaxSavedBytes = nil, nil, nil
	enterprise.AllowedPurposes = []string{}
	enterprise.OnLimit = ""
	got, err = ResolveScopedEffective(input)
	if err != nil || got.Scope != PolicyScopeProject || got.Config.Mode != ModeFullContent {
		t.Fatalf("enterprise default disabled blocked explicit project: %#v, %v", got, err)
	}
}

func TestResolveScopedEffectiveExecutionStorageFollowsSelectedLayer(t *testing.T) {
	input := scopedPolicyTestInput()
	input.DeploymentExecutionStorageMode = ExecutionStorageStrict
	enterprise := testMetadataPolicy()
	input.Enterprise = &enterprise
	input.EnterpriseExecutionStorageMode = ExecutionStorageTemporary
	got, err := ResolveScopedEffective(input)
	if err != nil || got.Scope != PolicyScopeEnterprise || got.ExecutionStorageMode != ExecutionStorageTemporary {
		t.Fatalf("enterprise execution storage: %#v, %v", got, err)
	}
	project := testMetadataPolicy()
	input.Project = &project
	input.ProjectExecutionStorageMode = ExecutionStorageStrict
	got, err = ResolveScopedEffective(input)
	if err != nil || got.Scope != PolicyScopeProject || got.ExecutionStorageMode != ExecutionStorageStrict {
		t.Fatalf("project strict override: %#v, %v", got, err)
	}
	input.User = userException(42, testFullPolicy())
	input.KeyType, input.OwnerUserID, input.ProjectQuotaBytes = "personal", 42, 800
	input.User.ExecutionStorageMode = ExecutionStorageTemporary
	got, err = ResolveScopedEffective(input)
	if err != nil || got.Scope != PolicyScopeUser || got.ExecutionStorageMode != ExecutionStorageTemporary {
		t.Fatalf("personal execution storage: %#v, %v", got, err)
	}
	input.User.ExecutionStorageMode = ExecutionStorageStrict
	if _, err := ResolveScopedEffective(input); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("full review with strict persistence accepted: %v", err)
	}
	input.User.ExecutionStorageMode = "unexpected"
	if _, err := ResolveScopedEffective(input); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("unsupported execution storage accepted: %v", err)
	}
}

func TestResolveScopedEffectivePersonalExceptionUsesSharedProjectQuota(t *testing.T) {
	input := scopedPolicyTestInput()
	project := testMetadataPolicy()
	project.Mode = ModeDisabled
	input.Project = &project
	input.User = userException(42, testFullPolicy())
	input.KeyType = "personal"
	input.OwnerUserID = 42
	input.ProjectQuotaBytes = 800

	got, err := ResolveScopedEffective(input)
	if err != nil || got.Scope != PolicyScopeUser || got.Config.Mode != ModeFullContent || *got.Config.QuotaBytes != 800 {
		t.Fatalf("personal exception: %#v, %v", got, err)
	}
	input.KeyType = "shared"
	got, err = ResolveScopedEffective(input)
	if err != nil || got.Scope != PolicyScopeProject || got.Config.Mode != ModeDisabled {
		t.Fatalf("shared key must not use personal exception: %#v, %v", got, err)
	}
	input.KeyType = "service"
	got, err = ResolveScopedEffective(input)
	if err != nil || got.Scope != PolicyScopeProject || got.Config.Mode != ModeDisabled {
		t.Fatalf("service key must not use personal exception: %#v, %v", got, err)
	}
	input.KeyType = "personal"
	input.OwnerUserID = 43
	if _, err := ResolveScopedEffective(input); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("mismatched owner accepted: %v", err)
	}
	input.OwnerUserID = 42
	input.ProjectQuotaBytes = 0
	if _, err := ResolveScopedEffective(input); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("full exception without project quota accepted: %v", err)
	}
	input.ProjectQuotaBytes = 800
	input.OwnerUserID = 0
	if _, err := ResolveScopedEffective(input); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("personal key without authenticated owner accepted: %v", err)
	}
	input.KeyType = "unknown"
	if _, err := ResolveScopedEffective(input); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("unknown key type accepted: %v", err)
	}
}

func TestResolveScopedEffectiveDeploymentGates(t *testing.T) {
	for name, change := range map[string]func(*ScopedPolicyInput){
		"deployment disabled": func(in *ScopedPolicyInput) { in.Deployment.Mode = ModeDisabled },
		"feature disabled":    func(in *ScopedPolicyInput) { in.FullContentEnabled = false },
		"storage unavailable": func(in *ScopedPolicyInput) { in.StorageReady = false },
	} {
		t.Run(name, func(t *testing.T) {
			input := scopedPolicyTestInput()
			project := testFullPolicy()
			input.Project = &project
			change(&input)
			if _, err := ResolveScopedEffective(input); !errors.Is(err, ErrPolicyInactive) {
				t.Fatalf("physical gate was bypassed: %v", err)
			}
		})
	}
	input := scopedPolicyTestInput()
	project := testFullPolicy()
	project.MaxSavedBytes = policyNumber(input.Limits.MaxSavedBytes + 1)
	input.Project = &project
	if _, err := ResolveScopedEffective(input); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("deployment item limit was bypassed: %v", err)
	}
}
