package workloadadmission

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"

	"github.com/redis/go-redis/v9"
)

const (
	storeAdmitted int64 = iota
	storeWait
	storeCapacity
	storeIdempotent
	storeConflict
	storeQueueFull
	storeConfigConflict
)

// All keys intentionally share one Redis Cluster hash tag. The global capacity
// decision is one atomic consistency domain; organization totals remain fields
// inside that bounded domain rather than replica-local counters.
func runtimeKeys(orgID int64) []string {
	prefix := "workload:{admission-v1}:"
	return []string{
		prefix + "leases",
		prefix + "lease-weights",
		prefix + "lease-orgs",
		prefix + "global-total",
		prefix + "org-totals",
		prefix + "queue:interactive",
		prefix + "queue:stream",
		prefix + "queue:embedding",
		prefix + "queue-deadlines",
		prefix + "metadata",
		prefix + "fairness",
		prefix + "fairness:org:" + strconv.FormatInt(orgID, 10),
		prefix + "config-version",
		prefix + "queue-org-counts",
	}
}

func operationMember(operationID string) string {
	sum := sha256.Sum256([]byte(operationID))
	return hex.EncodeToString(sum[:16])
}

func operationFingerprint(request Request, weight int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%s|%d", request.OperationID, request.OrgID, request.Class, weight)))
	return hex.EncodeToString(sum[:])
}

type storeResult struct {
	Code        int64
	GlobalUsed  int64
	OrgUsed     int64
	RetryMS     int64
	WaitedMS    int64
	QueueDepth  int64
	LeaseExpiry int64
}

