package mediajob

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type serviceStoreFixture struct {
	mu         sync.Mutex
	byIdentity map[string]Job
}

func newServiceStoreFixture() *serviceStoreFixture {
	return &serviceStoreFixture{byIdentity: make(map[string]Job)}
}

func (s *serviceStoreFixture) CreateOrGet(
	_ context.Context,
	record CreateRecord,
) (Job, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := record.Job.IdempotencyHash
	if existing, ok := s.byIdentity[key]; ok {
		return cloneJob(existing), true, nil
	}
	if !record.AllowCreate {
		return Job{}, false, domainError(ErrUnavailable, "async_media_admission_disabled")
	}
	s.byIdentity[key] = cloneJob(record.Job)
	return cloneJob(record.Job), false, nil
}

func (s *serviceStoreFixture) FindByIdempotency(
	_ context.Context,
	lookup IdempotencyLookup,
) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.byIdentity[lookup.Hash]
	if !ok || job.Scope != lookup.Scope || job.IdempotencySubjectType != lookup.SubjectType ||
		job.IdempotencySubjectID != lookup.SubjectID {
		return Job{}, ErrNotFound
	}
	return cloneJob(job), nil
}

func (s *serviceStoreFixture) Get(context.Context, Scope, string) (Job, error) {
	return Job{}, ErrNotFound
}
func (s *serviceStoreFixture) List(context.Context, ListFilter) (ListResult, error) {
	return ListResult{}, nil
}
func (s *serviceStoreFixture) RequestCancellation(
	context.Context, Scope, string, time.Time,
) (CancellationResult, error) {
	return CancellationResult{}, ErrNotFound
}
func (s *serviceStoreFixture) Claim(context.Context, string, time.Time, time.Duration) (*Job, error) {
	return nil, nil
}
func (s *serviceStoreFixture) MarkRunning(context.Context, Lease, time.Time) error {
	return nil
}
func (s *serviceStoreFixture) MarkProviderSucceeded(
	context.Context, Lease, string, time.Time,
) error {
	return nil
}
func (s *serviceStoreFixture) CompleteSuccess(
	context.Context, Lease, PersistedResult, time.Duration, time.Time,
) (Job, error) {
	return Job{}, nil
}
func (s *serviceStoreFixture) CompleteFailure(
	context.Context, Lease, string, time.Time,
) (Job, error) {
	return Job{}, nil
}
func (s *serviceStoreFixture) CompleteUnknown(
	context.Context, Lease, string, time.Time,
) (Job, error) {
	return Job{}, nil
}
func (s *serviceStoreFixture) CompleteOutputFailure(
	context.Context, Lease, string, time.Time,
) (Job, error) {
	return Job{}, nil
}
func (s *serviceStoreFixture) Requeue(
	context.Context, Lease, string, time.Time, time.Time,
) (Job, error) {
	return Job{}, nil
}
func (s *serviceStoreFixture) RequeueRecovery(
	context.Context, Lease, string, time.Time, time.Time,
) (Job, error) {
	return Job{}, nil
}
func (s *serviceStoreFixture) ClaimExpired(
	context.Context, string, time.Time, time.Duration,
) (*PurgeLease, error) {
	return nil, nil
}
func (s *serviceStoreFixture) MarkPurgeRetry(
	context.Context, PurgeLease, string, time.Time, time.Time,
) error {
	return nil
}
func (s *serviceStoreFixture) MarkPurged(
	context.Context, PurgeLease, time.Time,
) (Job, error) {
	return Job{}, nil
}

func TestServiceSubmissionEncryptsPromptAndEnforcesIdempotencyConflict(t *testing.T) {
	store := newServiceStoreFixture()
	key := []byte(strings.Repeat("c", 32))
	cipher, err := NewApplicationCipher(key)
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	service, err := NewService(
		store, UnavailableObjectStore{}, cipher,
		ModelAuthorizerFunc(func(
			_ context.Context,
			_ Scope,
			model string,
		) (ModelAuthorization, error) {
			return ModelAuthorization{RoutingModel: model}, nil
		}),
		[]byte(strings.Repeat("i", 32)),
		ServiceOptions{
			RuntimeEnabled:   true,
			AdmissionEnabled: true,
			LimitPolicy:      &mediaLimitRuntimeFixture{},
			Now: func() time.Time {
				return time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC)
			},
		},
	)
	if err != nil {
		t.Fatalf("service: %v", err)
	}
	input := SubmitInput{
		Scope: Scope{OrgID: 4, ProjectID: 8}, APIKeyID: 12,
		RequestID: "request-1", ClientRequestID: "caller-1",
		IdempotencyKey: "idempotent-1", AllowedModels: "image-prod",
		Body: []byte(`{"model":"image-prod","prompt":"private prompt","output_format":"png"}`),
	}
	first, err := service.Submit(context.Background(), input)
	if err != nil || first.Reused {
		t.Fatalf("first = %+v err=%v", first, err)
	}
	if first.Job.RequestCiphertext == "" ||
		strings.Contains(first.Job.RequestCiphertext, "private prompt") {
		t.Fatalf("prompt was not encrypted: %+v", first.Job)
	}
	plaintext, err := cipher.Open(first.Job.RequestCiphertext)
	if err != nil || !strings.Contains(string(plaintext), "private prompt") {
		t.Fatalf("decrypt = %s err=%v", plaintext, err)
	}
	repeated, err := service.Submit(context.Background(), input)
	if err != nil || !repeated.Reused || repeated.Job.JobID != first.Job.JobID {
		t.Fatalf("repeat = %+v err=%v", repeated, err)
	}
	input.Body = []byte(`{"model":"image-prod","prompt":"different prompt","output_format":"png"}`)
	_, err = service.Submit(context.Background(), input)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting reuse error = %v", err)
	}
}

