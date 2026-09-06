package adapters

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResponseNormalizationStagePreservesErrorChainAndClosesCodes(t *testing.T) {
	cause := errors.New("private_provider_detail")
	for _, stage := range []string{"identity", "json", "usage", "tool_normalization", "builtin_metering", "provider_envelope", "cache_projection"} {
		err := normalizationError(stage, cause)
		require.ErrorIs(t, err, cause)
		require.Equal(t, cause.Error(), err.Error())
		require.Equal(t, stage, ResponseNormalizationStage(fmt.Errorf("outer: %w", err)))
	}
	for _, err := range []error{nil, cause, normalizationError("private_provider_detail", cause)} {
		require.Equal(t, "unknown", ResponseNormalizationStage(err))
	}
}
