package agentcap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

var reserveInvocationScript = redis.NewScript(`
local workflow = KEYS[1]
local operation = KEYS[#KEYS]
local generation = ARGV[1]
local root_key_id = ARGV[2]
local agent_id = ARGV[3]
local member = ARGV[4]
local requested_tokens = tonumber(ARGV[5])
local requested_cost = tonumber(ARGV[6])
local lease_ttl_ms = tonumber(ARGV[7])
local state_ttl = tonumber(ARGV[8])
local lineage_count = tonumber(ARGV[9])
local lineage_digest = ARGV[10]

local now = redis.call("TIME")
local now_seconds = tonumber(now[1])
local now_ms = (now_seconds * 1000) + math.floor(tonumber(now[2]) / 1000)
local lease_expires_ms = now_ms + lease_ttl_ms

if redis.call("EXISTS", workflow) == 0 or
   redis.call("HGET", workflow, "status") ~= "active" or
   redis.call("HGET", workflow, "generation") ~= generation or
   redis.call("HGET", workflow, "root_key_id") ~= root_key_id or
   tonumber(redis.call("HGET", workflow, "expires_at") or "0") <= now_seconds then
    return 1
end

if redis.call("EXISTS", operation) == 1 then
    if redis.call("HGET", operation, "status") == "reserved" and
       redis.call("HGET", operation, "generation") == generation and
       redis.call("HGET", operation, "agent_id") == agent_id and
       redis.call("HGET", operation, "lineage_digest") == lineage_digest and
       tonumber(redis.call("HGET", operation, "token_reserved") or "-1") == requested_tokens and
       tonumber(redis.call("HGET", operation, "cost_reserved_cents") or "-1") == requested_cost then
        return 8
    end
    return 9
end

for index = 0, lineage_count - 1 do
    local base = 2 + (index * 4)
    local agent = KEYS[base]
    local rps = KEYS[base + 1]
    local rpm = KEYS[base + 2]
    local concurrency = KEYS[base + 3]
    if redis.call("EXISTS", agent) == 0 then
        return 2
    end
    if index == lineage_count - 1 and redis.call("HGET", agent, "agent_id") ~= agent_id then
        return 2
    end

    local cost_limit = tonumber(redis.call("HGET", agent, "cost_limit_cents") or "-1")
    local cost_reserved = tonumber(redis.call("HGET", agent, "cost_reserved_cents") or "-1")
    local cost_used = tonumber(redis.call("HGET", agent, "cost_used_cents") or "-1")
    local token_limit = tonumber(redis.call("HGET", agent, "token_limit") or "-1")
    local token_reserved = tonumber(redis.call("HGET", agent, "token_reserved") or "-1")
    local token_used = tonumber(redis.call("HGET", agent, "token_used") or "-1")
    local rps_limit = tonumber(redis.call("HGET", agent, "rps_limit") or "-1")
    local rpm_limit = tonumber(redis.call("HGET", agent, "rpm_limit") or "-1")
    local concurrency_limit = tonumber(redis.call("HGET", agent, "concurrency_limit") or "-1")
    if cost_limit < 0 or cost_reserved < 0 or cost_used < 0 or token_limit < 0 or token_reserved < 0 or
       token_used < 0 or rps_limit < 1 or rpm_limit < 1 or concurrency_limit < 1 then
        return 2
    end
    if redis.call("HGET", agent, "cost_exhausted") == "1" or requested_cost > (cost_limit - cost_used - cost_reserved) then
        return 3
    end
    if redis.call("HGET", agent, "token_exhausted") == "1" or requested_tokens > (token_limit - token_used - token_reserved) then
        return 4
    end

    redis.call("ZREMRANGEBYSCORE", rps, "-inf", now_ms - 1000)
    redis.call("ZREMRANGEBYSCORE", rpm, "-inf", now_ms - 60000)
    redis.call("ZREMRANGEBYSCORE", concurrency, "-inf", now_ms)
    if redis.call("ZCARD", rps) >= rps_limit then
        return 5
    end
    if redis.call("ZCARD", rpm) >= rpm_limit then
        return 6
    end
    if redis.call("ZCARD", concurrency) >= concurrency_limit then
        return 7
    end
end

local function extend_ttl(key, desired)
    local ttl = redis.call("TTL", key)
    if ttl >= 0 and ttl < desired then
        redis.call("EXPIRE", key, desired)
    end
end

extend_ttl(workflow, state_ttl)
for index = 0, lineage_count - 1 do
    local base = 2 + (index * 4)
    local agent = KEYS[base]
    local rps = KEYS[base + 1]
    local rpm = KEYS[base + 2]
    local concurrency = KEYS[base + 3]
    redis.call("HINCRBY", agent, "cost_reserved_cents", requested_cost)
    redis.call("HINCRBY", agent, "token_reserved", requested_tokens)
    redis.call("ZADD", rps, now_ms, member)
    redis.call("EXPIRE", rps, 2)
    redis.call("ZADD", rpm, now_ms, member)
    redis.call("EXPIRE", rpm, 120)
    redis.call("ZADD", concurrency, lease_expires_ms, member)
    redis.call("EXPIRE", concurrency, state_ttl)
    extend_ttl(agent, state_ttl)
end
redis.call("HSET", operation,
    "status", "reserved",
    "generation", generation,
    "agent_id", agent_id,
    "lineage_digest", lineage_digest,
    "token_reserved", requested_tokens,
    "cost_reserved_cents", requested_cost,
	"extension_sequence", 0,
    "lease_expires_at_ms", lease_expires_ms)
redis.call("EXPIRE", operation, state_ttl)
return 0
`)

