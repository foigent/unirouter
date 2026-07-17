package workloadadmission

import (
	"errors"
	"time"
)

const (
	ClassInteractive = "interactive"
	ClassStream      = "stream"
	ClassEmbedding   = "embedding"

	ReasonAllowed          = "allowed"
	ReasonCapacityExceeded = "workload_capacity_exceeded"
	ReasonStateUnavailable = "state_unavailable"
	ReasonLastKnownSafe    = "last_known_safe"
	ReasonConflict         = "operation_conflict"
	ReasonQueueFull        = "workload_queue_full"
	ReasonConfigConflict   = "workload_configuration_conflict"
)

var (
	ErrInvalidRequest   = errors.New("invalid workload admission request")
	ErrCapacityExceeded = errors.New("weighted workload capacity exceeded")
	ErrStateUnavailable = errors.New("weighted workload state unavailable")
	ErrConfigConflict   = errors.New("weighted workload configuration conflict")
	ErrConflict         = errors.New("weighted workload operation conflict")
)

type ClassPolicy struct {
	Weight  int64
	MaxWait time.Duration
}

type Options struct {
	GlobalCapacity     int64
	OrgCapacity        int64
	GlobalShortReserve int64
	OrgShortReserve    int64
	Interactive        ClassPolicy
	Stream             ClassPolicy
	Embedding          ClassPolicy
	PollInterval       time.Duration
	ActivePollWindow   time.Duration
	StoreTimeout       time.Duration
	LeaseGrace         time.Duration
	MaxQueueDepth      int64
	MaxOrgQueueDepth   int64
	MaxQueueScan       int64
}

func DefaultOptions() Options {
	return Options{
		GlobalCapacity:     100,
		OrgCapacity:        10,
		GlobalShortReserve: 10,
		OrgShortReserve:    2,
		Interactive:        ClassPolicy{Weight: 1, MaxWait: 50 * time.Millisecond},
		Stream:             ClassPolicy{Weight: 4, MaxWait: 250 * time.Millisecond},
		Embedding:          ClassPolicy{Weight: 1, MaxWait: 50 * time.Millisecond},
		PollInterval:       5 * time.Millisecond,
		ActivePollWindow:   25 * time.Millisecond,
		StoreTimeout:       100 * time.Millisecond,
		LeaseGrace:         5 * time.Second,
		MaxQueueDepth:      512,
		MaxOrgQueueDepth:   64,
		MaxQueueScan:       64,
	}
}

func (o Options) Validate() error {
	if o.GlobalCapacity <= 0 || o.OrgCapacity <= 0 || o.OrgCapacity > o.GlobalCapacity ||
		o.GlobalShortReserve < 0 || o.GlobalShortReserve >= o.GlobalCapacity ||
		o.OrgShortReserve < 0 || o.OrgShortReserve >= o.OrgCapacity ||
		o.PollInterval <= 0 || o.ActivePollWindow < o.PollInterval ||
		o.StoreTimeout <= 0 || o.StoreTimeout > time.Second || o.LeaseGrace < 0 ||
		o.MaxQueueDepth <= 0 || o.MaxQueueDepth > 10_000 ||
		o.MaxOrgQueueDepth <= 0 || o.MaxOrgQueueDepth > o.MaxQueueDepth ||
		o.MaxQueueScan <= 0 || o.MaxQueueScan > o.MaxQueueDepth {
		return ErrInvalidRequest
	}
	for _, policy := range []ClassPolicy{o.Interactive, o.Stream, o.Embedding} {
		if policy.Weight <= 0 || policy.Weight > o.OrgCapacity || policy.MaxWait <= 0 || policy.MaxWait > 2*time.Second {
			return ErrInvalidRequest
		}
	}
	if o.Stream.Weight > o.OrgCapacity-o.OrgShortReserve || o.Stream.Weight > o.GlobalCapacity-o.GlobalShortReserve {
		return ErrInvalidRequest
	}
	return nil
}

func (o Options) Policy(class string) (ClassPolicy, bool) {
	switch class {
	case ClassInteractive:
		return o.Interactive, true
	case ClassStream:
		return o.Stream, true
	case ClassEmbedding:
		return o.Embedding, true
	default:
		return ClassPolicy{}, false
	}
}

type Request struct {
	OperationID string
	OrgID       int64
	Class       string
	LeaseTTL    time.Duration
}

type Reservation struct {
	OperationID string
	OrgID       int64
	Class       string
	Weight      int64
	member      string
	fingerprint string
	Fallback    bool
	Idempotent  bool
}

type Decision struct {
	Class          string
	Weight         int64
	Decision       string
	Reason         string
	Waited         time.Duration
	RetryAfter     time.Duration
	GlobalUsed     int64
	OrgUsed        int64
	GlobalCapacity int64
	OrgCapacity    int64
	QueueDepth     int64
	Fallback       bool
}
