package streampolicy

import (
	"fmt"
	"time"
)

const Version = "stream-transport.v1"

const (
	minSSELineBytes          = 64 * 1024
	maxSSELineBytes          = 16 * 1024 * 1024
	minWriteIdleSeconds      = 1
	maxWriteIdleSeconds      = 300
	minHeartbeatSeconds      = 1
	minStreamDurationSeconds = 60
	maxStreamDurationSeconds = 24 * 60 * 60
)

// Config is the immutable transport policy used by one gateway process.
// Financial reservation extension and content guardrails are separate policy
// layers; this contract only bounds SSE framing and socket lifetime behavior.
type Config struct {
	MaxSSELineBytes         int `json:"max_sse_line_bytes"`
	WriteIdleTimeoutSeconds int `json:"write_idle_timeout_seconds"`
	HeartbeatSeconds        int `json:"heartbeat_seconds"`
	MaxDurationSeconds      int `json:"max_duration_seconds"`
}

func Default() Config {
	return Config{
		MaxSSELineBytes:         2 * 1024 * 1024,
		WriteIdleTimeoutSeconds: 30,
		HeartbeatSeconds:        15,
		MaxDurationSeconds:      30 * 60,
	}
}

func (c Config) Validate() error {
	if c.MaxSSELineBytes < minSSELineBytes || c.MaxSSELineBytes > maxSSELineBytes {
		return fmt.Errorf("max SSE line bytes must be between %d and %d", minSSELineBytes, maxSSELineBytes)
	}
	if c.WriteIdleTimeoutSeconds < minWriteIdleSeconds || c.WriteIdleTimeoutSeconds > maxWriteIdleSeconds {
		return fmt.Errorf("write idle timeout seconds must be between %d and %d", minWriteIdleSeconds, maxWriteIdleSeconds)
	}
	if c.HeartbeatSeconds < minHeartbeatSeconds || c.HeartbeatSeconds >= c.WriteIdleTimeoutSeconds {
		return fmt.Errorf("heartbeat seconds must be between %d and write idle timeout seconds - 1", minHeartbeatSeconds)
	}
	if c.MaxDurationSeconds < minStreamDurationSeconds || c.MaxDurationSeconds > maxStreamDurationSeconds {
		return fmt.Errorf("max stream duration seconds must be between %d and %d", minStreamDurationSeconds, maxStreamDurationSeconds)
	}
	return nil
}

func (c Config) WriteIdleTimeout() time.Duration {
	return time.Duration(c.WriteIdleTimeoutSeconds) * time.Second
}

func (c Config) HeartbeatInterval() time.Duration {
	return time.Duration(c.HeartbeatSeconds) * time.Second
}

func (c Config) MaxDuration() time.Duration {
	return time.Duration(c.MaxDurationSeconds) * time.Second
}
