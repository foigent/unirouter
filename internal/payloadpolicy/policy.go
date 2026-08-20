package payloadpolicy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/uniroute/unirouter/internal/gatewayerror"
)

const (
	MiB = int64(1024 * 1024)

	PlatformHardMaxBytes = 64 * MiB

	ProfileChatRequest       = "chat_request"
	ProfileEmbeddingsRequest = "embeddings_request"
	ProfileNonStreamResponse = "nonstream_response"
)

var (
	ErrPayloadTooLarge = errors.New("payload exceeds the endpoint safety limit")
	ErrBudgetExceeded  = errors.New("payload in-flight byte budget is exhausted")
)

// Limits are application-layer memory safety limits. The platform hard cap is
// intentionally not a tenant setting: a future large-payload product must use
// an explicitly managed profile rather than widening normal text endpoints.
type Limits struct {
	ChatRequestBytes       int64
	EmbeddingsRequestBytes int64
	NonStreamResponseBytes int64
	NodeInFlightBytes      int64
	OrgInFlightBytes       int64
}

func DefaultLimits() Limits {
	return Limits{
		ChatRequestBytes:       8 * MiB,
		EmbeddingsRequestBytes: 16 * MiB,
		NonStreamResponseBytes: 16 * MiB,
		NodeInFlightBytes:      256 * MiB,
		OrgInFlightBytes:       64 * MiB,
	}
}

func (l Limits) Validate() error {
	profiles := map[string]int64{
		ProfileChatRequest:       l.ChatRequestBytes,
		ProfileEmbeddingsRequest: l.EmbeddingsRequestBytes,
		ProfileNonStreamResponse: l.NonStreamResponseBytes,
	}
	for name, value := range profiles {
		if value <= 0 || value > PlatformHardMaxBytes {
			return fmt.Errorf("payload profile %s must be between 1 and %d bytes", name, PlatformHardMaxBytes)
		}
	}
	if l.NodeInFlightBytes <= 0 || l.OrgInFlightBytes <= 0 || l.OrgInFlightBytes > l.NodeInFlightBytes {
		return fmt.Errorf("payload in-flight budgets require 0 < org <= node")
	}
	if l.NodeInFlightBytes < l.ChatRequestBytes || l.NodeInFlightBytes < l.EmbeddingsRequestBytes || l.NodeInFlightBytes < l.NonStreamResponseBytes ||
		l.OrgInFlightBytes < l.ChatRequestBytes || l.OrgInFlightBytes < l.EmbeddingsRequestBytes || l.OrgInFlightBytes < l.NonStreamResponseBytes {
		return fmt.Errorf("payload node and organization in-flight budgets must fit every endpoint profile")
	}
	return nil
}

func (l Limits) MaxBytes(profile string) (int64, error) {
	switch profile {
	case ProfileChatRequest:
		return l.ChatRequestBytes, nil
	case ProfileEmbeddingsRequest:
		return l.EmbeddingsRequestBytes, nil
	case ProfileNonStreamResponse:
		return l.NonStreamResponseBytes, nil
	default:
		return 0, fmt.Errorf("unknown payload profile %q", profile)
	}
}

// Budget accounts for bytes actually retained by this process. It is local by
// design: distributed request/concurrency admission is a separate policy,
// while this guard prevents one replica or organization from exhausting that
// replica's heap before routing or delivery.
type Budget struct {
	mu       sync.Mutex
	limits   Limits
	nodeUsed int64
	orgUsed  map[int64]int64
}

func NewBudget(limits Limits) (*Budget, error) {
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	return &Budget{limits: limits, orgUsed: make(map[int64]int64)}, nil
}

func MustNewBudget(limits Limits) *Budget {
	budget, err := NewBudget(limits)
	if err != nil {
		panic(err)
	}
	return budget
}

func (b *Budget) Limits() Limits {
	if b == nil {
		return DefaultLimits()
	}
	return b.limits
}

type Lease struct {
	budget *Budget
	orgID  int64
	bytes  int64
	once   sync.Once
}

func (l *Lease) Release() {
	if l == nil || l.budget == nil {
		return
	}
	l.once.Do(func() {
		l.budget.release(l.orgID, l.bytes)
		l.bytes = 0
	})
}

