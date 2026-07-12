//go:build integration

package workloadadmission

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestP6F21RedisMultiInstanceLeaseAndFaultIntegration(t *testing.T) {
	address := os.Getenv("REDIS_ADDR")
	if address == "" {
		address = "localhost:6379"
	}
	clients := make([]*redis.Client, 3)
	services := make([]*Service, 3)
	options := testOptions()
	options.GlobalCapacity = 6
	options.OrgCapacity = 5
	options.Stream = ClassPolicy{Weight: 3, MaxWait: 100 * time.Millisecond}
	options.FallbackTTL = 500 * time.Millisecond
	for index := range clients {
		clients[index] = redis.NewClient(&redis.Options{
			Addr: address, Password: os.Getenv("REDIS_PASSWORD"), PoolSize: 8,
			ContextTimeoutEnabled: true,
		})
		t.Cleanup(func() { _ = clients[index].Close() })
		var err error
		services[index], err = NewService(clients[index], options)
		if err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	if err := clients[0].Ping(ctx).Err(); err != nil {
		t.Fatalf("real Redis is required: %v", err)
	}
	for _, replicaCount := range []int{2, 3} {
		t.Run(fmt.Sprintf("replicas-%d", replicaCount), func(t *testing.T) {
			if err := clients[0].FlushDB(ctx).Err(); err != nil {
				t.Fatal(err)
			}
			var admitted atomic.Int64
			var denied atomic.Int64
			reservations := make(chan *Reservation, 30)
			var wait sync.WaitGroup
			for index := 0; index < 30; index++ {
				wait.Add(1)
				go func(index int) {
					defer wait.Done()
					reservation, _, err := services[index%replicaCount].Reserve(ctx, request(
						fmt.Sprintf("real-%d-%02d", replicaCount, index), 77, ClassInteractive, time.Minute,
					))
					if err == nil {
						admitted.Add(1)
						reservations <- reservation
						return
					}
					if errors.Is(err, ErrCapacityExceeded) {
						denied.Add(1)
						return
					}
					t.Errorf("unexpected admission error: %v", err)
				}(index)
			}
			wait.Wait()
			close(reservations)
			if admitted.Load() != 5 || denied.Load() != 25 {
				t.Fatalf("real Redis admitted=%d denied=%d, want 5/25", admitted.Load(), denied.Load())
			}
			for reservation := range reservations {
				if err := services[0].Release(ctx, reservation); err != nil {
					t.Fatal(err)
				}
			}
		})
	}

	if err := clients[0].FlushDB(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	crashOptions := testOptions()
	crashOptions.GlobalCapacity = 1
	crashOptions.OrgCapacity = 1
	crashOptions.Stream = ClassPolicy{Weight: 1, MaxWait: 50 * time.Millisecond}
	crashOptions.LeaseGrace = 10 * time.Millisecond
	crashed, err := NewService(clients[0], crashOptions)
	if err != nil {
		t.Fatal(err)
	}
	recovery, err := NewService(clients[1], crashOptions)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := crashed.Reserve(ctx, request("real-crash", 77, ClassInteractive, 40*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(70 * time.Millisecond)
	recovered, _, err := recovery.Reserve(ctx, request("real-recovered", 77, ClassInteractive, time.Minute))
	if err != nil {
		t.Fatalf("expired real Redis lease retained capacity: %v", err)
	}
	if err := recovery.Release(ctx, recovered); err != nil {
		t.Fatal(err)
	}

	testP6F21SlowRedisFailsWithinBound(t)
}

func testP6F21SlowRedisFailsWithinBound(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	accepted := make(chan net.Conn, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr == nil {
			accepted <- connection
		}
	}()
	client := redis.NewClient(&redis.Options{
		Addr: listener.Addr().String(), DialTimeout: 20 * time.Millisecond,
		ContextTimeoutEnabled: true, MaxRetries: -1,
	})
	t.Cleanup(func() { _ = client.Close() })
	options := testOptions()
	options.StoreTimeout = 20 * time.Millisecond
	service, err := NewService(client, options)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, decision, err := service.Reserve(context.Background(), request("slow-store", 77, ClassInteractive, time.Minute))
	if !errors.Is(err, ErrStateUnavailable) || decision.Reason != ReasonStateUnavailable {
		t.Fatalf("slow Redis err=%v decision=%#v", err, decision)
	}
	if elapsed := time.Since(started); elapsed > 150*time.Millisecond {
		t.Fatalf("slow Redis exceeded bounded failure latency: %v", elapsed)
	}
	select {
	case connection := <-accepted:
		_ = connection.Close()
	default:
	}
}
