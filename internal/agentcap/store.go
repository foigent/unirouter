package agentcap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/uniroute/unirouter/internal/money"
)

const redisKeyPrefix = "ur:agentcap:v2"

type refreshGrant struct {
	JTI      string `json:"jti"`
	IssuedAt int64  `json:"issued_at"`
}

var registerWorkflowScript = redis.NewScript(`
if redis.call("EXISTS", KEYS[1]) == 1 then
    return 1
end
redis.call("HSET", KEYS[1],
    "schema_version", ARGV[1],
    "status", "active",
    "generation", ARGV[2],
    "root_key_id", ARGV[3],
    "root_agent_id", ARGV[4],
    "expires_at", ARGV[5],
    "max_descendants", ARGV[6],
    "descendant_count", 0)
redis.call("EXPIREAT", KEYS[1], ARGV[7])
redis.call("HSET", KEYS[2],
    "agent_id", ARGV[4],
    "parent_agent_id", "",
    "depth", 0,
    "max_children", ARGV[8],
    "max_descendants", ARGV[6],
    "children_count", 0,
    "descendant_count", 0,
    "cost_limit_micros", ARGV[9],
    "cost_reserved_micros", 0,
    "cost_used_micros", 0,
    "cost_exhausted", 0,
    "token_limit", ARGV[10],
    "token_reserved", 0,
    "token_used", 0,
    "token_exhausted", 0,
    "rpm_limit", ARGV[11],
    "rps_limit", ARGV[12],
    "concurrency_limit", ARGV[13])
redis.call("EXPIREAT", KEYS[2], ARGV[7])
return 0
`)

var registerChildScript = redis.NewScript(`
local workflow = KEYS[1]
local child = KEYS[#KEYS]
local parent = KEYS[#KEYS - 1]
if redis.call("EXISTS", workflow) == 0 then
    return 1
end
if redis.call("HGET", workflow, "status") ~= "active" or
   redis.call("HGET", workflow, "generation") ~= ARGV[1] or
   redis.call("HGET", workflow, "root_key_id") ~= ARGV[2] or
   tonumber(redis.call("HGET", workflow, "expires_at") or "0") <= tonumber(ARGV[3]) then
    return 1
end
if redis.call("EXISTS", child) == 1 then
    return 3
end
if tonumber(redis.call("HGET", workflow, "descendant_count") or "0") >=
   tonumber(redis.call("HGET", workflow, "max_descendants") or "0") then
    return 4
end
for index = 2, #KEYS - 1 do
    if redis.call("EXISTS", KEYS[index]) == 0 then
        return 2
    end
    if tonumber(redis.call("HGET", KEYS[index], "descendant_count") or "0") >=
       tonumber(redis.call("HGET", KEYS[index], "max_descendants") or "0") then
        return 4
    end
end
if redis.call("HGET", parent, "agent_id") ~= ARGV[5] then
    return 2
end
if tonumber(redis.call("HGET", parent, "children_count") or "0") >=
   tonumber(redis.call("HGET", parent, "max_children") or "0") then
    return 4
end
redis.call("HINCRBY", workflow, "descendant_count", 1)
for index = 2, #KEYS - 1 do
    redis.call("HINCRBY", KEYS[index], "descendant_count", 1)
end
redis.call("HINCRBY", parent, "children_count", 1)
redis.call("HSET", child,
    "agent_id", ARGV[4],
    "parent_agent_id", ARGV[5],
    "depth", ARGV[6],
    "max_children", ARGV[7],
    "max_descendants", ARGV[8],
    "children_count", 0,
    "descendant_count", 0,
    "cost_limit_micros", ARGV[10],
    "cost_reserved_micros", 0,
    "cost_used_micros", 0,
    "cost_exhausted", 0,
    "token_limit", ARGV[11],
    "token_reserved", 0,
    "token_used", 0,
    "token_exhausted", 0,
    "rpm_limit", ARGV[12],
    "rps_limit", ARGV[13],
    "concurrency_limit", ARGV[14])
redis.call("EXPIREAT", child, ARGV[9])
return 0
`)

var storeRefreshScript = redis.NewScript(`
local existing = redis.call("GET", KEYS[1])
if existing then
    return existing
end
redis.call("SET", KEYS[1], ARGV[1], "EX", ARGV[2])
redis.call("SET", KEYS[2], "1", "EX", ARGV[3])
return ARGV[1]
`)

var revokeWorkflowScript = redis.NewScript(`
if redis.call("EXISTS", KEYS[1]) == 0 then
    return 1
end
if redis.call("HGET", KEYS[1], "root_key_id") ~= ARGV[1] then
    return 2
end
if redis.call("HGET", KEYS[1], "status") == "revoked" then
    return 0
end
redis.call("HSET", KEYS[1],
    "status", "revoked",
    "generation", ARGV[2],
    "revoked_at", ARGV[3])
return 0
`)

