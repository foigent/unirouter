package mediajob

import (
	"context"
	"errors"
	"io"

	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

func (p *GPTImageProvider) CheckProviderTask(ctx context.Context, job Job) (ProviderResult, error) {
	route := job.dispatchRoute
	if route == nil || route.PublicEvidence == nil || job.CanonicalDispatch == nil ||
		!sameCanonicalMediaRoute(*job.CanonicalDispatch, *route.PublicEvidence) ||
		job.Attempt != 1 || !job.SingleUpstreamAttempt ||
		!modelprofile.SupportsMediaJobBinding(job.ProfileID, route.Provider, route.UpstreamModel) {
		return ProviderResult{}, &ProviderError{Code: "provider_task_dispatch_binding_invalid", OutcomeUnknown: true}
	}
	protocol, err := endpointprotocol.NewRegistry().Resolve(job.ProfileID, route.PublicEvidence.ProtocolProfileVersion)
	profile, async := protocol.(endpointprotocol.AsyncMediaProfile)
	if err != nil || !async {
		return ProviderResult{}, &ProviderError{Code: "provider_task_profile_invalid", OutcomeUnknown: true}
	}
	secret, err := auth.DecryptKey(route.EncryptedSecret, p.encKey)
	if err != nil {
		return ProviderResult{}, &ProviderError{Code: "provider_key_unavailable", OutcomeUnknown: true}
	}
	defer clearString(&secret)
	req, err := profile.BuildStatusRequest(ctx, endpointprotocol.BuildInput{
		BaseURL: route.BaseURL, APIKey: secret, MappedModel: route.UpstreamModel, RequestedModel: job.RequestedModel,
	}, job.providerTaskID)
	if err != nil {
		return ProviderResult{}, &ProviderError{Code: protocolErrorCode(err), OutcomeUnknown: true}
	}
	res, err := p.httpClient.Do(req)
	if err != nil {
		return ProviderResult{}, &ProviderError{Code: "provider_task_read_unavailable", Retryable: true}
	}
	defer res.Body.Close()
	const maxBytes = 128 << 10
	body, err := io.ReadAll(io.LimitReader(res.Body, maxBytes+1))
	if err != nil {
		return ProviderResult{}, &ProviderError{Code: "provider_task_read_unavailable", Retryable: true}
	}
	if len(body) > maxBytes {
		return ProviderResult{}, &ProviderError{Code: "provider_task_read_response_too_large", OutcomeUnknown: true}
	}
	if res.StatusCode == 429 || res.StatusCode >= 500 {
		return ProviderResult{}, &ProviderError{Code: "provider_task_read_unavailable", Retryable: true}
	}
	outcome, err := profile.ParseStatusResponse(route.UpstreamModel, job.providerTaskID, res.StatusCode, body)
	if err != nil {
		return ProviderResult{}, &ProviderError{Code: protocolErrorCode(err), OutcomeUnknown: true}
	}
	switch outcome.ProviderState {
	case endpointprotocol.MediaTaskPending:
		return ProviderResult{TaskID: outcome.MediaTaskReference()}, nil
	case endpointprotocol.MediaTaskUnavailable:
		return ProviderResult{}, &ProviderError{Code: "provider_task_unavailable", OutcomeUnknown: true}
	case endpointprotocol.MediaTaskFailed:
		return ProviderResult{}, &ProviderError{Code: "provider_task_failed"}
	case endpointprotocol.MediaTaskCompleted:
		artifact, err := endpointprotocol.DownloadMP4(ctx, p.httpClient, profile, outcome.ResultURL)
		if err != nil {
			return ProviderResult{}, &ProviderError{Code: protocolErrorCode(err),
				Retryable: errors.Is(err, endpointprotocol.ErrUpstreamResponse), OutcomeUnknown: true}
		}
		outcome.ResultURL, outcome.ProviderObject = "", ""
		return ProviderResult{MappedModel: route.UpstreamModel, Bytes: artifact.Bytes, MediaType: artifact.MediaType, Outcome: outcome}, nil
	default:
		return ProviderResult{}, &ProviderError{Code: "provider_task_status_invalid", OutcomeUnknown: true}
	}
}
