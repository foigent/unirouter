package modelexecution

import (
	"context"
	"time"
)

type transactionRetry uint8

const (
	retryNone transactionRetry = iota
	retrySerialization
	retryCommitAmbiguity
)

const commitRecoveryTimeout = 5 * time.Second

// runBoundedTransaction gives ordinary transaction work one retry only for a
// PostgreSQL serialization/deadlock failure. A commit error has a separate
// one-shot recovery budget because the caller cannot know whether PostgreSQL
// committed; the operation must replay its exact durable owner/fence instead
// of minting new authority.
func runBoundedTransaction[T any](
	ctx context.Context,
	operation func(context.Context) (T, transactionRetry, error),
) (T, error) {
	serializationRetryAvailable := true
	for {
		result, retry, err := operation(ctx)
		if err == nil {
			return result, nil
		}
		switch retry {
		case retrySerialization:
			if serializationRetryAvailable {
				serializationRetryAvailable = false
				continue
			}
		case retryCommitAmbiguity:
			return runCommitRecovery(ctx, serializationRetryAvailable, operation)
		}
		return result, err
	}
}

func runCommitRecovery[T any](
	ctx context.Context,
	serializationRetryAvailable bool,
	operation func(context.Context) (T, transactionRetry, error),
) (T, error) {
	// A commit error can coincide with caller cancellation even though
	// PostgreSQL committed. Resolve that uncertainty under one fresh, bounded
	// context while preserving request-scoped values.
	recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), commitRecoveryTimeout)
	defer cancel()
	for {
		result, retry, err := operation(recoveryCtx)
		if err == nil {
			return result, nil
		}
		if retry == retrySerialization && serializationRetryAvailable {
			serializationRetryAvailable = false
			continue
		}
		return result, err
	}
}

func transactionRetryFor(err error) transactionRetry {
	if retryableTransaction(err) {
		return retrySerialization
	}
	return retryNone
}
