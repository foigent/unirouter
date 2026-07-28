package completedfeed

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReferenceRuntimeConfigurationFailsClosed(t *testing.T) {
	projector := func(payload []byte, sourceInstance string) (ReferenceEnvelopeV1Result, error) {
		return BuildReferenceEnvelopeV1(payload, sourceInstance)
	}

	legacy, err := NewService(nil, "", "", false)
	require.NoError(t, err)
	require.Equal(t, ReferenceModeLegacy, legacy.ReferenceMode())

	_, err = NewService(nil, "", "", false, WithReferenceProjection("unknown", projector))
	require.ErrorContains(t, err, "reference mode")

	_, err = NewService(
		nil,
		"",
		"",
		false,
		WithReferenceProjection(
			string(ReferenceModeLegacy),
			projector,
			expectedReferenceRuntimeBindingV1,
		),
	)
	require.ErrorContains(t, err, "legacy")

	_, err = NewService(nil, "", "", false, WithReferenceProjection(string(ReferenceModeShadow), projector))
	require.ErrorContains(t, err, "binding")

	_, err = NewService(
		nil,
		"",
		"",
		false,
		WithReferenceProjection(
			string(ReferenceModeShadow),
			projector,
			expectedReferenceRuntimeBindingV1,
		),
	)
	require.ErrorContains(t, err, "requires an enabled")

	_, err = NewService(
		nil,
		"",
		"",
		false,
		WithReferenceProjection(
			string(ReferenceModeShadow),
			nil,
			expectedReferenceRuntimeBindingV1,
		),
	)
	require.ErrorContains(t, err, "projector")

	mismatchedBinding := expectedReferenceRuntimeBindingV1
	mismatchedBinding.ManifestDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	_, err = NewService(
		nil,
		"",
		"",
		false,
		WithReferenceProjection(
			string(ReferenceModeShadow),
			projector,
			mismatchedBinding,
		),
	)
	require.ErrorContains(t, err, "pinned contract")

	configured := &Service{}
	require.NoError(
		t,
		WithReferenceProjection(
			string(ReferenceModeNeutralCanary),
			projector,
			expectedReferenceRuntimeBindingV1,
		)(configured),
	)
	require.Equal(t, ReferenceModeNeutralCanary, configured.ReferenceMode())
	require.NotNil(t, configured.referenceProjector)
	require.Equal(t, expectedReferenceRuntimeBindingV1, configured.referenceBinding)
}

func TestReferenceProjectionComparisonClassifiesOnlyBoundedResults(t *testing.T) {
	_, payload, _, err := BuildSnapshot(SnapshotInput{
		RequestID:            "reference-runtime-comparison",
		OrgID:                7,
		ProjectID:            22,
		TerminalStatus:       TerminalSettled,
		TerminalAvailableAt:  time.Date(2026, 7, 28, 1, 2, 3, 0, time.UTC),
		Model:                "runtime-model",
		PromptTokens:         5,
		UncachedPromptTokens: 5,
		CompletionTokens:     1,
		UsageSource:          "provider",
		UsageAuthority:       "authoritative",
		UsagePurpose:         "billing",
		UsageFinality:        "final",
		UsageContractVersion: "usage.v1",
		CacheUsageSchema:     "cache.v1",
		CacheUsageStatus:     "not_used",
		CalculatedCostMicros: 10,
		ChargedCostMicros:    10,
		PricingVersion:       "pricing.v1",
	})
	require.NoError(t, err)
	legacy, err := BuildReferenceEnvelopeV1(payload, "runtime-source")
	require.NoError(t, err)

	testCases := []struct {
		name      string
		projector ReferenceProjector
		expected  string
	}{
		{
			name: "match",
			projector: func([]byte, string) (ReferenceEnvelopeV1Result, error) {
				return legacy, nil
			},
			expected: ReferenceProjectionMatch,
		},
		{
			name: "neutral error",
			projector: func([]byte, string) (ReferenceEnvelopeV1Result, error) {
				return ReferenceEnvelopeV1Result{}, errors.New("private cause must not escape")
			},
			expected: ReferenceProjectionNeutralError,
		},
		{
			name: "canonical mismatch",
			projector: func([]byte, string) (ReferenceEnvelopeV1Result, error) {
				result := legacy
				result.CanonicalBytes = append([]byte(nil), legacy.CanonicalBytes...)
				result.CanonicalBytes[0] ^= 1
				return result, nil
			},
			expected: ReferenceProjectionCanonicalMismatch,
		},
		{
			name: "envelope mismatch",
			projector: func([]byte, string) (ReferenceEnvelopeV1Result, error) {
				result := legacy
				result.EnvelopeBytes = append([]byte(nil), legacy.EnvelopeBytes...)
				result.EnvelopeBytes[0] ^= 1
				return result, nil
			},
			expected: ReferenceProjectionEnvelopeMismatch,
		},
		{
			name: "identity mismatch",
			projector: func([]byte, string) (ReferenceEnvelopeV1Result, error) {
				result := legacy
				result.IdempotencyKey = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
				return result, nil
			},
			expected: ReferenceProjectionIdentityMismatch,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			before := append([]byte(nil), payload...)
			service := &Service{
				sourceInstance:     "runtime-source",
				referenceMode:      ReferenceModeShadow,
				referenceProjector: testCase.projector,
				referenceBinding:   expectedReferenceRuntimeBindingV1,
			}
			require.Equal(t, testCase.expected, service.compareReferenceProjection(payload))
			require.True(t, bytes.Equal(before, payload))
		})
	}

	neutralCalls := 0
	service := &Service{
		sourceInstance: "runtime-source",
		referenceMode:  ReferenceModeShadow,
		referenceProjector: func([]byte, string) (ReferenceEnvelopeV1Result, error) {
			neutralCalls++
			return ReferenceEnvelopeV1Result{}, nil
		},
		referenceBinding: expectedReferenceRuntimeBindingV1,
	}
	require.Equal(
		t,
		ReferenceProjectionLegacyError,
		service.compareReferenceProjection([]byte(`{"invalid":true}`)),
	)
	require.Zero(t, neutralCalls)

	mismatchedBinding := expectedReferenceRuntimeBindingV1
	mismatchedBinding.ProtocolCommit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	service = &Service{
		sourceInstance: "runtime-source",
		referenceMode:  ReferenceModeShadow,
		referenceProjector: func([]byte, string) (ReferenceEnvelopeV1Result, error) {
			return legacy, nil
		},
		referenceBinding: mismatchedBinding,
	}
	require.Equal(
		t,
		ReferenceProjectionIdentityMismatch,
		service.compareReferenceProjection(payload),
	)
}
