package streampolicy

import (
	"testing"
	"time"
)

func TestDefaultValid(t *testing.T) {
	config := Default()
	if err := config.Validate(); err != nil {
		t.Fatalf("Default().Validate() error = %v", err)
	}
	if config.MaxSSELineBytes < 1024*1024 {
		t.Fatalf("default must admit a 1 MiB SSE fixture, got %d", config.MaxSSELineBytes)
	}
	if EnforceBufferMaxBytes != 4*1024 || EnforceBufferMaxWait != 200*time.Millisecond ||
		CheckpointInterval != time.Second || CheckpointDeliveredBytes != 64*1024 ||
		CheckpointCompletionTokens != 128 || CheckpointLeaseDuration <= CheckpointHeartbeatInterval ||
		FinalizationLeaseDuration <= CheckpointLeaseDuration ||
		ReservationGuardTokens >= ReservationExtensionTokens {
		t.Fatalf("unsafe D6-16 constants")
	}
}

func TestValidateRejectsUnsafeBounds(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "line too small", mutate: func(c *Config) { c.MaxSSELineBytes = 1024 }},
		{name: "line too large", mutate: func(c *Config) { c.MaxSSELineBytes = 17 * 1024 * 1024 }},
		{name: "write timeout zero", mutate: func(c *Config) { c.WriteIdleTimeoutSeconds = 0 }},
		{name: "heartbeat reaches write timeout", mutate: func(c *Config) { c.HeartbeatSeconds = c.WriteIdleTimeoutSeconds }},
		{name: "duration too short", mutate: func(c *Config) { c.MaxDurationSeconds = 59 }},
		{name: "duration too long", mutate: func(c *Config) { c.MaxDurationSeconds = 24*60*60 + 1 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := Default()
			test.mutate(&config)
			if err := config.Validate(); err == nil {
				t.Fatal("expected invalid policy")
			}
		})
	}
}
