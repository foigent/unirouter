package mediajob

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/limitpolicy"
)

type Service struct {
	store            ServiceStore
	objects          ObjectStore
	cipher           PayloadCipher
	authorizer       ModelAuthorizer
	idempotencyKey   []byte
	retention        time.Duration
	maxAttempts      int
	now              func() time.Time
	runtimeEnabled   bool
	admissionEnabled bool
	limits           LimitPolicyRuntime
	requestTTL       time.Duration
}

type ServiceOptions struct {
	Retention        time.Duration
	MaxAttempts      int
	Now              func() time.Time
	RuntimeEnabled   bool
	AdmissionEnabled bool
	LimitPolicy      LimitPolicyRuntime
	RequestTTL       time.Duration
}

type SubmitInput struct {
	Scope              Scope
	APIKeyID           int64
	AccessGrantID      int64
	AccessCredentialID int64
	CredentialRef      string
	RequestID          string
	ClientRequestID    string
	IdempotencyKey     string
	AllowedModels      string
	LimitSubject       limitpolicy.Subject
	Body               []byte
}

func NewService(
	store ServiceStore,
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
	if options.AdmissionEnabled && options.LimitPolicy == nil {
		return nil, domainError(ErrUnavailable, "limit_policy_unavailable")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.RequestTTL == 0 {
		options.RequestTTL = 24 * time.Hour
	}
	if options.RequestTTL < time.Hour || options.RequestTTL > 72*time.Hour || options.RequestTTL%time.Hour != 0 {
		return nil, domainError(ErrInvalid, "request_ttl_invalid")
	}
	return &Service{
		store: store, objects: objects, cipher: cipher, authorizer: authorizer,
		idempotencyKey: append([]byte(nil), idempotencyKey...),
		retention:      options.Retention, maxAttempts: options.MaxAttempts,
		now: options.Now, runtimeEnabled: options.RuntimeEnabled,
		admissionEnabled: options.AdmissionEnabled, limits: options.LimitPolicy,
		requestTTL: options.RequestTTL,
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
	input.CredentialRef = strings.TrimSpace(input.CredentialRef)
	bound := input.AccessGrantID > 0 || input.AccessCredentialID > 0 || input.CredentialRef != ""
	if bound && (input.AccessGrantID <= 0 || input.AccessCredentialID <= 0 || input.CredentialRef == "") {
		return CreateResult{}, domainError(ErrInvalid, "submission_access_binding_invalid")
	}
	request, err := DecodeSubmitRequest(input.Body)
	if err != nil {
		return CreateResult{}, err
	}
	requestHash, normalizedBody, err := RequestSHA256(request)
	if err != nil {
		return CreateResult{}, err
	}
	idempotencyHash, err := AccessIdempotencyHash(
		s.idempotencyKey,
		input.Scope,
		input.APIKeyID,
		input.AccessCredentialID,
		input.IdempotencyKey,
	)
	if err != nil {
		return CreateResult{}, err
	}
	subjectType, subjectID := mediaIdempotencySubject(input.APIKeyID, input.AccessCredentialID)
	existing, existingErr := s.store.FindByIdempotency(ctx, IdempotencyLookup{
		Scope: input.Scope, SubjectType: subjectType, SubjectID: subjectID, Hash: idempotencyHash,
	})
	if existingErr == nil {
		if existing.RequestSHA256 != requestHash {
			return CreateResult{}, domainError(ErrConflict, "idempotency_payload_conflict")
		}
		return CreateResult{Job: existing, Reused: true}, nil
	}
	if !errors.Is(existingErr, ErrNotFound) {
		return CreateResult{}, existingErr
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
	limitSubject := input.LimitSubject
	limitSubject.OrgID = input.Scope.OrgID
	limitSubject.ProjectID = input.Scope.ProjectID
	limitSubject.APIKeyID = input.APIKeyID
	limitSubject.AccessCredentialID = input.AccessCredentialID
	limitSubject.CredentialRef = input.CredentialRef
	limitSubject.AccessGrantID = input.AccessGrantID
	limitSubject.Model = authorization.RoutingModel
	var rateReservation *limitpolicy.Reservation
	if s.admissionEnabled {
		rateReservation, err = s.limits.Reserve(ctx, limitpolicy.AdmissionRequest{
			OperationID: mediaLimitOperationID("rate", idempotencyHash),
			Subject:     limitSubject, LeaseTTL: 2 * time.Minute,
			Phase: limitpolicy.AdmissionPhaseRate,
		})
		if err != nil {
			return CreateResult{}, mediaRateLimitError(err)
		}
	}
	ciphertext, err := s.cipher.Seal(normalizedBody)
	if err != nil {
		s.cancelRateReservation(ctx, rateReservation)
		return CreateResult{}, err
	}
	jobID, err := NewJobID()
	if err != nil {
		s.cancelRateReservation(ctx, rateReservation)
		return CreateResult{}, err
	}
	now := s.now().UTC()
	record := CreateRecord{
		AllowCreate:          s.admissionEnabled,
		ExpectedLimitSubject: limitSubject,
		RequestTTL:           s.requestTTL,
		Job: Job{
			ContractVersion: ContractVersion,
			JobID:           jobID, Scope: input.Scope, APIKeyID: input.APIKeyID,
			AccessGrantID:          positiveInt64Pointer(input.AccessGrantID),
			AccessCredentialID:     positiveInt64Pointer(input.AccessCredentialID),
			CredentialRef:          input.CredentialRef,
			RequestID:              input.RequestID,
			ClientRequestID:        strings.TrimSpace(input.ClientRequestID),
			Operation:              OperationTextToImage,
			ProfileID:              endpointprotocol.ProfileOpenAIImageGPT,
			RequestedModel:         request.Model,
			RoutingModel:           authorization.RoutingModel,
			RequestSHA256:          requestHash,
			IdempotencyHash:        idempotencyHash,
			IdempotencySubjectType: subjectType,
			IdempotencySubjectID:   subjectID,
			RequestCiphertext:      ciphertext,
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
		s.cancelRateReservation(ctx, rateReservation)
		return CreateResult{}, err
	}
	if reused && job.RequestSHA256 != requestHash {
		return CreateResult{}, domainError(ErrConflict, "idempotency_payload_conflict")
	}
	if rateReservation != nil {
		// Rate attempts remain in their exact RPS/RPM windows. Finalization only
		// closes the idempotent operation handle; there is no concurrency or TPM
		// reservation in this phase.
		_ = s.limits.Finalize(context.WithoutCancel(ctx), rateReservation, 0)
	}
	return CreateResult{Job: job, Reused: reused}, nil
}

func mediaIdempotencySubject(apiKeyID, credentialID int64) (string, int64) {
	if credentialID > 0 {
		return "access_credential", credentialID
	}
	return "api_key", apiKeyID
}

func mediaLimitOperationID(phase, identity string) string {
	digest := sha256.Sum256([]byte("unirouter.async-media.limit." + phase + "\x00" + identity))
	return "media-" + phase + "-" + hex.EncodeToString(digest[:20])
}

func mediaRateLimitError(err error) error {
	switch {
	case errors.Is(err, limitpolicy.ErrRPSExceeded):
		return domainError(ErrRateLimited, "limit_policy.rps_exceeded")
	case errors.Is(err, limitpolicy.ErrRPMExceeded):
		return domainError(ErrRateLimited, "limit_policy.rpm_exceeded")
	case errors.Is(err, limitpolicy.ErrConflict):
		return domainError(ErrConflict, "idempotency_payload_conflict")
	default:
		return domainError(ErrUnavailable, "limit_policy_state_unavailable")
	}
}

func (s *Service) cancelRateReservation(ctx context.Context, reservation *limitpolicy.Reservation) {
	if s == nil || s.limits == nil || reservation == nil || reservation.Idempotent {
		return
	}
	_ = s.limits.CancelBeforeDispatch(context.WithoutCancel(ctx), reservation)
}

func positiveInt64Pointer(value int64) *int64 {
	if value <= 0 {
		return nil
	}
	copy := value
	return &copy
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