func parseStoreResult(value interface{}) (storeResult, error) {
	values, ok := value.([]interface{})
	if !ok || len(values) != 8 {
		return storeResult{}, fmt.Errorf("unexpected workload admission Redis result %T", value)
	}
	parsed := make([]int64, len(values))
	for index := range values {
		var err error
		parsed[index], err = redisInt64(values[index])
		if err != nil {
			return storeResult{}, err
		}
	}
	return storeResult{
		Code: parsed[0], GlobalUsed: parsed[1], OrgUsed: parsed[2], RetryMS: parsed[4],
		WaitedMS: parsed[5], QueueDepth: parsed[6], LeaseExpiry: parsed[7],
	}, nil
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

var reserveScript = redis.NewScript(`
local leases = KEYS[1]
local lease_weights = KEYS[2]
local lease_orgs = KEYS[3]
local global_total_key = KEYS[4]
local org_totals = KEYS[5]
local q_interactive = KEYS[6]
local q_stream = KEYS[7]
local q_embedding = KEYS[8]
local queue_deadlines = KEYS[9]
local metadata = KEYS[10]
local fairness = KEYS[11]
local org_fairness = KEYS[12]
local config_version_key = KEYS[13]
local org_queue_counts = KEYS[14]

local member = ARGV[1]
local fingerprint = ARGV[2]
local org = ARGV[3]
local class = ARGV[4]
local weight = tonumber(ARGV[5])
local global_capacity = tonumber(ARGV[6])
local org_capacity = tonumber(ARGV[7])
local global_short_reserve = tonumber(ARGV[8])
local org_short_reserve = tonumber(ARGV[9])
local lease_ttl_ms = tonumber(ARGV[10])
local max_wait_ms = tonumber(ARGV[11])
local active_poll_ms = tonumber(ARGV[12])
local max_queue = tonumber(ARGV[13])
local max_scan = tonumber(ARGV[14])
local state_ttl = tonumber(ARGV[15])
local config_version = ARGV[16]
local max_org_queue = tonumber(ARGV[17])

local now = redis.call("TIME")
local now_ms = (tonumber(now[1]) * 1000) + math.floor(tonumber(now[2]) / 1000)

local function meta_field(m, suffix)
    return m .. ":" .. suffix
end

local function queue_for(c)
    if c == "stream" then return q_stream end
    if c == "embedding" then return q_embedding end
    return q_interactive
end

local function delete_metadata(m)
    redis.call("HDEL", metadata,
        meta_field(m, "fingerprint"), meta_field(m, "state"),
        meta_field(m, "class"), meta_field(m, "weight"),
        meta_field(m, "org"), meta_field(m, "gfinish"),
        meta_field(m, "ofinish"), meta_field(m, "queued"),
        meta_field(m, "poll"), meta_field(m, "expiry"))
end

local function decrement_org_queue(queued_org)
    if not queued_org then return end
    local count = math.max(0, tonumber(redis.call("HGET", org_queue_counts, queued_org) or "0") - 1)
    if count == 0 then redis.call("HDEL", org_queue_counts, queued_org)
    else redis.call("HSET", org_queue_counts, queued_org, count) end
end

local global_total = tonumber(redis.call("GET", global_total_key) or "0")
local expired_leases = redis.call("ZRANGEBYSCORE", leases, "-inf", now_ms)
for _, expired_member in ipairs(expired_leases) do
    local expired_weight = tonumber(redis.call("HGET", lease_weights, expired_member) or "0")
    local expired_org = redis.call("HGET", lease_orgs, expired_member)
    global_total = math.max(0, global_total - expired_weight)
    if expired_org then
        local expired_org_total = tonumber(redis.call("HGET", org_totals, expired_org) or "0")
        expired_org_total = math.max(0, expired_org_total - expired_weight)
        if expired_org_total == 0 then
            redis.call("HDEL", org_totals, expired_org)
        else
            redis.call("HSET", org_totals, expired_org, expired_org_total)
        end
    end
    redis.call("ZREM", leases, expired_member)
    redis.call("HDEL", lease_weights, expired_member)
    redis.call("HDEL", lease_orgs, expired_member)
    delete_metadata(expired_member)
end
redis.call("SET", global_total_key, global_total, "EX", state_ttl)

local expired_queue = redis.call("ZRANGEBYSCORE", queue_deadlines, "-inf", "(" .. now_ms)
for _, expired_member in ipairs(expired_queue) do
    local expired_class = redis.call("HGET", metadata, meta_field(expired_member, "class")) or "interactive"
    local expired_org = redis.call("HGET", metadata, meta_field(expired_member, "org"))
    redis.call("ZREM", queue_for(expired_class), expired_member)
    redis.call("ZREM", queue_deadlines, expired_member)
    decrement_org_queue(expired_org)
    delete_metadata(expired_member)
end

local configured_version = redis.call("GET", config_version_key)
local active_queue_depth = redis.call("ZCARD", q_interactive) + redis.call("ZCARD", q_stream) + redis.call("ZCARD", q_embedding)
if configured_version and configured_version ~= config_version and
   (redis.call("ZCARD", leases) > 0 or active_queue_depth > 0) then
    return {6, global_total, tonumber(redis.call("HGET", org_totals, org) or "0"), now_ms, 1000, 0, active_queue_depth, 0}
end
redis.call("SET", config_version_key, config_version, "EX", state_ttl)

local existing_state = redis.call("HGET", metadata, meta_field(member, "state"))
if existing_state then
    if redis.call("HGET", metadata, meta_field(member, "fingerprint")) ~= fingerprint then
        return {4, global_total, tonumber(redis.call("HGET", org_totals, org) or "0"), now_ms, 0, 0,
                redis.call("ZCARD", q_interactive) + redis.call("ZCARD", q_stream) + redis.call("ZCARD", q_embedding), 0}
    end
    if existing_state == "admitted" and redis.call("ZSCORE", leases, member) then
        local expiry = tonumber(redis.call("ZSCORE", leases, member) or "0")
        return {3, global_total, tonumber(redis.call("HGET", org_totals, org) or "0"), now_ms, 0,
                0, redis.call("ZCARD", q_interactive) + redis.call("ZCARD", q_stream) + redis.call("ZCARD", q_embedding), expiry}
    end
end

local queue_depth = redis.call("ZCARD", q_interactive) + redis.call("ZCARD", q_stream) + redis.call("ZCARD", q_embedding)
if not existing_state then
    local org_queue_depth = tonumber(redis.call("HGET", org_queue_counts, org) or "0")
    if queue_depth >= max_queue or org_queue_depth >= max_org_queue then
        return {5, global_total, tonumber(redis.call("HGET", org_totals, org) or "0"), now_ms, 1000, 0, queue_depth, 0}
    end
    local global_served = tonumber(redis.call("HGET", fairness, "served") or "0")
    local global_last_field = "last:" .. class
    local global_last = tonumber(redis.call("HGET", fairness, global_last_field) or "0")
    local global_finish = math.max(global_served, global_last) + weight
    redis.call("HSET", fairness, global_last_field, global_finish)

    local org_served_field = "served"
    local org_last_field = "last:" .. class
    local org_served = tonumber(redis.call("HGET", org_fairness, org_served_field) or "0")
    local org_last = tonumber(redis.call("HGET", org_fairness, org_last_field) or "0")
    local org_finish = math.max(org_served, org_last) + weight
    redis.call("HSET", org_fairness, org_last_field, org_finish)
    local score = math.max(global_finish, org_finish)

    redis.call("HSET", metadata,
        meta_field(member, "fingerprint"), fingerprint,
        meta_field(member, "state"), "queued",
        meta_field(member, "class"), class,
        meta_field(member, "weight"), weight,
        meta_field(member, "org"), org,
        meta_field(member, "gfinish"), global_finish,
        meta_field(member, "ofinish"), org_finish,
        meta_field(member, "queued"), now_ms,
        meta_field(member, "poll"), now_ms)
    redis.call("ZADD", queue_for(class), score, member)
    redis.call("ZADD", queue_deadlines, now_ms + max_wait_ms, member)
    redis.call("HINCRBY", org_queue_counts, org, 1)
    queue_depth = queue_depth + 1
else
    redis.call("HSET", metadata, meta_field(member, "poll"), now_ms)
end

local function best_active(only_short, require_capacity)
    local best_member = nil
    local best_score = nil
    local best_class = nil
    local best_weight = 0
    local best_org = nil
    local queues = {{q_interactive, "interactive"}, {q_stream, "stream"}, {q_embedding, "embedding"}}
    for _, entry in ipairs(queues) do
        local candidate_class = entry[2]
        if not only_short or candidate_class ~= "stream" then
            local candidates = redis.call("ZRANGE", entry[1], 0, max_scan - 1, "WITHSCORES")
            for index = 1, #candidates, 2 do
                local candidate = candidates[index]
                local candidate_score = tonumber(candidates[index + 1])
                local poll = tonumber(redis.call("HGET", metadata, meta_field(candidate, "poll")) or "0")
                if poll >= now_ms - active_poll_ms then
                    local candidate_weight = tonumber(redis.call("HGET", metadata, meta_field(candidate, "weight")) or "0")
                    local candidate_org = redis.call("HGET", metadata, meta_field(candidate, "org"))
                    local capacity_ok = true
                    if require_capacity then
                        local candidate_org_total = tonumber(redis.call("HGET", org_totals, candidate_org) or "0")
                        capacity_ok = global_total + candidate_weight <= global_capacity and
                                      candidate_org_total + candidate_weight <= org_capacity
                        if candidate_class == "stream" then
                            capacity_ok = capacity_ok and
                                global_total + candidate_weight <= global_capacity - global_short_reserve and
                                candidate_org_total + candidate_weight <= org_capacity - org_short_reserve
                        end
                    end
                    if capacity_ok then
                        if not best_member or candidate_score < best_score or
                           (candidate_score == best_score and candidate < best_member) then
                            best_member = candidate
                            best_score = candidate_score
                            best_class = candidate_class
                            best_weight = candidate_weight
                            best_org = candidate_org
                        end
                        break
                    end
                end
            end
        end
    end
    return best_member, best_score, best_class, best_weight, best_org
end

local selected, selected_score, selected_class, selected_weight, selected_org = best_active(false, false)
local selected_org_total = tonumber(redis.call("HGET", org_totals, selected_org or org) or "0")
local capacity_ok = selected and global_total + selected_weight <= global_capacity and
                    selected_org_total + selected_weight <= org_capacity
local reserve_ok = capacity_ok
if selected_class == "stream" then
    reserve_ok = reserve_ok and global_total + selected_weight <= global_capacity - global_short_reserve and
                 selected_org_total + selected_weight <= org_capacity - org_short_reserve
end

if selected and capacity_ok and not reserve_ok and selected_class == "stream" then
    local short_member, short_score, short_class, short_weight, short_org = best_active(true, true)
    if short_member then
        selected, selected_score, selected_class, selected_weight, selected_org =
            short_member, short_score, short_class, short_weight, short_org
        selected_org_total = tonumber(redis.call("HGET", org_totals, selected_org) or "0")
        capacity_ok = global_total + selected_weight <= global_capacity and
                      selected_org_total + selected_weight <= org_capacity
        reserve_ok = capacity_ok
    end
end

if selected and not capacity_ok and global_total + selected_weight <= global_capacity then
    local eligible_member, eligible_score, eligible_class, eligible_weight, eligible_org = best_active(false, true)
    if eligible_member then
        selected, selected_score, selected_class, selected_weight, selected_org =
            eligible_member, eligible_score, eligible_class, eligible_weight, eligible_org
        selected_org_total = tonumber(redis.call("HGET", org_totals, selected_org) or "0")
        capacity_ok = global_total + selected_weight <= global_capacity and
                      selected_org_total + selected_weight <= org_capacity
        reserve_ok = capacity_ok
        if selected_class == "stream" then
            reserve_ok = reserve_ok and global_total + selected_weight <= global_capacity - global_short_reserve and
                         selected_org_total + selected_weight <= org_capacity - org_short_reserve
        end
    end
end

if selected == member and reserve_ok then
    local expiry = now_ms + lease_ttl_ms
    redis.call("ZREM", queue_for(class), member)
    redis.call("ZREM", queue_deadlines, member)
    decrement_org_queue(org)
    redis.call("ZADD", leases, expiry, member)
    redis.call("HSET", lease_weights, member, weight)
    redis.call("HSET", lease_orgs, member, org)
    global_total = global_total + weight
    local org_total = tonumber(redis.call("HGET", org_totals, org) or "0") + weight
    redis.call("SET", global_total_key, global_total, "EX", state_ttl)
    redis.call("HSET", org_totals, org, org_total)
    redis.call("HSET", metadata, meta_field(member, "state"), "admitted", meta_field(member, "expiry"), expiry)
    redis.call("HSET", fairness, "served", tonumber(redis.call("HGET", metadata, meta_field(member, "gfinish")) or "0"))
    redis.call("HSET", org_fairness, "served", tonumber(redis.call("HGET", metadata, meta_field(member, "ofinish")) or "0"))
    for _, key in ipairs(KEYS) do redis.call("EXPIRE", key, state_ttl) end
    local queued_at = tonumber(redis.call("HGET", metadata, meta_field(member, "queued")) or now_ms)
    return {0, global_total, org_total, now_ms, 0, now_ms - queued_at, queue_depth - 1, expiry}
end

local deadline = tonumber(redis.call("ZSCORE", queue_deadlines, member) or now_ms)
local queued_at = tonumber(redis.call("HGET", metadata, meta_field(member, "queued")) or now_ms)
if now_ms >= deadline then
    redis.call("ZREM", queue_for(class), member)
    redis.call("ZREM", queue_deadlines, member)
    decrement_org_queue(org)
    delete_metadata(member)
    local earliest = redis.call("ZRANGE", leases, 0, 0, "WITHSCORES")
    local retry_ms = 1000
    if #earliest == 2 then retry_ms = math.max(1, math.min(1000, tonumber(earliest[2]) - now_ms)) end
    return {2, global_total, tonumber(redis.call("HGET", org_totals, org) or "0"), now_ms,
            retry_ms, now_ms - queued_at, queue_depth - 1, 0}
end

for _, key in ipairs(KEYS) do redis.call("EXPIRE", key, state_ttl) end
return {1, global_total, tonumber(redis.call("HGET", org_totals, org) or "0"), now_ms,
        math.max(1, math.min(25, deadline - now_ms)), now_ms - queued_at, queue_depth, 0}
`)

var releaseScript = redis.NewScript(`
local leases = KEYS[1]
local lease_weights = KEYS[2]
local lease_orgs = KEYS[3]
local global_total_key = KEYS[4]
local org_totals = KEYS[5]
local metadata = KEYS[10]
local config_version_key = KEYS[13]

local member = ARGV[1]
local fingerprint = ARGV[2]
local state_ttl = tonumber(ARGV[3])
local function field(suffix) return member .. ":" .. suffix end
local stored = redis.call("HGET", metadata, field("fingerprint"))
if not stored then return {0, tonumber(redis.call("GET", global_total_key) or "0"), 0} end
if stored ~= fingerprint then return {2, tonumber(redis.call("GET", global_total_key) or "0"), 0} end
if redis.call("HGET", metadata, field("state")) ~= "admitted" then return {0, tonumber(redis.call("GET", global_total_key) or "0"), 0} end

local weight = tonumber(redis.call("HGET", lease_weights, member) or "0")
local org = redis.call("HGET", lease_orgs, member)
local global_total = math.max(0, tonumber(redis.call("GET", global_total_key) or "0") - weight)
redis.call("SET", global_total_key, global_total, "EX", state_ttl)
local org_total = 0
if org then
    org_total = math.max(0, tonumber(redis.call("HGET", org_totals, org) or "0") - weight)
    if org_total == 0 then redis.call("HDEL", org_totals, org) else redis.call("HSET", org_totals, org, org_total) end
end
redis.call("ZREM", leases, member)
redis.call("HDEL", lease_weights, member)
redis.call("HDEL", lease_orgs, member)
redis.call("HDEL", metadata, field("fingerprint"), field("state"), field("class"), field("weight"),
    field("org"), field("gfinish"), field("ofinish"), field("queued"), field("poll"), field("expiry"))
if redis.call("ZCARD", leases) == 0 and redis.call("ZCARD", KEYS[6]) == 0 and
   redis.call("ZCARD", KEYS[7]) == 0 and redis.call("ZCARD", KEYS[8]) == 0 then
    redis.call("DEL", config_version_key)
end
return {0, global_total, org_total}
`)

var abandonScript = redis.NewScript(`
local q_interactive = KEYS[6]
local q_stream = KEYS[7]
local q_embedding = KEYS[8]
local queue_deadlines = KEYS[9]
local metadata = KEYS[10]
local leases = KEYS[1]
local config_version_key = KEYS[13]
local org_queue_counts = KEYS[14]
local member = ARGV[1]
local fingerprint = ARGV[2]
local function field(suffix) return member .. ":" .. suffix end
if redis.call("HGET", metadata, field("fingerprint")) ~= fingerprint or
   redis.call("HGET", metadata, field("state")) ~= "queued" then return 0 end
local class = redis.call("HGET", metadata, field("class")) or "interactive"
local org = redis.call("HGET", metadata, field("org"))
local queue = q_interactive
if class == "stream" then queue = q_stream elseif class == "embedding" then queue = q_embedding end
redis.call("ZREM", queue, member)
redis.call("ZREM", queue_deadlines, member)
if org then
    local count = math.max(0, tonumber(redis.call("HGET", org_queue_counts, org) or "0") - 1)
    if count == 0 then redis.call("HDEL", org_queue_counts, org)
    else redis.call("HSET", org_queue_counts, org, count) end
end
redis.call("HDEL", metadata, field("fingerprint"), field("state"), field("class"), field("weight"),
    field("org"), field("gfinish"), field("ofinish"), field("queued"), field("poll"), field("expiry"))
if redis.call("ZCARD", leases) == 0 and redis.call("ZCARD", q_interactive) == 0 and
   redis.call("ZCARD", q_stream) == 0 and redis.call("ZCARD", q_embedding) == 0 then
    redis.call("DEL", config_version_key)
end
return 1
`)
