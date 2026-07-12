package limitpolicy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	storeAllowed int64 = iota
	storeRPSExceeded
	storeRPMExceeded
	storeTPMExceeded
	storeConcurrencyExceeded
	storeIdempotent = 8
	storeConflict   = 9
)

// Each invocation uses one hash tag derived from the policy population. A
// single policy therefore remains Redis Cluster compatible even when it uses
// rate, token, concurrency, operation and emergency-lease keys together.
var reserveScript = redis.NewScript(`
local rps = KEYS[1]
local rpm = KEYS[2]
local tpm_events = KEYS[3]
local tpm_values = KEYS[4]
local tpm_total_key = KEYS[5]
local concurrency = KEYS[6]
local operation = KEYS[7]
local fallback_nodes = KEYS[8]

local member = ARGV[1]
local fingerprint = ARGV[2]
local rps_limit = tonumber(ARGV[3])
local rpm_limit = tonumber(ARGV[4])
local tpm_limit = tonumber(ARGV[5])
local concurrency_limit = tonumber(ARGV[6])
local requested_tokens = tonumber(ARGV[7])
local input_tokens = tonumber(ARGV[8])
local lease_ttl_ms = tonumber(ARGV[9])
local state_ttl = tonumber(ARGV[10])
local mode = ARGV[11]
local node_id = ARGV[12]
local fallback_ttl_ms = tonumber(ARGV[13])
local fallback_cap = tonumber(ARGV[14])

local now = redis.call("TIME")
local now_ms = (tonumber(now[1]) * 1000) + math.floor(tonumber(now[2]) / 1000)
local lease_expires_ms = now_ms + lease_ttl_ms
local token_cutoff = now_ms - 60000

local function prune_tokens()
    local total = tonumber(redis.call("GET", tpm_total_key) or "0")
    local expired = redis.call("ZRANGEBYSCORE", tpm_events, "-inf", token_cutoff)
    for _, expired_member in ipairs(expired) do
        local value = tonumber(redis.call("HGET", tpm_values, expired_member) or "0")
        total = total - value
        redis.call("HDEL", tpm_values, expired_member)
    end
    if #expired > 0 then
        redis.call("ZREMRANGEBYSCORE", tpm_events, "-inf", token_cutoff)
    end
    if total < 0 then total = 0 end
    redis.call("SET", tpm_total_key, total, "EX", 120)
    return total
end

redis.call("ZREMRANGEBYSCORE", rps, "-inf", now_ms - 1000)
redis.call("ZREMRANGEBYSCORE", rpm, "-inf", now_ms - 60000)
redis.call("ZREMRANGEBYSCORE", concurrency, "-inf", now_ms)
redis.call("ZREMRANGEBYSCORE", fallback_nodes, "-inf", now_ms)
local token_total = prune_tokens()

if redis.call("EXISTS", operation) == 1 then
    if redis.call("HGET", operation, "status") == "reserved" and
       redis.call("HGET", operation, "fingerprint") == fingerprint then
        local fallback_expiry = tonumber(redis.call("ZSCORE", fallback_nodes, node_id) or "0")
        return {8, 0, redis.call("ZCARD", rps), redis.call("ZCARD", rpm), token_total,
                redis.call("ZCARD", concurrency), now_ms, fallback_expiry, 0}
    end
    return {9, 0, redis.call("ZCARD", rps), redis.call("ZCARD", rpm), token_total,
            redis.call("ZCARD", concurrency), now_ms, 0, 0}
end

local rps_count = redis.call("ZCARD", rps)
local rpm_count = redis.call("ZCARD", rpm)
local concurrency_count = redis.call("ZCARD", concurrency)
local reason = 0
if rps_limit > 0 and rps_count >= rps_limit then
    reason = 1
elseif rpm_limit > 0 and rpm_count >= rpm_limit then
    reason = 2
elseif tpm_limit > 0 and token_total + requested_tokens > tpm_limit then
    reason = 3
elseif concurrency_limit > 0 and concurrency_count >= concurrency_limit then
    reason = 4
end

if reason > 0 and mode == "enforce" then
    return {reason, reason, rps_count, rpm_count, token_total, concurrency_count, now_ms, 0, 0}
end

if rps_limit > 0 then
    redis.call("ZADD", rps, now_ms, member)
    redis.call("EXPIRE", rps, 2)
    rps_count = rps_count + 1
end
if rpm_limit > 0 then
    redis.call("ZADD", rpm, now_ms, member)
    redis.call("EXPIRE", rpm, 120)
    rpm_count = rpm_count + 1
end
if tpm_limit > 0 then
    redis.call("ZADD", tpm_events, now_ms, member)
    redis.call("HSET", tpm_values, member, requested_tokens)
    redis.call("EXPIRE", tpm_events, 120)
    redis.call("EXPIRE", tpm_values, 120)
    token_total = token_total + requested_tokens
    redis.call("SET", tpm_total_key, token_total, "EX", 120)
end
if concurrency_limit > 0 then
    redis.call("ZADD", concurrency, lease_expires_ms, member)
    redis.call("EXPIRE", concurrency, state_ttl)
    concurrency_count = concurrency_count + 1
end

redis.call("HSET", operation,
    "status", "reserved",
    "fingerprint", fingerprint,
    "member", member,
    "input_tokens", input_tokens,
    "reserved_tokens", requested_tokens,
    "admitted_at_ms", now_ms,
    "lease_expires_at_ms", lease_expires_ms)
redis.call("EXPIRE", operation, state_ttl)

local fallback_expiry = 0
if fallback_cap > 0 and fallback_ttl_ms > 0 then
    local existing = redis.call("ZSCORE", fallback_nodes, node_id)
    if existing or redis.call("ZCARD", fallback_nodes) < fallback_cap then
        fallback_expiry = now_ms + fallback_ttl_ms
        redis.call("ZADD", fallback_nodes, fallback_expiry, node_id)
        redis.call("EXPIRE", fallback_nodes, math.max(2, math.ceil(fallback_ttl_ms / 1000) * 2))
    end
end

return {0, reason, rps_count, rpm_count, token_total, concurrency_count,
        now_ms, fallback_expiry, reason > 0 and 1 or 0}
`)

