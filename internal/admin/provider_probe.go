package admin

import (
	"context"
	"encoding/json"

	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/models"
)

// runProviderCredentialProbe is the single live-probe path used by ordinary
// connectivity checks and zero-downtime credential rotation. Persistence and
// lifecycle decisions remain with the caller.
func runProviderCredentialProbe(
	ctx context.Context,
	channelID int64,
	baseURL, channelModels, adapterType string,
	adapterConfig, modelMapping json.RawMessage,
	apiKey, upstreamModel, prompt string,
) (*adapters.TestResult, error) {
	adapter, err := adapters.DefaultRegistry().Get(adapterType)
	if err != nil {
		return nil, err
	}
	return adapter.Test(ctx, adapters.TestInput{
		Channel: models.Channel{
			ID:            channelID,
			BaseURL:       baseURL,
			Models:        channelModels,
			ModelMapping:  modelMapping,
			AdapterType:   adapters.NormalizeType(adapterType),
			AdapterConfig: adapterConfig,
		},
		APIKey: apiKey,
		Model:  upstreamModel,
		Prompt: prompt,
	})
}
