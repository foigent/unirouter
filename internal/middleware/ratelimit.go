package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/redis/go-redis/v9"
	"github.com/uniroute/unirouter/internal/accessscope"
	"github.com/uniroute/unirouter/internal/errdocs"
)

const (
	rateLimitQPSExceeded      int64 = 1
	rateLimitRPMExceeded      int64 = 2
	rateLimitGrantQPSExceeded int64 = 3
	rateLimitGrantRPMExceeded int64 = 4
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

// grantRateLimitScript atomically admits against both the logical Credential
// family and its live Access Grant. Checking every window before writing any
// of them prevents a rejected aggregate Grant admission from consuming a
// credential-local slot (and vice versa).
var grantRateLimitScript = redis.NewScript(`
local now = redis.call("TIME")
local now_nanoseconds = (tonumber(now[1]) * 1000000000) + (tonumber(now[2]) * 1000)
local qps_window_start = now_nanoseconds - 1000000000
local rpm_window_start = now_nanoseconds - 60000000000

redis.call("ZREMRANGEBYSCORE", KEYS[1], "-inf", qps_window_start)
redis.call("ZREMRANGEBYSCORE", KEYS[2], "-inf", rpm_window_start)
redis.call("ZREMRANGEBYSCORE", KEYS[3], "-inf", qps_window_start)
redis.call("ZREMRANGEBYSCORE", KEYS[4], "-inf", rpm_window_start)

if redis.call("ZCARD", KEYS[1]) >= tonumber(ARGV[1]) then return 1 end
if redis.call("ZCARD", KEYS[2]) >= tonumber(ARGV[2]) then return 2 end
if redis.call("ZCARD", KEYS[3]) >= tonumber(ARGV[3]) then return 3 end
if redis.call("ZCARD", KEYS[4]) >= tonumber(ARGV[4]) then return 4 end

redis.call("ZADD", KEYS[1], now_nanoseconds, ARGV[5])
redis.call("EXPIRE", KEYS[1], 2)
redis.call("ZADD", KEYS[2], now_nanoseconds, ARGV[5])
redis.call("EXPIRE", KEYS[2], 120)
redis.call("ZADD", KEYS[3], now_nanoseconds, ARGV[5])
redis.call("EXPIRE", KEYS[3], 2)
redis.call("ZADD", KEYS[4], now_nanoseconds, ARGV[5])
redis.call("EXPIRE", KEYS[4], 120)
return 0
`)

// RateLimit returns middleware that enforces legacy physical-key limits or,
// for Grant-bound credentials, atomic logical-Credential plus Grant aggregate
// limits using Redis sliding windows.
func RateLimit(rdb *redis.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiKey := GetAPIKeyFromContext(r.Context())
			if apiKey == nil {
				next.ServeHTTP(w, r)
				return
			}

			rpmLimit := apiKey.RPMLimit
			qpsLimit := apiKey.QPSLimit
			if apiKey.GrantBound {
				if apiKey.AccessCredentialID == nil || apiKey.AccessGrantID == nil {
					w.Header().Set("Retry-After", "1")
					writeGatewayLimitError(w, http.StatusServiceUnavailable, "server_error", "limit_policy.state_unavailable", "Access credential rate identity is unavailable")
					return
				}
				grantRPM := accessscope.ProjectDefaultRPMLimit
				if apiKey.GrantRPMLimit != nil {
					grantRPM = *apiKey.GrantRPMLimit
				}
				grantRPS := accessscope.ProjectDefaultRPSLimit
				if apiKey.GrantRPSLimit != nil {
					grantRPS = *apiKey.GrantRPSLimit
				}
				if rpmLimit <= 0 || qpsLimit <= 0 || grantRPM <= 0 || grantRPS <= 0 {
					w.Header().Set("Retry-After", "60")
					writeGatewayLimitError(w, http.StatusTooManyRequests, "rate_limit_error", "limit_policy.zero_limit", "Access credential or Grant request limit is zero")
					return
				}
				member := uniqueMember()
				result, err := grantRateLimitScript.Run(r.Context(), rdb, []string{
					"rl:credential:qps:" + fmt.Sprint(*apiKey.AccessCredentialID),
					"rl:credential:rpm:" + fmt.Sprint(*apiKey.AccessCredentialID),
					"rl:grant:qps:" + fmt.Sprint(*apiKey.AccessGrantID),
					"rl:grant:rpm:" + fmt.Sprint(*apiKey.AccessGrantID),
				}, qpsLimit, rpmLimit, grantRPS, grantRPM, member).Int64()
				if !writeRateLimitResult(w, result, err) {
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			if rpmLimit <= 0 {
				rpmLimit = 60
			}
			if qpsLimit <= 0 {
				qpsLimit = 10
			}

			// Legacy unbound credentials retain their physical-key bucket.
			result, err := rateLimitScript.Run(r.Context(), rdb, []string{
				"rl:key:qps:" + fmt.Sprint(apiKey.ID),
				"rl:key:rpm:" + fmt.Sprint(apiKey.ID),
			}, qpsLimit, rpmLimit, uniqueMember()).Int64()
			if !writeRateLimitResult(w, result, err) {
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func writeRateLimitResult(w http.ResponseWriter, result int64, err error) bool {
	if err != nil {
		w.Header().Set("Retry-After", "1")
		writeGatewayLimitError(w, http.StatusServiceUnavailable, "server_error", "limit_policy.state_unavailable", "Shared API-key rate state is temporarily unavailable")
		return false
	}

	if result == rateLimitQPSExceeded || result == rateLimitGrantQPSExceeded {
		w.Header().Set("Retry-After", "1")
		writeGatewayLimitError(w, http.StatusTooManyRequests, "rate_limit_error", "limit_policy.rps_exceeded", "RPS limit exceeded")
		return false
	}

	if result == rateLimitRPMExceeded || result == rateLimitGrantRPMExceeded {
		w.Header().Set("Retry-After", "60")
		writeGatewayLimitError(w, http.StatusTooManyRequests, "rate_limit_error", "limit_policy.rpm_exceeded", "RPM limit exceeded")
		return false
	}
	return true
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
