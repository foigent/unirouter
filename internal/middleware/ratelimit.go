package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

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

			ctx := context.Background()
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

			now := time.Now()
			nowNano := float64(now.UnixNano())

			// Check QPS (sliding window, 1 second)
			qpsKey := "rl:qps:" + fmt.Sprint(keyID)
			qpsWindowStart := now.Add(-1 * time.Second)
			qpsWindowStartNano := float64(qpsWindowStart.UnixNano())

			pipe := rdb.Pipeline()
			qpsCountCmd := pipe.ZCount(ctx, qpsKey,
				strconv.FormatFloat(qpsWindowStartNano, 'f', 0, 64),
				strconv.FormatFloat(nowNano, 'f', 0, 64))
			pipe.ZRemRangeByScore(ctx, qpsKey, "-inf",
				strconv.FormatFloat(qpsWindowStartNano, 'f', 0, 64))
			pipe.ZAdd(ctx, qpsKey, redis.Z{Score: nowNano, Member: uniqueMember()})
			pipe.Expire(ctx, qpsKey, 2*time.Second)
			_, err := pipe.Exec(ctx)
			if err != nil {
				// On Redis error, allow the request (fail-open)
				next.ServeHTTP(w, r)
				return
			}

			if qpsCountCmd.Val() > int64(qpsLimit) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte(`{"error":{"type":"rate_limit_exceeded","message":"QPS limit exceeded"}}`))
				return
			}

			// Check RPM (sliding window, 60 seconds)
			rpmKey := "rl:rpm:" + fmt.Sprint(keyID)
			rpmWindowStart := now.Add(-60 * time.Second)
			rpmWindowStartNano := float64(rpmWindowStart.UnixNano())

			pipe2 := rdb.Pipeline()
			rpmCountCmd := pipe2.ZCount(ctx, rpmKey,
				strconv.FormatFloat(rpmWindowStartNano, 'f', 0, 64),
				strconv.FormatFloat(nowNano, 'f', 0, 64))
			pipe2.ZRemRangeByScore(ctx, rpmKey, "-inf",
				strconv.FormatFloat(rpmWindowStartNano, 'f', 0, 64))
			pipe2.ZAdd(ctx, rpmKey, redis.Z{Score: nowNano, Member: uniqueMember()})
			pipe2.Expire(ctx, rpmKey, 120*time.Second)
			_, err = pipe2.Exec(ctx)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			if rpmCountCmd.Val() > int64(rpmLimit) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte(`{"error":{"type":"rate_limit_exceeded","message":"RPM limit exceeded"}}`))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// uniqueMember generates a unique string for Redis sorted set member to avoid collisions.
func uniqueMember() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