var extendInvocationScript = redis.NewScript(`
local operation = KEYS[1]
local generation = ARGV[1]
local agent_id = ARGV[2]
local sequence = tonumber(ARGV[3])
local additional_tokens = tonumber(ARGV[4])
local additional_cost = tonumber(ARGV[5])
local lineage_count = tonumber(ARGV[6])
local lineage_digest = ARGV[7]

if redis.call("EXISTS", operation) == 0 then return 1 end
if redis.call("HGET", operation, "generation") ~= generation or
   redis.call("HGET", operation, "agent_id") ~= agent_id or
   redis.call("HGET", operation, "lineage_digest") ~= lineage_digest then
    return 2
end
if redis.call("HGET", operation, "status") ~= "reserved" then return 9 end
local current_sequence = tonumber(redis.call("HGET", operation, "extension_sequence") or "0")
if sequence == current_sequence then
    if tonumber(redis.call("HGET", operation, "last_extension_tokens") or "-1") == additional_tokens and
       tonumber(redis.call("HGET", operation, "last_extension_cost_cents") or "-1") == additional_cost then
        return 8
    end
    return 9
end
if sequence ~= current_sequence + 1 then return 9 end

for index = 0, lineage_count - 1 do
    local agent = KEYS[2 + index]
    if redis.call("EXISTS", agent) == 0 then return 2 end
    local cost_limit = tonumber(redis.call("HGET", agent, "cost_limit_cents") or "-1")
    local cost_reserved = tonumber(redis.call("HGET", agent, "cost_reserved_cents") or "-1")
    local cost_used = tonumber(redis.call("HGET", agent, "cost_used_cents") or "-1")
    local token_limit = tonumber(redis.call("HGET", agent, "token_limit") or "-1")
    local token_reserved = tonumber(redis.call("HGET", agent, "token_reserved") or "-1")
    local token_used = tonumber(redis.call("HGET", agent, "token_used") or "-1")
    if cost_limit < 0 or cost_reserved < 0 or cost_used < 0 or token_limit < 0 or token_reserved < 0 or token_used < 0 then
        return 2
    end
    if redis.call("HGET", agent, "cost_exhausted") == "1" or additional_cost > (cost_limit - cost_used - cost_reserved) then
        return 3
    end
    if redis.call("HGET", agent, "token_exhausted") == "1" or additional_tokens > (token_limit - token_used - token_reserved) then
        return 4
    end
end

for index = 0, lineage_count - 1 do
    local agent = KEYS[2 + index]
    redis.call("HINCRBY", agent, "cost_reserved_cents", additional_cost)
    redis.call("HINCRBY", agent, "token_reserved", additional_tokens)
end
redis.call("HINCRBY", operation, "token_reserved", additional_tokens)
redis.call("HINCRBY", operation, "cost_reserved_cents", additional_cost)
redis.call("HSET", operation,
    "extension_sequence", sequence,
    "last_extension_tokens", additional_tokens,
    "last_extension_cost_cents", additional_cost)
return 0
`)