func TestServiceDisabledAndAPIKeyModelScopeFailClosed(t *testing.T) {
	store := newServiceStoreFixture()
	cipher, _ := NewApplicationCipher([]byte(strings.Repeat("c", 32)))
	authorizer := ModelAuthorizerFunc(func(
		_ context.Context,
		_ Scope,
		model string,
	) (ModelAuthorization, error) {
		return ModelAuthorization{RoutingModel: model}, nil
	})
	disabled, err := NewService(
		store, UnavailableObjectStore{}, cipher, authorizer,
		[]byte(strings.Repeat("i", 32)), ServiceOptions{},
	)
	if err != nil {
		t.Fatalf("disabled service: %v", err)
	}
	input := SubmitInput{
		Scope: Scope{OrgID: 1, ProjectID: 2}, APIKeyID: 3,
		RequestID: "r", IdempotencyKey: "i",
		Body: []byte(`{"model":"image-prod","prompt":"p"}`),
	}
	if _, err := disabled.Submit(context.Background(), input); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("disabled error = %v", err)
	} else {
		var contractErr *Error
		if !errors.As(err, &contractErr) ||
			contractErr.Code != "async_media_admission_disabled" {
			t.Fatalf("disabled contract error = %v", err)
		}
	}
	maintenance, err := NewService(
		store, UnavailableObjectStore{}, cipher, authorizer,
		[]byte(strings.Repeat("i", 32)),
		ServiceOptions{RuntimeEnabled: true},
	)
	if err != nil {
		t.Fatalf("maintenance service: %v", err)
	}
	if !maintenance.RuntimeEnabled() || maintenance.AdmissionEnabled() {
		t.Fatalf(
			"maintenance flags runtime=%t admission=%t",
			maintenance.RuntimeEnabled(),
			maintenance.AdmissionEnabled(),
		)
	}
	if _, err := maintenance.Submit(context.Background(), input); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("maintenance admission error = %v", err)
	} else {
		var contractErr *Error
		if !errors.As(err, &contractErr) ||
			contractErr.Code != "async_media_admission_disabled" {
			t.Fatalf("maintenance contract error = %v", err)
		}
	}
	if _, err := NewService(
		store, UnavailableObjectStore{}, cipher, authorizer,
		[]byte(strings.Repeat("i", 32)),
		ServiceOptions{AdmissionEnabled: true},
	); !errors.Is(err, ErrInvalid) {
		t.Fatalf("admission without runtime error = %v", err)
	}

	enabled, _ := NewService(
		store, UnavailableObjectStore{}, cipher, authorizer,
		[]byte(strings.Repeat("i", 32)),
		ServiceOptions{RuntimeEnabled: true, AdmissionEnabled: true, LimitPolicy: &mediaLimitRuntimeFixture{}},
	)
	created, err := enabled.Submit(context.Background(), input)
	if err != nil || created.Reused {
		t.Fatalf("enabled create = %+v err=%v", created, err)
	}
	replayed, err := maintenance.Submit(context.Background(), input)
	if err != nil || !replayed.Reused || replayed.Job.JobID != created.Job.JobID {
		t.Fatalf("maintenance replay = %+v err=%v", replayed, err)
	}
	input.AllowedModels = "text-only"
	replayed, err = enabled.Submit(context.Background(), input)
	if err != nil || !replayed.Reused || replayed.Job.JobID != created.Job.JobID {
		t.Fatalf("accepted idempotent replay was re-adjudicated: %+v err=%v", replayed, err)
	}
	input.IdempotencyKey = "i-new"
	if _, err := enabled.Submit(context.Background(), input); !errors.Is(err, ErrForbidden) {
		t.Fatalf("model scope error = %v", err)
	}
}