var terminalScript = redis.NewScript(`
local tpm_events = KEYS[1]
local tpm_values = KEYS[2]
local tpm_total_key = KEYS[3]
local concurrency = KEYS[4]
local operation = KEYS[5]

local fingerprint = ARGV[1]
local terminal = ARGV[2]
local terminal_tokens = tonumber(ARGV[3])
local tpm_limit = tonumber(ARGV[4])

local now = redis.call("TIME")
local now_ms = (tonumber(now[1]) * 1000) + math.floor(tonumber(now[2]) / 1000)
local cutoff = now_ms - 60000
local total = tonumber(redis.call("GET", tpm_total_key) or "0")
local expired = redis.call("ZRANGEBYSCORE", tpm_events, "-inf", cutoff)
for _, expired_member in ipairs(expired) do
    total = total - tonumber(redis.call("HGET", tpm_values, expired_member) or "0")
    redis.call("HDEL", tpm_values, expired_member)
end
if #expired > 0 then
    redis.call("ZREMRANGEBYSCORE", tpm_events, "-inf", cutoff)
end
if total < 0 then total = 0 end

if redis.call("EXISTS", operation) == 0 then
    return {1, total, 0}
end
if redis.call("HGET", operation, "fingerprint") ~= fingerprint then
    return {2, total, 0}
end
local status = redis.call("HGET", operation, "status")
if status == terminal then
    return {0, total, tonumber(redis.call("HGET", operation, "overrun") or "0")}
end
if status ~= "reserved" then
    return {3, total, 0}
end

local member = redis.call("HGET", operation, "member")
local current_tokens = tonumber(redis.call("HGET", tpm_values, member) or "-1")
if current_tokens >= 0 then
    total = total - current_tokens
    if terminal_tokens > 0 then
        redis.call("HSET", tpm_values, member, terminal_tokens)
        total = total + terminal_tokens
    else
        redis.call("HDEL", tpm_values, member)
        redis.call("ZREM", tpm_events, member)
    end
elseif terminal_tokens > 0 and tpm_limit > 0 then
    local settled_member = member .. ":terminal"
    redis.call("ZADD", tpm_events, now_ms, settled_member)
    redis.call("HSET", tpm_values, settled_member, terminal_tokens)
    total = total + terminal_tokens
end

if total < 0 then total = 0 end
redis.call("SET", tpm_total_key, total, "EX", 120)
redis.call("EXPIRE", tpm_events, 120)
redis.call("EXPIRE", tpm_values, 120)
redis.call("ZREM", concurrency, member)
local overrun = 0
if tpm_limit > 0 and total > tpm_limit then overrun = 1 end
redis.call("HSET", operation, "status", terminal, "terminal_tokens", terminal_tokens, "overrun", overrun)
return {0, total, overrun}
`)