var finalizeInvocationScript = redis.NewScript(`
local operation = KEYS[1]
local generation = ARGV[1]
local agent_id = ARGV[2]
local member = ARGV[3]
local actual_tokens = tonumber(ARGV[4])
local actual_cost = tonumber(ARGV[5])
local lineage_count = tonumber(ARGV[6])
local lineage_digest = ARGV[7]

if redis.call("EXISTS", operation) == 0 then
    return 1
end
if redis.call("HGET", operation, "generation") ~= generation or
   redis.call("HGET", operation, "agent_id") ~= agent_id or
   redis.call("HGET", operation, "lineage_digest") ~= lineage_digest then
    return 2
end
local status = redis.call("HGET", operation, "status")
if status == "settled" then
    return 0
end
if status ~= "reserved" then
    return 3
end

local reserved_tokens = tonumber(redis.call("HGET", operation, "token_reserved") or "-1")
local reserved_cost = tonumber(redis.call("HGET", operation, "cost_reserved_cents") or "-1")
if reserved_tokens < 0 or reserved_cost < 0 then
    return 2
end
for index = 0, lineage_count - 1 do
    local base = 2 + (index * 2)
    local agent = KEYS[base]
    if redis.call("EXISTS", agent) == 0 or
       tonumber(redis.call("HGET", agent, "token_reserved") or "-1") < reserved_tokens or
       tonumber(redis.call("HGET", agent, "cost_reserved_cents") or "-1") < reserved_cost then
        return 2
    end
end

for index = 0, lineage_count - 1 do
    local base = 2 + (index * 2)
    local agent = KEYS[base]
    local concurrency = KEYS[base + 1]
    local other_cost_reserved = tonumber(redis.call("HGET", agent, "cost_reserved_cents")) - reserved_cost
    local cost_used = tonumber(redis.call("HGET", agent, "cost_used_cents"))
    local cost_limit = tonumber(redis.call("HGET", agent, "cost_limit_cents"))
    local other_token_reserved = tonumber(redis.call("HGET", agent, "token_reserved")) - reserved_tokens
    local token_used = tonumber(redis.call("HGET", agent, "token_used"))
    local token_limit = tonumber(redis.call("HGET", agent, "token_limit"))
    if actual_cost > (cost_limit - cost_used - other_cost_reserved) then
        redis.call("HSET", agent, "cost_exhausted", 1)
    end
    if actual_tokens > (token_limit - token_used - other_token_reserved) then
        redis.call("HSET", agent, "token_exhausted", 1)
    end
    redis.call("HINCRBY", agent, "cost_reserved_cents", -reserved_cost)
    redis.call("HINCRBY", agent, "token_reserved", -reserved_tokens)
    redis.call("HINCRBY", agent, "cost_used_cents", actual_cost)
    redis.call("HINCRBY", agent, "token_used", actual_tokens)
    redis.call("ZREM", concurrency, member)
end
redis.call("HSET", operation,
    "status", "settled",
    "actual_tokens", actual_tokens,
    "actual_cost_cents", actual_cost)
return 0
`)