func (b *Budget) acquire(orgID, size int64) bool {
	if size <= 0 {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.nodeUsed+size > b.limits.NodeInFlightBytes || b.orgUsed[orgID]+size > b.limits.OrgInFlightBytes {
		return false
	}
	b.nodeUsed += size
	b.orgUsed[orgID] += size
	return true
}

func (b *Budget) release(orgID, size int64) {
	if size <= 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nodeUsed -= size
	b.orgUsed[orgID] -= size
	if b.orgUsed[orgID] <= 0 {
		delete(b.orgUsed, orgID)
	}
}

// ReadAll retains at most the selected endpoint limit and accounts for bytes
// before appending them to memory. Content-Length is never trusted as the
// authority; chunked and misleading requests follow the same bounded path.
func (b *Budget) ReadAll(reader io.Reader, orgID int64, maxBytes int64) ([]byte, *Lease, error) {
	if b == nil {
		return nil, nil, fmt.Errorf("payload budget is unavailable")
	}
	if reader == nil || maxBytes <= 0 || maxBytes > PlatformHardMaxBytes {
		return nil, nil, fmt.Errorf("payload read configuration is invalid")
	}
	lease := &Lease{budget: b, orgID: orgID}

	var out bytes.Buffer
	chunk := make([]byte, 32*1024)
	for {
		n, readErr := reader.Read(chunk)
		if n > 0 {
			if int64(out.Len())+int64(n) > maxBytes {
				lease.Release()
				return nil, nil, ErrPayloadTooLarge
			}
			if !b.acquire(orgID, int64(n)) {
				lease.Release()
				return nil, nil, ErrBudgetExceeded
			}
			lease.bytes += int64(n)
			_, _ = out.Write(chunk[:n])
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return out.Bytes(), lease, nil
			}
			lease.Release()
			return nil, nil, readErr
		}
		if n == 0 {
			lease.Release()
			return nil, nil, io.ErrNoProgress
		}
	}
}

type envelope struct {
	body  []byte
	max   int64
	lease *Lease
}

type envelopeContextKey struct{}

func BodyFromContext(ctx context.Context) ([]byte, bool) {
	if ctx == nil {
		return nil, false
	}
	value, ok := ctx.Value(envelopeContextKey{}).(*envelope)
	if !ok || value == nil {
		return nil, false
	}
	return value.body, true
}

func RequestMiddleware(budget *Budget, profile string, orgID func(context.Context) int64) func(http.Handler) http.Handler {
	return requestMiddleware(budget, func() (int64, error) {
		return budget.Limits().MaxBytes(profile)
	}, orgID)
}

// RequestMiddlewareWithMax applies the same retained-byte accounting to an
// endpoint whose product contract owns a fixed maximum rather than a
// configurable text profile. It is used for the small async-media submission
// envelope so live authorization can be repeated after the complete body has
// arrived without retaining unaccounted bytes.
func RequestMiddlewareWithMax(budget *Budget, maxBytes int64, orgID func(context.Context) int64) func(http.Handler) http.Handler {
	return requestMiddleware(budget, func() (int64, error) {
		if maxBytes <= 0 || maxBytes > PlatformHardMaxBytes {
			return 0, fmt.Errorf("payload maximum must be between 1 and %d bytes", PlatformHardMaxBytes)
		}
		return maxBytes, nil
	}, orgID)
}

func requestMiddleware(
	budget *Budget,
	maximum func() (int64, error),
	orgID func(context.Context) int64,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			maxBytes, err := maximum()
			if err != nil {
				writeError(w, r, http.StatusInternalServerError, "server_error.internal_error", "Payload policy is unavailable")
				return
			}
			id := int64(0)
			if orgID != nil {
				id = orgID(r.Context())
			}
			body, lease, err := budget.ReadAll(r.Body, id, maxBytes)
			if err != nil {
				writeReadError(w, r, err)
				return
			}
			defer lease.Release()
			_ = r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(body))
			r.ContentLength = int64(len(body))
			ctx := context.WithValue(r.Context(), envelopeContextKey{}, &envelope{
				body: body, max: maxBytes, lease: lease,
			})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ReplaceRequestBody updates the same in-memory envelope after server-owned
// model governance rewrites. It adjusts accounting before exposing the new
// bytes to downstream routing and never permits a rewrite beyond the profile.
func ReplaceRequestBody(r *http.Request, body []byte) (*http.Request, error) {
	if r == nil {
		return nil, fmt.Errorf("request is nil")
	}
	value, ok := r.Context().Value(envelopeContextKey{}).(*envelope)
	if !ok || value == nil || value.lease == nil {
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		return r, nil
	}
	if int64(len(body)) > value.max {
		return nil, ErrPayloadTooLarge
	}
	delta := int64(len(body) - len(value.body))
	if delta > 0 && !value.lease.budget.acquire(value.lease.orgID, delta) {
		return nil, ErrBudgetExceeded
	}
	if delta < 0 {
		value.lease.budget.release(value.lease.orgID, -delta)
	}
	value.lease.bytes += delta
	value.body = body
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	return r, nil
}

func writeReadError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrPayloadTooLarge):
		writeError(w, r, http.StatusRequestEntityTooLarge, "validation_error.payload_too_large", "Request payload exceeds the endpoint safety limit")
	case errors.Is(err, ErrBudgetExceeded):
		w.Header().Set("Retry-After", "1")
		writeError(w, r, http.StatusServiceUnavailable, "server_error.payload_capacity_exceeded", "Payload capacity is temporarily unavailable")
	default:
		writeError(w, r, http.StatusBadRequest, "validation_error.invalid_request", "Failed to read request body")
	}
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	gatewayerror.WriteRequest(w, r, status, code, message)
}