func (s *Service) registerWorkflow(ctx context.Context, claims *Claims) error {
	costLimitMicros, err := capabilityCostBudgetMicros(claims.Limits.CostBudgetCents)
	if err != nil {
		return err
	}
	stateExpiresAt := time.Unix(claims.WorkflowExpiresAt, 0).Add(s.tokenTTL).Unix()
	result, err := registerWorkflowScript.Run(ctx, s.rdb,
		[]string{workflowKey(claims.WorkflowID), agentKey(claims.WorkflowID, claims.AgentID)},
		SchemaVersion,
		claims.WorkflowGeneration,
		claims.RootKeyID,
		claims.RootAgentID,
		claims.WorkflowExpiresAt,
		claims.Limits.MaxDescendants,
		stateExpiresAt,
		claims.Limits.MaxChildren,
		costLimitMicros,
		claims.Limits.TokenBudget,
		claims.Limits.RPMLimit,
		claims.Limits.RPSLimit,
		claims.Limits.ConcurrencyLimit,
	).Int64()
	if err != nil {
		return fmt.Errorf("%w: register workflow: %v", ErrUnavailable, err)
	}
	if result != 0 {
		return fmt.Errorf("%w: workflow identity collision", ErrConflict)
	}
	return nil
}

func (s *Service) registerChild(ctx context.Context, parent, child *Claims) error {
	costLimitMicros, err := capabilityCostBudgetMicros(child.Limits.CostBudgetCents)
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(parent.Lineage)+2)
	keys = append(keys, workflowKey(parent.WorkflowID))
	for _, ancestorID := range parent.Lineage {
		keys = append(keys, agentKey(parent.WorkflowID, ancestorID))
	}
	keys = append(keys, agentKey(parent.WorkflowID, child.AgentID))
	stateExpiresAt := time.Unix(parent.WorkflowExpiresAt, 0).Add(s.tokenTTL).Unix()
	result, err := registerChildScript.Run(ctx, s.rdb, keys,
		parent.WorkflowGeneration,
		parent.RootKeyID,
		s.now().Unix(),
		child.AgentID,
		parent.AgentID,
		child.Depth,
		child.Limits.MaxChildren,
		child.Limits.MaxDescendants,
		stateExpiresAt,
		costLimitMicros,
		child.Limits.TokenBudget,
		child.Limits.RPMLimit,
		child.Limits.RPSLimit,
		child.Limits.ConcurrencyLimit,
	).Int64()
	if err != nil {
		return fmt.Errorf("%w: register child Agent: %v", ErrUnavailable, err)
	}
	switch result {
	case 0:
		return nil
	case 1:
		return ErrRevoked
	case 2:
		return ErrInvalidToken
	case 3:
		return fmt.Errorf("%w: child Agent is already registered", ErrConflict)
	case 4:
		return fmt.Errorf("%w: parent or ancestor topology limit reached", ErrLimitExceeded)
	default:
		return fmt.Errorf("%w: unknown child registration result", ErrUnavailable)
	}
}

func capabilityCostBudgetMicros(costBudgetCents int64) (int64, error) {
	value, err := money.CentsToMicros(costBudgetCents)
	if err != nil || value <= 0 || value > MaxExactRuntimeBudget {
		return 0, fmt.Errorf("%w: cost budget exceeds the exact shared-counter range", ErrInvalidRequest)
	}
	return value, nil
}

func (s *Service) verifyState(ctx context.Context, claims *Claims) error {
	var workflowCommand *redis.SliceCmd
	var agentCommand *redis.SliceCmd
	var supersededCommand *redis.IntCmd
	_, err := s.rdb.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		workflowCommand = pipe.HMGet(ctx, workflowKey(claims.WorkflowID), "status", "generation", "root_key_id", "expires_at")
		agentCommand = pipe.HMGet(ctx, agentKey(claims.WorkflowID, claims.AgentID), "agent_id", "parent_agent_id", "depth")
		supersededCommand = pipe.Exists(ctx, supersededKey(claims.ID))
		return nil
	})
	if err != nil {
		return fmt.Errorf("%w: verify capability state: %v", ErrUnavailable, err)
	}
	workflowValues, err := workflowCommand.Result()
	if err != nil || len(workflowValues) != 4 || anyNil(workflowValues) {
		return ErrRevoked
	}
	if stringValue(workflowValues[0]) != "active" ||
		stringValue(workflowValues[1]) != claims.WorkflowGeneration ||
		stringValue(workflowValues[2]) != strconv.FormatInt(claims.RootKeyID, 10) {
		return ErrRevoked
	}
	expiresAt, err := strconv.ParseInt(stringValue(workflowValues[3]), 10, 64)
	if err != nil || expiresAt <= s.now().Unix() || expiresAt != claims.WorkflowExpiresAt {
		return ErrRevoked
	}
	agentValues, err := agentCommand.Result()
	if err != nil || len(agentValues) != 3 || anyNil(agentValues) {
		return ErrInvalidToken
	}
	if stringValue(agentValues[0]) != claims.AgentID || stringValue(agentValues[1]) != claims.ParentAgentID ||
		stringValue(agentValues[2]) != strconv.Itoa(claims.Depth) {
		return ErrInvalidToken
	}
	if superseded, err := supersededCommand.Result(); err != nil {
		return fmt.Errorf("%w: verify token generation: %v", ErrUnavailable, err)
	} else if superseded > 0 {
		return ErrSuperseded
	}
	return nil
}

