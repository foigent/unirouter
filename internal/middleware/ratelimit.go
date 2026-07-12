package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/redis/go-redis/v9"
	"github.com/uniroute/unirouter/internal/errdocs"
)

const (
	rateLimitQPSExceeded int64 = 1
	rateLimitRPMExceeded int64 = 2
)

var rateLimitScript = redis.NewScript(`
local now = redis.call("TIME")
local now_nanoseconds = (tonumber(now[1]) * 1000000000) + (tonumber(now[2]) * 1000)
local qps_window_start = now_nanoseconds - 1000000000
local rpm_window_start = now_nanoseconds - 60000000000

redis.call("ZREMRANGEBYSCORE", KEYS[1], "-inf", qps_window_start)
redis.call("ZREMRANGEBYSCORE", KEYS[2], "-inf", rpm_window_start)

if redis.call("ZCARD", KEYS[1]) >= tonumber(ARGV[1]) then
    return 1
end
if redis.call("ZCARD", KEYS[2]) >= tonumber(ARGV[2]) then
    return 2
end

redis.call("ZADD", KEYS[1], now_nanoseconds, ARGV[3])
redis.call("EXPIRE", KEYS[1], 2)
redis.call("ZADD", KEYS[2], now_nanoseconds, ARGV[3])
redis.call("EXPIRE", KEYS[2], 120)
return 0
`)

// RateLimit returns middleware that enforces per-key RPM and QPS limits
// using Redis sliding window counters.
func RateLimit(rdb *redis.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiKey := GetAPIKeyFromContext(r.Context())
			if apiKey == nil {
				next.ServeHTTP(w, r)
				return
			}

			keyID := apiKey.ID

			// Use per-key limits from the API key record
			rpmLimit := apiKey.RPMLimit
			qpsLimit := apiKey.QPSLimit
			if rpmLimit <= 0 {
				rpmLimit = 60
			}
			if qpsLimit <= 0 {
				qpsLimit = 10
			}

			// Check and record both windows atomically. Redis TIME keeps all
			// instances on the same clock, while a unique member preserves every
			// request that shares the same timestamp.
			qpsKey := "rl:qps:" + fmt.Sprint(keyID)
			rpmKey := "rl:rpm:" + fmt.Sprint(keyID)
			result, err := rateLimitScript.Run(
				r.Context(),
				rdb,
				[]string{qpsKey, rpmKey},
				qpsLimit,
				rpmLimit,
				uniqueMember(),
			).Int64()
			if err != nil {
				w.Header().Set("Retry-After", "1")
				writeGatewayLimitError(w, http.StatusServiceUnavailable, "server_error", "limit_policy.state_unavailable", "Shared API-key rate state is temporarily unavailable")
				return
			}

			if result == rateLimitQPSExceeded {
				w.Header().Set("Retry-After", "1")
				writeGatewayLimitError(w, http.StatusTooManyRequests, "rate_limit_error", "limit_policy.rps_exceeded", "RPS limit exceeded")
				return
			}

			if result == rateLimitRPMExceeded {
				w.Header().Set("Retry-After", "60")
				writeGatewayLimitError(w, http.StatusTooManyRequests, "rate_limit_error", "limit_policy.rpm_exceeded", "RPM limit exceeded")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func writeGatewayLimitError(w http.ResponseWriter, status int, errorType, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"type": errorType, "code": code, "message": message,
			"doc_url": errdocs.URLForCode(code), "param": nil,
		},
	})
}

// uniqueMember generates a unique string for Redis sorted set member to avoid collisions.
func uniqueMember() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