type storeResult struct {
	Code           int64
	Reason         string
	Observed       Usage
	WouldDeny      bool
	FallbackWindow time.Duration
}

func (s *Service) terminalStore(ctx context.Context, item policyReservation, terminal string, terminalTokens, tpmLimit int64) (bool, error) {
	keys := operationRuntimeKeys(item.RuntimeRef, item.OperationID)
	result, err := terminalScript.Run(ctx, s.rdb, keys[2:7],
		item.Fingerprint, terminal, terminalTokens, tpmLimit).Result()
	if err != nil {
		return false, err
	}
	values, ok := result.([]interface{})
	if !ok || len(values) != 3 {
		return false, fmt.Errorf("unexpected terminal Redis result %T", result)
	}
	code, err := redisInt64(values[0])
	if err != nil {
		return false, err
	}
	overrun, parseErr := redisInt64(values[2])
	if parseErr != nil {
		return false, parseErr
	}
	switch code {
	case 0:
		return overrun == 1, nil
	case 2, 3:
		return false, ErrConflict
	default:
		return false, ErrStateUnavailable
	}
}

func runtimeKeys(runtimeRef string) []string {
	sum := sha256.Sum256([]byte(runtimeRef))
	tag := hex.EncodeToString(sum[:8])
	prefix := "limit:{" + tag + "}:"
	return []string{
		prefix + "rps",
		prefix + "rpm",
		prefix + "tpm-events",
		prefix + "tpm-values",
		prefix + "tpm-total",
		prefix + "concurrency",
		prefix + "op:" + operationMember(runtimeRef),
		prefix + "fallback-nodes",
	}
}

// operationRuntimeKeys substitutes the operation key while retaining the same
// cluster hash tag as every other key for the policy population.
func operationRuntimeKeys(runtimeRef, operationID string) []string {
	keys := runtimeKeys(runtimeRef)
	prefix := keys[0][:len(keys[0])-len("rps")]
	keys[6] = prefix + "op:" + operationMember(operationID)
	return keys
}

func operationMember(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:16])
}

func reasonForCode(code int64) string {
	switch code {
	case storeRPSExceeded:
		return ReasonRPSExceeded
	case storeRPMExceeded:
		return ReasonRPMExceeded
	case storeTPMExceeded:
		return ReasonTPMExceeded
	case storeConcurrencyExceeded:
		return ReasonConcurrency
	default:
		return ReasonAllowed
	}
}

func redisInt64(value interface{}) (int64, error) {
	switch typed := value.(type) {
	case int64:
		return typed, nil
	case string:
		return strconv.ParseInt(typed, 10, 64)
	case []byte:
		return strconv.ParseInt(string(typed), 10, 64)
	default:
		return 0, fmt.Errorf("unexpected integer type %T", value)
	}
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