var cancelInvocationScript = redis.NewScript(`
local operation = KEYS[1]
local generation = ARGV[1]
local agent_id = ARGV[2]
local member = ARGV[3]
local lineage_count = tonumber(ARGV[4])
local lineage_digest = ARGV[5]

if redis.call("EXISTS", operation) == 0 then
    return 1
end
if redis.call("HGET", operation, "generation") ~= generation or
   redis.call("HGET", operation, "agent_id") ~= agent_id or
   redis.call("HGET", operation, "lineage_digest") ~= lineage_digest then
    return 2
end
local status = redis.call("HGET", operation, "status")
if status == "cancelled" or status == "settled" then
    return 0
end
if status ~= "reserved" then
    return 3
end

local reserved_tokens = tonumber(redis.call("HGET", operation, "token_reserved") or "-1")
local reserved_cost = tonumber(redis.call("HGET", operation, "cost_reserved_cents") or "-1")
if reserved_tokens < 0 or reserved_cost < 0 then
    return 2
end
for index = 0, lineage_count - 1 do
    local base = 2 + (index * 2)
    local agent = KEYS[base]
    if redis.call("EXISTS", agent) == 0 or
       tonumber(redis.call("HGET", agent, "token_reserved") or "-1") < reserved_tokens or
       tonumber(redis.call("HGET", agent, "cost_reserved_cents") or "-1") < reserved_cost then
        return 2
    end
end

for index = 0, lineage_count - 1 do
    local base = 2 + (index * 2)
    local agent = KEYS[base]
    local concurrency = KEYS[base + 1]
    redis.call("HINCRBY", agent, "cost_reserved_cents", -reserved_cost)
    redis.call("HINCRBY", agent, "token_reserved", -reserved_tokens)
    redis.call("ZREM", concurrency, member)
end
redis.call("HSET", operation, "status", "cancelled")
return 0
`)

func (s *Service) reserveInvocation(ctx context.Context, claims *Claims, reservation *InvocationReservation, leaseTTL, stateTTL time.Duration) error {
	keys := []string{workflowKey(claims.WorkflowID)}
	for _, agentID := range claims.Lineage {
		keys = append(keys,
			agentKey(claims.WorkflowID, agentID),
			invocationRPSKey(claims.WorkflowID, agentID),
			invocationRPMKey(claims.WorkflowID, agentID),
			invocationConcurrencyKey(claims.WorkflowID, agentID),
		)
	}
	keys = append(keys, invocationOperationKey(claims.WorkflowID, reservation.OperationID))
	result, err := reserveInvocationScript.Run(ctx, s.rdb, keys,
		claims.WorkflowGeneration,
		claims.RootKeyID,
		claims.AgentID,
		invocationOperationDigest(reservation.OperationID),
		reservation.ReservedTokens,
		reservation.ReservedCostCents,
		ceilMilliseconds(leaseTTL),
		ceilSeconds(stateTTL),
		len(claims.Lineage),
		invocationLineageDigest(claims.Lineage),
	).Int64()
	if err != nil {
		return fmt.Errorf("%w: reserve invocation: %v", ErrUnavailable, err)
	}
	switch result {
	case 0, 8:
		return nil
	case 1:
		return ErrRevoked
	case 2:
		return ErrInvalidToken
	case 3:
		return ErrCostBudgetExceeded
	case 4:
		return ErrTokenBudgetExceeded
	case 5:
		return ErrRPSLimitExceeded
	case 6:
		return ErrRPMLimitExceeded
	case 7:
		return ErrConcurrencyExceeded
	case 9:
		return ErrConflict
	default:
		return fmt.Errorf("%w: unknown invocation reservation result", ErrUnavailable)
	}
}

func (s *Service) finalizeInvocation(ctx context.Context, reservation *InvocationReservation, actualTokens, actualCostCents int64) error {
	keys := invocationTerminalKeys(reservation)
	result, err := finalizeInvocationScript.Run(ctx, s.rdb, keys,
		reservation.WorkflowGeneration,
		reservation.AgentID,
		invocationOperationDigest(reservation.OperationID),
		actualTokens,
		actualCostCents,
		len(reservation.Lineage),
		invocationLineageDigest(reservation.Lineage),
	).Int64()
	if err != nil {
		return fmt.Errorf("%w: finalize invocation: %v", ErrUnavailable, err)
	}
	switch result {
	case 0:
		return nil
	case 1, 2:
		return fmt.Errorf("%w: invocation reservation state missing", ErrUnavailable)
	case 3:
		return ErrConflict
	default:
		return fmt.Errorf("%w: unknown invocation finalization result", ErrUnavailable)
	}
}

