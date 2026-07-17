package workloadadmission

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const maxLeaseTTL = 24 * time.Hour

type Service struct {
	rdb     *redis.Client
	options Options
}

func NewService(rdb *redis.Client, options Options) (*Service, error) {
	if rdb == nil {
		return nil, fmt.Errorf("%w: Redis client is required", ErrInvalidRequest)
	}
	if err := options.Validate(); err != nil {
		return nil, err
	}
	return &Service{rdb: rdb, options: options}, nil
}

func (s *Service) Options() Options {
	if s == nil {
		return Options{}
	}
	return s.options
}

func (s *Service) Version() string {
	if s == nil {
		return ""
	}
	o := s.options
	canonical := fmt.Sprintf("admission-v2|redis=fail-closed|g=%d|o=%d|gs=%d|os=%d|i=%d/%d|s=%d/%d|e=%d/%d|poll=%d|active=%d|store=%d|grace=%d|queue=%d/%d/%d",
		o.GlobalCapacity, o.OrgCapacity, o.GlobalShortReserve, o.OrgShortReserve,
		o.Interactive.Weight, ceilMilliseconds(o.Interactive.MaxWait),
		o.Stream.Weight, ceilMilliseconds(o.Stream.MaxWait),
		o.Embedding.Weight, ceilMilliseconds(o.Embedding.MaxWait),
		ceilMilliseconds(o.PollInterval), ceilMilliseconds(o.ActivePollWindow),
		ceilMilliseconds(o.StoreTimeout), ceilMilliseconds(o.LeaseGrace),
		o.MaxQueueDepth, o.MaxOrgQueueDepth, o.MaxQueueScan)
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:8])
}

func (s *Service) Reserve(ctx context.Context, request Request) (*Reservation, Decision, error) {
	if s == nil {
		return nil, Decision{}, ErrInvalidRequest
	}
	ctx = contextOrBackground(ctx)
	policy, ok := s.options.Policy(request.Class)
	if !ok || request.OperationID == "" || len(request.OperationID) > 64 || request.OrgID <= 0 ||
		request.LeaseTTL <= 0 || request.LeaseTTL > maxLeaseTTL {
		return nil, Decision{}, ErrInvalidRequest
	}
	member := operationMember(request.OperationID)
	fingerprint := operationFingerprint(request, policy.Weight)
	stateTTL := request.LeaseTTL + s.options.LeaseGrace + policy.MaxWait + time.Second
	if stateTTL > maxLeaseTTL+time.Minute {
		stateTTL = maxLeaseTTL + time.Minute
	}
	keys := runtimeKeys(request.OrgID)
	for {
		callCtx, cancel := s.storeContext(ctx, policy.MaxWait)
		value, err := reserveScript.Run(callCtx, s.rdb, keys,
			member, fingerprint, request.OrgID, request.Class, policy.Weight,
			s.options.GlobalCapacity, s.options.OrgCapacity,
			s.options.GlobalShortReserve, s.options.OrgShortReserve,
			ceilMilliseconds(request.LeaseTTL+s.options.LeaseGrace), ceilMilliseconds(policy.MaxWait),
			ceilMilliseconds(s.options.ActivePollWindow), s.options.MaxQueueDepth, s.options.MaxQueueScan,
			ceilSeconds(stateTTL), s.Version(), s.options.MaxOrgQueueDepth).Result()
		cancel()
		if err != nil {
			s.abandon(request.OrgID, member, fingerprint, stateTTL)
			if ctx.Err() != nil {
				decision := s.decision(request.Class, policy.Weight, "deny", ReasonStateUnavailable)
				decision.RetryAfter = time.Second
				return nil, decision, ErrStateUnavailable
			}
			decision := s.decision(request.Class, policy.Weight, "deny", ReasonStateUnavailable)
			decision.RetryAfter = time.Second
			return nil, decision, ErrStateUnavailable
		}
		store, err := parseStoreResult(value)
		if err != nil {
			s.abandon(request.OrgID, member, fingerprint, stateTTL)
			decision := s.decision(request.Class, policy.Weight, "deny", ReasonStateUnavailable)
			decision.RetryAfter = time.Second
			return nil, decision, ErrStateUnavailable
		}
		decision := s.decision(request.Class, policy.Weight, "allow", ReasonAllowed)
		decision.GlobalUsed = store.GlobalUsed
		decision.OrgUsed = store.OrgUsed
		decision.Waited = time.Duration(store.WaitedMS) * time.Millisecond
		decision.QueueDepth = store.QueueDepth
		switch store.Code {
		case storeAdmitted, storeIdempotent:
			return &Reservation{
				OperationID: request.OperationID, OrgID: request.OrgID, Class: request.Class,
				Weight: policy.Weight, member: member, fingerprint: fingerprint,
				Idempotent: store.Code == storeIdempotent,
			}, decision, nil
		case storeWait:
			wait := time.Duration(store.RetryMS) * time.Millisecond
			if wait <= 0 || wait > s.options.PollInterval {
				wait = s.options.PollInterval
			}
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				s.abandon(request.OrgID, member, fingerprint, stateTTL)
				decision.Decision = "deny"
				decision.Reason = ReasonStateUnavailable
				decision.RetryAfter = time.Second
				return nil, decision, ErrStateUnavailable
			case <-timer.C:
			}
		case storeCapacity:
			decision.Decision = "deny"
			decision.Reason = ReasonCapacityExceeded
			decision.RetryAfter = time.Duration(maxInt64(1, store.RetryMS)) * time.Millisecond
			return nil, decision, ErrCapacityExceeded
		case storeQueueFull:
			decision.Decision = "deny"
			decision.Reason = ReasonQueueFull
			decision.RetryAfter = time.Second
			return nil, decision, ErrCapacityExceeded
		case storeConflict:
			decision.Decision = "deny"
			decision.Reason = ReasonConflict
			return nil, decision, ErrConflict
		case storeConfigConflict:
			decision.Decision = "deny"
			decision.Reason = ReasonConfigConflict
			decision.RetryAfter = time.Second
			return nil, decision, ErrConfigConflict
		default:
			decision.Decision = "deny"
			decision.Reason = ReasonStateUnavailable
			decision.RetryAfter = time.Second
			return nil, decision, ErrStateUnavailable
		}
	}
}