func (s *Service) loadRefreshGrant(ctx context.Context, jti string) (refreshGrant, bool, error) {
	encoded, err := s.rdb.Get(ctx, refreshKey(jti)).Result()
	if errors.Is(err, redis.Nil) {
		return refreshGrant{}, false, nil
	}
	if err != nil {
		return refreshGrant{}, false, fmt.Errorf("%w: read refresh state: %v", ErrUnavailable, err)
	}
	var grant refreshGrant
	if err := json.Unmarshal([]byte(encoded), &grant); err != nil || grant.JTI == "" || grant.IssuedAt <= 0 {
		return refreshGrant{}, false, fmt.Errorf("%w: corrupt refresh grant", ErrUnavailable)
	}
	return grant, true, nil
}

func (s *Service) reserveRefreshGrant(ctx context.Context, old *Claims) (refreshGrant, error) {
	jti, err := randomID("capjti_", 16)
	if err != nil {
		return refreshGrant{}, fmt.Errorf("generate refreshed capability identity: %w", err)
	}
	grant := refreshGrant{JTI: jti, IssuedAt: s.now().UTC().Truncate(time.Second).Unix()}
	encoded, err := json.Marshal(grant)
	if err != nil {
		return refreshGrant{}, fmt.Errorf("encode refresh grant: %w", err)
	}
	supersededTTL := int64(old.ExpiresAt.Time.Add(5 * time.Second).Sub(s.now()).Seconds())
	if supersededTTL < 1 {
		supersededTTL = 1
	}
	stored, err := storeRefreshScript.Run(ctx, s.rdb,
		[]string{refreshKey(old.ID), supersededKey(old.ID)},
		string(encoded),
		supersededTTL,
		supersededTTL,
	).Text()
	if err != nil {
		return refreshGrant{}, fmt.Errorf("%w: store refresh state: %v", ErrUnavailable, err)
	}
	if err := json.Unmarshal([]byte(stored), &grant); err != nil || grant.JTI == "" || grant.IssuedAt <= 0 {
		return refreshGrant{}, fmt.Errorf("%w: corrupt stored refresh grant", ErrUnavailable)
	}
	return grant, nil
}

func (s *Service) revokeWorkflow(ctx context.Context, workflowID string, rootKeyID int64, generation string, revokedAt time.Time) error {
	result, err := revokeWorkflowScript.Run(ctx, s.rdb,
		[]string{workflowKey(workflowID)},
		rootKeyID,
		generation,
		revokedAt.Unix(),
	).Int64()
	if err != nil {
		return fmt.Errorf("%w: revoke workflow: %v", ErrUnavailable, err)
	}
	switch result {
	case 0:
		return nil
	case 1:
		return fmt.Errorf("%w: workflow not found", ErrInvalidRequest)
	case 2:
		return ErrInvalidToken
	default:
		return fmt.Errorf("%w: unknown revocation result", ErrUnavailable)
	}
}

func workflowKey(workflowID string) string {
	return redisKeyPrefix + ":{" + workflowID + "}:workflow"
}

func agentKey(workflowID, agentID string) string {
	digest := sha256.Sum256([]byte(agentID))
	return workflowKey(workflowID) + ":agent:" + hex.EncodeToString(digest[:])
}

func refreshKey(jti string) string {
	return redisKeyPrefix + ":{refresh:" + jti + "}:grant"
}

func supersededKey(jti string) string {
	return redisKeyPrefix + ":{refresh:" + jti + "}:superseded"
}

func anyNil(values []interface{}) bool {
	for _, value := range values {
		if value == nil {
			return true
		}
	}
	return false
}

func stringValue(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		return fmt.Sprint(value)
	}
}