func TestServiceClassifiesModelAuthorityErrorsWithoutLeakingInternals(t *testing.T) {
	store := newServiceStoreFixture()
	cipher, _ := NewApplicationCipher([]byte(strings.Repeat("c", 32)))
	input := SubmitInput{
		Scope: Scope{OrgID: 1, ProjectID: 2}, APIKeyID: 3,
		RequestID: "r", IdempotencyKey: "authority-error",
		Body: []byte(`{"model":"image-prod","prompt":"p"}`),
	}
	for name, testCase := range map[string]struct {
		authorityError error
		expectedKind   error
		expectedCode   string
	}{
		"denied": {
			authorityError: ErrForbidden,
			expectedKind:   ErrForbidden,
			expectedCode:   "model_not_allowed",
		},
		"unavailable": {
			authorityError: ErrUnavailable,
			expectedKind:   ErrUnavailable,
			expectedCode:   "model_authority_unavailable",
		},
		"unexpected": {
			authorityError: errors.New("private database detail"),
			expectedKind:   ErrUnavailable,
			expectedCode:   "model_authority_failed",
		},
	} {
		t.Run(name, func(t *testing.T) {
			service, err := NewService(
				store,
				UnavailableObjectStore{},
				cipher,
				ModelAuthorizerFunc(func(
					context.Context,
					Scope,
					string,
				) (ModelAuthorization, error) {
					return ModelAuthorization{}, testCase.authorityError
				}),
				[]byte(strings.Repeat("i", 32)),
				ServiceOptions{RuntimeEnabled: true, AdmissionEnabled: true, LimitPolicy: &mediaLimitRuntimeFixture{}},
			)
			if err != nil {
				t.Fatalf("service: %v", err)
			}
			_, err = service.Submit(context.Background(), input)
			if !errors.Is(err, testCase.expectedKind) {
				t.Fatalf("error kind = %v want %v", err, testCase.expectedKind)
			}
			var contractErr *Error
			if !errors.As(err, &contractErr) || contractErr.Code != testCase.expectedCode {
				t.Fatalf("contract error = %v want %s", err, testCase.expectedCode)
			}
			if strings.Contains(err.Error(), "private database detail") {
				t.Fatalf("authority detail leaked: %v", err)
			}
		})
	}
}

func TestServiceAllowsAPIKeyResolvedModelWhilePreservingRequestedAlias(t *testing.T) {
	store := newServiceStoreFixture()
	cipher, _ := NewApplicationCipher([]byte(strings.Repeat("c", 32)))
	service, err := NewService(
		store,
		UnavailableObjectStore{},
		cipher,
		ModelAuthorizerFunc(func(
			context.Context,
			Scope,
			string,
		) (ModelAuthorization, error) {
			return ModelAuthorization{RoutingModel: "gpt-image-prod"}, nil
		}),
		[]byte(strings.Repeat("i", 32)),
		ServiceOptions{RuntimeEnabled: true, AdmissionEnabled: true, LimitPolicy: &mediaLimitRuntimeFixture{}},
	)
	if err != nil {
		t.Fatalf("service: %v", err)
	}
	result, err := service.Submit(context.Background(), SubmitInput{
		Scope: Scope{OrgID: 1, ProjectID: 2}, APIKeyID: 3,
		RequestID: "r", IdempotencyKey: "alias-resolved",
		AllowedModels: "gpt-image-prod",
		Body:          []byte(`{"model":"company-image","prompt":"p"}`),
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if result.Job.RequestedModel != "company-image" ||
		result.Job.RoutingModel != "gpt-image-prod" {
		t.Fatalf("job = %+v", result.Job)
	}
}

func TestProviderFailureRequiresExplicitRetryClassification(t *testing.T) {
	code, retryable, unknown := providerFailure(errors.New("unclassified"))
	if code != "provider_outcome_unclassified" || retryable || !unknown {
		t.Fatalf(
			"unclassified code=%q retryable=%t unknown=%t",
			code, retryable, unknown,
		)
	}

	code, retryable, unknown = providerFailure(&ProviderError{
		Code: " Provider HTTP/503 ", Retryable: true,
	})
	if code != "provider_http_503" || !retryable || unknown {
		t.Fatalf(
			"classified code=%q retryable=%t unknown=%t",
			code, retryable, unknown,
		)
	}

	code, _, _ = providerFailure(&ProviderError{Code: "***"})
	if code != "unknown_failure" {
		t.Fatalf("empty sanitized code = %q", code)
	}
}
