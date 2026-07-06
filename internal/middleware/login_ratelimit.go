package middleware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type LoginRateLimitOptions struct {
	Enabled       bool
	Window        time.Duration
	IPRequests    int
	UserRequests  int
	MaxBodyBytes  int64
	RedisKeyScope string
}

func LoginRateLimit(rdb *redis.Client, options LoginRateLimitOptions) func(http.Handler) http.Handler {
	if options.Window <= 0 {
		options.Window = 60 * time.Second
	}
	if options.MaxBodyBytes <= 0 {
		options.MaxBodyBytes = 64 * 1024
	}
	if options.RedisKeyScope == "" {
		options.RedisKeyScope = "login"
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !options.Enabled || rdb == nil || r.Method != http.MethodPost {
				next.ServeHTTP(w, r)
				return
			}

			username := "__unknown__"
			if options.UserRequests > 0 && r.Body != nil {
				body, err := io.ReadAll(io.LimitReader(r.Body, options.MaxBodyBytes+1))
				r.Body = &replayReadCloser{
					Reader: io.MultiReader(bytes.NewReader(body), r.Body),
					closer: r.Body,
				}
				if err == nil {
					if int64(len(body)) <= options.MaxBodyBytes {
						username = loginUsername(body)
					} else {
						username = "__oversized__"
					}
				}
			}

			client := clientAddress(r.RemoteAddr)
			ctx := context.Background()
			if options.IPRequests > 0 {
				key := rateLimitKey(options.RedisKeyScope, "ip", client)
				allowed, retryAfter, err := fixedWindowAllow(ctx, rdb, key, options.IPRequests, options.Window)
				if err == nil && !allowed {
					writeRateLimitExceeded(w, retryAfter)
					return
				}
			}
			if options.UserRequests > 0 {
				key := rateLimitKey(options.RedisKeyScope, "user", client, username)
				allowed, retryAfter, err := fixedWindowAllow(ctx, rdb, key, options.UserRequests, options.Window)
				if err == nil && !allowed {
					writeRateLimitExceeded(w, retryAfter)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

func fixedWindowAllow(ctx context.Context, rdb *redis.Client, key string, limit int, window time.Duration) (bool, time.Duration, error) {
	count, err := rdb.Incr(ctx, key).Result()
	if err != nil {
		return true, 0, err
	}
	if count == 1 {
		if err := rdb.Expire(ctx, key, window).Err(); err != nil {
			return true, 0, err
		}
	}
	if count <= int64(limit) {
		return true, 0, nil
	}
	ttl, err := rdb.TTL(ctx, key).Result()
	if err != nil || ttl <= 0 {
		ttl = window
	}
	return false, ttl, nil
}

func loginUsername(body []byte) string {
	var req struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return "__invalid__"
	}
	username := strings.ToLower(strings.TrimSpace(req.Username))
	if username == "" {
		return "__unknown__"
	}
	return username
}

func clientAddress(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil && host != "" {
		return host
	}
	if remoteAddr == "" {
		return "__unknown__"
	}
	return remoteAddr
}

func rateLimitKey(scope string, parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(append([]string{scope}, parts...), "\x00")))
	return fmt.Sprintf("rl:%s:%s", scope, hex.EncodeToString(sum[:]))
}

func writeRateLimitExceeded(w http.ResponseWriter, retryAfter time.Duration) {
	seconds := int(retryAfter.Round(time.Second).Seconds())
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After", fmt.Sprint(seconds))
	w.WriteHeader(http.StatusTooManyRequests)
	w.Write([]byte(`{"error":{"type":"rate_limit_exceeded","message":"Too many login attempts"}}`))
}

type replayReadCloser struct {
	io.Reader
	closer io.Closer
}

func (r *replayReadCloser) Close() error {
	return r.closer.Close()
}