func (s *Service) Release(ctx context.Context, reservation *Reservation) error {
	if s == nil || reservation == nil || reservation.OperationID == "" || reservation.OrgID <= 0 || reservation.member == "" || reservation.fingerprint == "" {
		return ErrInvalidRequest
	}
	if reservation.Fallback {
		// Backward-compatible cleanup for a reservation admitted by a drained
		// pre-v2 process. Admission v2 never creates fallback reservations.
		return nil
	}
	callCtx, cancel := context.WithTimeout(contextOrBackground(ctx), s.options.StoreTimeout)
	defer cancel()
	value, err := releaseScript.Run(callCtx, s.rdb, runtimeKeys(reservation.OrgID), reservation.member, reservation.fingerprint,
		ceilSeconds(maxLeaseTTL+time.Minute)).Result()
	if err != nil {
		return ErrStateUnavailable
	}
	values, ok := value.([]interface{})
	if !ok || len(values) != 3 {
		return ErrStateUnavailable
	}
	code, err := redisInt64(values[0])
	if err != nil {
		return ErrStateUnavailable
	}
	if code == 2 {
		return ErrConflict
	}
	return nil
}

func (s *Service) decision(class string, weight int64, result, reason string) Decision {
	return Decision{
		Class: class, Weight: weight, Decision: result, Reason: reason,
		GlobalCapacity: s.options.GlobalCapacity, OrgCapacity: s.options.OrgCapacity,
	}
}

func (s *Service) storeContext(ctx context.Context, maxWait time.Duration) (context.Context, context.CancelFunc) {
	ctx = contextOrBackground(ctx)
	remaining := s.options.StoreTimeout
	if deadline, ok := ctx.Deadline(); ok {
		until := time.Until(deadline)
		if until < remaining {
			remaining = until
		}
	}
	if maxWait > 0 && maxWait < remaining {
		remaining = maxWait
	}
	if remaining <= 0 {
		remaining = time.Millisecond
	}
	return context.WithTimeout(ctx, remaining)
}

func (s *Service) abandon(orgID int64, member, fingerprint string, stateTTL time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), s.options.StoreTimeout)
	defer cancel()
	_, _ = abandonScript.Run(ctx, s.rdb, runtimeKeys(orgID), member, fingerprint, ceilSeconds(stateTTL)).Result()
}

func contextOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func ceilMilliseconds(value time.Duration) int64 {
	if value <= 0 {
		return 1
	}
	return int64((value + time.Millisecond - 1) / time.Millisecond)
}

func ceilSeconds(value time.Duration) int64 {
	if value <= 0 {
		return 1
	}
	return int64((value + time.Second - 1) / time.Second)
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
