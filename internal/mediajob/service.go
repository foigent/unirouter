package mediajob

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/endpointprotocol"
)

type Service struct {
	store            Store
	objects          ObjectStore
	cipher           PayloadCipher
	authorizer       ModelAuthorizer
	idempotencyKey   []byte
	retention        time.Duration
	maxAttempts      int
	now              func() time.Time
	runtimeEnabled   bool
	admissionEnabled bool
}

type ServiceOptions struct {
	Retention        time.Duration
	MaxAttempts      int
	Now              func() time.Time
	RuntimeEnabled   bool
	AdmissionEnabled bool
}

type SubmitInput struct {
	Scope           Scope
	APIKeyID        int64
	RequestID       string
	ClientRequestID string
	IdempotencyKey  string
	AllowedModels   string
	Body            []byte
}

func NewService(
	store Store,
	objects ObjectStore,
	cipher PayloadCipher,
	authorizer ModelAuthorizer,
	idempotencyKey []byte,
	options ServiceOptions,
) (*Service, error) {
	if store == nil || objects == nil || cipher == nil || authorizer == nil ||
		len(idempotencyKey) < 32 {
		return nil, domainError(ErrUnavailable, "service_dependency_unavailable")
	}
	if options.Retention == 0 {
		options.Retention = DefaultRetention
	}
	if options.Retention != DefaultRetention {
		return nil, domainError(ErrInvalid, "retention_must_be_seven_days")
	}
	if options.MaxAttempts == 0 {
		options.MaxAttempts = 3
	}
	if options.MaxAttempts < 1 || options.MaxAttempts > 10 {
		return nil, domainError(ErrInvalid, "max_attempts_invalid")
	}
	if options.AdmissionEnabled && !options.RuntimeEnabled {
		return nil, domainError(ErrInvalid, "admission_requires_runtime")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Service{
		store: store, objects: objects, cipher: cipher, authorizer: authorizer,
		idempotencyKey: append([]byte(nil), idempotencyKey...),
		retention:      options.Retention, maxAttempts: options.MaxAttempts,
		now: options.Now, runtimeEnabled: options.RuntimeEnabled,
		admissionEnabled: options.AdmissionEnabled,
	}, nil
}

func (s *Service) Submit(ctx context.Context, input SubmitInput) (CreateResult, error) {
	if err := ctx.Err(); err != nil {
		return CreateResult{}, err
	}
	if !input.Scope.Valid() || input.APIKeyID <= 0 ||
		strings.TrimSpace(input.RequestID) == "" {
		return CreateResult{}, domainError(ErrInvalid, "submission_identity_invalid")
	}
	request, err := DecodeSubmitRequest(input.Body)
	if err != nil {
		return CreateResult{}, err
	}
	authorization, err := s.authorizer.AuthorizeModel(ctx, input.Scope, request.Model)
	if err != nil {
		switch {
		case errors.Is(err, ErrForbidden):
			return CreateResult{}, domainError(ErrForbidden, "model_not_allowed")
		case errors.Is(err, ErrUnavailable):
			return CreateResult{}, domainError(ErrUnavailable, "model_authority_unavailable")
		default:
			return CreateResult{}, domainError(ErrUnavailable, "model_authority_failed")
		}
	}
	authorization.RoutingModel = strings.TrimSpace(authorization.RoutingModel)
	if authorization.RoutingModel == "" {
		return CreateResult{}, domainError(ErrForbidden, "model_not_allowed")
	}
	if !modelAllowed(
		input.AllowedModels,
		request.Model,
		authorization.RoutingModel,
	) {
		return CreateResult{}, domainError(ErrForbidden, "model_not_allowed_for_api_key")
	}
	requestHash, normalizedBody, err := RequestSHA256(request)
	if err != nil {
		return CreateResult{}, err
	}
	idempotencyHash, err := IdempotencyHash(
		s.idempotencyKey,
		input.Scope,
		input.APIKeyID,
		input.IdempotencyKey,
	)
	if err != nil {
		return CreateResult{}, err
	}
	ciphertext, err := s.cipher.Seal(normalizedBody)
	if err != nil {
		return CreateResult{}, err
	}
	jobID, err := NewJobID()
	if err != nil {
		return CreateResult{}, err
	}
	now := s.now().UTC()
	record := CreateRecord{
		AllowCreate: s.admissionEnabled,
		Job: Job{
			ContractVersion: ContractVersion,
			JobID:           jobID, Scope: input.Scope, APIKeyID: input.APIKeyID,
			RequestID:         input.RequestID,
			ClientRequestID:   strings.TrimSpace(input.ClientRequestID),
			Operation:         OperationTextToImage,
			ProfileID:         endpointprotocol.ProfileOpenAIImageGPT,
			RequestedModel:    request.Model,
			RoutingModel:      authorization.RoutingModel,
			RequestSHA256:     requestHash,
			IdempotencyHash:   idempotencyHash,
			RequestCiphertext: ciphertext,
			Lifecycle: Lifecycle{
				Admission: AdmissionAccepted,
				Queue:     QueueQueued, Lease: LeaseUnleased,
				Execution: ExecutionPending, Outcome: OutcomePending,
				Cancellation: CancellationNotRequested,
				Object:       ObjectPending, Settlement: SettlementNotEligible,
			},
			MaxAttempts:   s.maxAttempts,
			NextAttemptAt: now,
			CreatedAt:     now, UpdatedAt: now,
		},
	}
	job, reused, err := s.store.CreateOrGet(ctx, record)
	if err != nil {
		return CreateResult{}, err
	}
	if reused && job.RequestSHA256 != requestHash {
		return CreateResult{}, domainError(ErrConflict, "idempotency_payload_conflict")
	}
	return CreateResult{Job: job, Reused: reused}, nil
}

func (s *Service) RuntimeEnabled() bool {
	return s != nil && s.runtimeEnabled
}

func (s *Service) AdmissionEnabled() bool {
	return s != nil && s.admissionEnabled
}

func modelAllowed(allowedModels string, models ...string) bool {
	if strings.TrimSpace(allowedModels) == "" {
		return true
	}
	for _, allowed := range strings.Split(allowedModels, ",") {
		allowed = strings.TrimSpace(allowed)
		if allowed == "*" {
			return true
		}
		for _, model := range models {
			if allowed == model {
				return true
			}
		}
	}
	return false
}

func (s *Service) Get(ctx context.Context, scope Scope, jobID string) (Job, error) {
	if err := ctx.Err(); err != nil {
		return Job{}, err
	}
	if !scope.Valid() || !ValidJobID(jobID) {
		return Job{}, domainError(ErrInvalid, "job_lookup_invalid")
	}
	return s.store.Get(ctx, scope, jobID)
}

func (s *Service) List(ctx context.Context, filter ListFilter) (ListResult, error) {
	if err := ctx.Err(); err != nil {
		return ListResult{}, err
	}
	if !filter.Scope.Valid() {
		return ListResult{}, domainError(ErrInvalid, "list_scope_invalid")
	}
	if filter.Limit == 0 {
		filter.Limit = 50
	}
	if filter.Limit < 1 || filter.Limit > 100 {
		return ListResult{}, domainError(ErrInvalid, "list_limit_invalid")
	}
	return s.store.List(ctx, filter)
}

func (s *Service) Cancel(
	ctx context.Context,
	scope Scope,
	jobID string,
) (CancellationResult, error) {
	if err := ctx.Err(); err != nil {
		return CancellationResult{}, err
	}
	if !scope.Valid() || !ValidJobID(jobID) {
		return CancellationResult{}, domainError(ErrInvalid, "cancellation_identity_invalid")
	}
	return s.store.RequestCancellation(ctx, scope, jobID, s.now().UTC())
}

func (s *Service) ReadOutput(
	ctx context.Context,
	scope Scope,
	jobID string,
) (OutputContent, error) {
	if err := ctx.Err(); err != nil {
		return OutputContent{}, err
	}
	job, err := s.Get(ctx, scope, jobID)
	if err != nil {
		return OutputContent{}, err
	}
	if !job.ContentReadable(s.now().UTC()) {
		if job.Output != nil && job.Output.ExpiresAt != nil &&
			!s.now().UTC().Before(job.Output.ExpiresAt.UTC()) {
			return OutputContent{}, ErrOutputExpired
		}
		return OutputContent{}, domainError(ErrConflict, "output_not_available")
	}
	return s.objects.Read(ctx, job)
}

func decodeStoredRequest(cipher PayloadCipher, job Job) (NormalizedRequest, error) {
	body, err := cipher.Open(job.RequestCiphertext)
	if err != nil {
		return NormalizedRequest{}, err
	}
	var request NormalizedRequest
	if err := json.Unmarshal(body, &request); err != nil {
		return NormalizedRequest{}, domainError(ErrUnavailable, "stored_request_invalid")
	}
	normalized, err := NormalizeSubmitRequest(SubmitRequest{
		Model: request.Model, Prompt: request.Prompt, N: &request.N,
		Size: request.Size, Quality: request.Quality,
		Background: request.Background, OutputFormat: request.OutputFormat,
		Stream: &request.Stream,
	})
	if err != nil {
		return NormalizedRequest{}, domainError(ErrUnavailable, "stored_request_invalid")
	}
	hash, _, err := RequestSHA256(normalized)
	if err != nil || hash != job.RequestSHA256 {
		return NormalizedRequest{}, domainError(ErrConflict, "stored_request_hash_mismatch")
	}
	return normalized, nil
}

func providerFailure(err error) (string, bool, bool) {
	var providerErr *ProviderError
	if errors.As(err, &providerErr) && providerErr != nil {
		code := sanitizeErrorCode(providerErr.Code)
		return code, providerErr.Retryable, providerErr.OutcomeUnknown
	}
	// Provider implementations must explicitly prove a definitive failure before
	// redispatch is allowed. An unclassified error might have happened after the
	// upstream accepted the request, so fail closed as an unknown outcome.
	return "provider_outcome_unclassified", false, true
}
