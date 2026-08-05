package resetexec

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestPurgeRedisDeletesOnlyReviewedProductPrefixes(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	for _, key := range []string{
		"limit:{org:1}", "rl:login:admin", "uniroute:response-cache:v2:item",
		"ur:agentcap:v2:root", "wallet:v2:1", "workload:{admission-v1}:lease",
		"unrelated:keep",
	} {
		if err := client.Set(ctx, key, "fixture", 0).Err(); err != nil {
			t.Fatal(err)
		}
	}
	before, err := ObserveRedis(ctx, client)
	if err != nil {
		t.Fatal(err)
	}
	if before.BoundToScope {
		t.Fatal("volatile Redis observation must not alter semantic scope")
	}
	deleted, err := PurgeRedis(ctx, client)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 6 {
		t.Fatalf("deleted=%d want 6", deleted)
	}
	if value, err := client.Get(ctx, "unrelated:keep").Result(); err != nil || value != "fixture" {
		t.Fatalf("unrelated Redis key changed: value=%q err=%v", value, err)
	}
}

func TestVerifyFileSHA256RejectsPermissionAndDigestDrift(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifact.json")
	if err := os.WriteFile(path, []byte("reviewed"), 0o600); err != nil {
		t.Fatal(err)
	}
	const digest = "sha256:e4f934f321eb76c9bf8b5103e0a0d9afe72d6e62ace3d3ea849790619bf7487a"
	if _, err := VerifyFileSHA256(path, digest, 32); err != nil {
		t.Fatalf("verify exact artifact: %v", err)
	}
	if _, err := VerifyFileSHA256(path, "sha256:"+strings.Repeat("0", 64), 32); err == nil {
		t.Fatal("digest drift was accepted")
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyFileSHA256(path, digest, 32); err == nil {
		t.Fatal("group/world-readable evidence was accepted")
	}
}