func (s *Service) extendInvocation(ctx context.Context, reservation *InvocationReservation, sequence int, additionalTokens, additionalCostCents int64) error {
	keys := []string{invocationOperationKey(reservation.WorkflowID, reservation.OperationID)}
	for _, agentID := range reservation.Lineage {
		keys = append(keys, agentKey(reservation.WorkflowID, agentID))
	}
	result, err := extendInvocationScript.Run(ctx, s.rdb, keys,
		reservation.WorkflowGeneration,
		reservation.AgentID,
		sequence,
		additionalTokens,
		additionalCostCents,
		len(reservation.Lineage),
		invocationLineageDigest(reservation.Lineage),
	).Int64()
	if err != nil {
		return fmt.Errorf("%w: extend invocation: %v", ErrUnavailable, err)
	}
	switch result {
	case 0, 8:
		return nil
	case 1, 2:
		return fmt.Errorf("%w: invocation reservation state missing", ErrUnavailable)
	case 3:
		return ErrCostBudgetExceeded
	case 4:
		return ErrTokenBudgetExceeded
	case 9:
		return ErrConflict
	default:
		return fmt.Errorf("%w: unknown invocation extension result", ErrUnavailable)
	}
}

func (s *Service) cancelInvocation(ctx context.Context, reservation *InvocationReservation) error {
	keys := invocationTerminalKeys(reservation)
	result, err := cancelInvocationScript.Run(ctx, s.rdb, keys,
		reservation.WorkflowGeneration,
		reservation.AgentID,
		invocationOperationDigest(reservation.OperationID),
		len(reservation.Lineage),
		invocationLineageDigest(reservation.Lineage),
	).Int64()
	if err != nil {
		return fmt.Errorf("%w: cancel invocation: %v", ErrUnavailable, err)
	}
	switch result {
	case 0:
		return nil
	case 1, 2:
		return fmt.Errorf("%w: invocation reservation state missing", ErrUnavailable)
	case 3:
		return ErrConflict
	default:
		return fmt.Errorf("%w: unknown invocation cancellation result", ErrUnavailable)
	}
}

func invocationTerminalKeys(reservation *InvocationReservation) []string {
	keys := []string{invocationOperationKey(reservation.WorkflowID, reservation.OperationID)}
	for _, agentID := range reservation.Lineage {
		keys = append(keys,
			agentKey(reservation.WorkflowID, agentID),
			invocationConcurrencyKey(reservation.WorkflowID, agentID),
		)
	}
	return keys
}

func invocationRPSKey(workflowID, agentID string) string {
	return agentKey(workflowID, agentID) + ":invocation:rps"
}

func invocationRPMKey(workflowID, agentID string) string {
	return agentKey(workflowID, agentID) + ":invocation:rpm"
}

func invocationConcurrencyKey(workflowID, agentID string) string {
	return agentKey(workflowID, agentID) + ":invocation:concurrency"
}

func invocationOperationKey(workflowID, operationID string) string {
	return workflowKey(workflowID) + ":invocation:operation:" + invocationOperationDigest(operationID)
}

func invocationOperationDigest(operationID string) string {
	digest := sha256.Sum256([]byte("unirouter.agent-capability.invocation.v1\x00" + operationID))
	return hex.EncodeToString(digest[:])
}

func invocationLineageDigest(lineage []string) string {
	hasher := sha256.New()
	_, _ = hasher.Write([]byte("unirouter.agent-capability.lineage.v1\x00"))
	for _, agentID := range lineage {
		_, _ = hasher.Write([]byte(agentID))
		_, _ = hasher.Write([]byte{0})
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func ceilSeconds(duration time.Duration) int64 {
	seconds := duration / time.Second
	if duration%time.Second != 0 {
		seconds++
	}
	if seconds < 1 {
		return 1
	}
	return int64(seconds)
}

func ceilMilliseconds(duration time.Duration) int64 {
	milliseconds := duration / time.Millisecond
	if duration%time.Millisecond != 0 {
		milliseconds++
	}
	if milliseconds < 1 {
		return 1
	}
	return int64(milliseconds)
}
