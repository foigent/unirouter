package gateway

import (
	"context"
	"fmt"
	"strings"

	"github.com/uniroute/unirouter/internal/executionevidence"
	"github.com/uniroute/unirouter/internal/executionprojection"
	"github.com/uniroute/unirouter/internal/middleware"
)

type executionProjectionContextKey struct{}

type executionProjectionContextValue struct {
	OrgID      int64
	RequestID  string
	AttemptID  string
	Projection executionprojection.Projection
}

// ContextWithCoordinatorExecutionProjection binds the compact projection from
// an already-applied execution coordinator decision to its exact attempt. It is
// intentionally a leaf helper: callers must invoke it only after the
// coordinator has durably opened that attempt. The helper validates the compact
// shape and context identity but never creates or guesses a provider receipt.
func ContextWithCoordinatorExecutionProjection(
	ctx context.Context,
	orgID int64,
	requestID, attemptID string,
	compact executionevidence.CompactProjection,
) (context.Context, error) {
	requestID = strings.TrimSpace(requestID)
	attemptID = strings.TrimSpace(attemptID)
	if ctx == nil || orgID <= 0 || requestID == "" || len(requestID) > 64 ||
		attemptID == "" || len(attemptID) > 64 || strings.ContainsAny(requestID+attemptID, "\x00\r\n") {
		return ctx, fmt.Errorf("gateway execution projection requires an exact bounded attempt identity")
	}
	if contextOrgID := middleware.GetOrgIDFromContext(ctx); contextOrgID > 0 && contextOrgID != orgID {
		return ctx, fmt.Errorf("gateway execution projection organization does not match context")
	}
	if contextRequestID := strings.TrimSpace(middleware.GetRequestIDFromContext(ctx)); contextRequestID != "" && contextRequestID != requestID {
		return ctx, fmt.Errorf("gateway execution projection request does not match context")
	}
	if contextAttemptID := strings.TrimSpace(attemptIDFromContext(ctx)); contextAttemptID != "" && contextAttemptID != attemptID {
		return ctx, fmt.Errorf("gateway execution projection attempt does not match context")
	}
	normalized, err := executionevidence.NormalizeCompact(compact)
	if err != nil {
		return ctx, fmt.Errorf("gateway execution projection: %w", err)
	}
	projection, err := executionprojection.Normalize(executionprojection.Projection{
		Source:  executionprojection.SourceProviderAttempt,
		Compact: &normalized,
	})
	if err != nil {
		return ctx, err
	}
	ctx = contextWithAttemptID(ctx, attemptID)
	ctx = contextWithDurableProviderAttempt(ctx)
	return context.WithValue(ctx, executionProjectionContextKey{}, executionProjectionContextValue{
		OrgID: orgID, RequestID: requestID, AttemptID: attemptID, Projection: projection,
	}), nil
}

func contextWithDurableLegacyProviderAttempt(
	ctx context.Context,
	orgID int64,
	requestID string,
	legacyChannelID int64,
) context.Context {
	ctx = contextWithDurableProviderAttempt(ctx)
	attemptID := strings.TrimSpace(attemptIDFromContext(ctx))
	requestID = strings.TrimSpace(requestID)
	if ctx == nil || orgID <= 0 || requestID == "" || attemptID == "" || legacyChannelID <= 0 {
		return ctx
	}
	projection, err := executionprojection.Normalize(executionprojection.Projection{
		Source:          executionprojection.SourcePrecontractProviderAttempt,
		LegacyChannelID: legacyChannelID,
	})
	if err != nil {
		return ctx
	}
	return context.WithValue(ctx, executionProjectionContextKey{}, executionProjectionContextValue{
		OrgID: orgID, RequestID: requestID, AttemptID: attemptID, Projection: projection,
	})
}

func executionProjectionFromContext(
	ctx context.Context,
	orgID int64,
	requestID, attemptID string,
) (executionprojection.Projection, bool) {
	if ctx == nil {
		return executionprojection.Projection{}, false
	}
	value, ok := ctx.Value(executionProjectionContextKey{}).(executionProjectionContextValue)
	if !ok || value.OrgID != orgID || value.RequestID != strings.TrimSpace(requestID) ||
		value.AttemptID != strings.TrimSpace(attemptID) {
		return executionprojection.Projection{}, false
	}
	normalized, err := executionprojection.Normalize(value.Projection)
	if err != nil {
		return executionprojection.Projection{}, false
	}
	return normalized, true
}
