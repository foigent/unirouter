package mediajob

import "context"

type UnavailableObjectStore struct{}

func (UnavailableObjectStore) Persist(
	context.Context,
	Job,
	ProviderResult,
) (PersistedResult, error) {
	return PersistedResult{}, domainError(ErrUnavailable, "object_store_disabled")
}

func (UnavailableObjectStore) Recover(context.Context, Job) (*PersistedResult, error) {
	return nil, domainError(ErrUnavailable, "object_store_disabled")
}

func (UnavailableObjectStore) Read(context.Context, Job) (OutputContent, error) {
	return OutputContent{}, domainError(ErrUnavailable, "object_store_disabled")
}

func (UnavailableObjectStore) Delete(context.Context, Job) error {
	return domainError(ErrUnavailable, "object_store_disabled")
}
