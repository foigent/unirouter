package workloadadmission

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func testOptions() Options {
	options := DefaultOptions()
	options.GlobalCapacity = 8
	options.OrgCapacity = 8
	options.GlobalShortReserve = 0
	options.OrgShortReserve = 0
	options.Interactive = ClassPolicy{Weight: 1, MaxWait: 20 * time.Millisecond}
	options.Stream = ClassPolicy{Weight: 4, MaxWait: 100 * time.Millisecond}
	options.Embedding = ClassPolicy{Weight: 1, MaxWait: 20 * time.Millisecond}
	options.PollInterval = time.Millisecond
	options.ActivePollWindow = 10 * time.Millisecond
	options.StoreTimeout = 50 * time.Millisecond
	options.LeaseGrace = 5 * time.Millisecond
	options.FallbackTTL = 500 * time.Millisecond
	options.MaxQueueDepth = 128
	options.MaxOrgQueueDepth = 64
	options.MaxQueueScan = 64
	return options
}

func newTestService(t *testing.T, server *miniredis.Miniredis, options Options) (*Service, *redis.Client) {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), ContextTimeoutEnabled: true, MaxRetries: -1})
	service, err := NewService(client, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return service, client
}

func request(operation string, orgID int64, class string, ttl time.Duration) Request {
	return Request{OperationID: operation, OrgID: orgID, Class: class, LeaseTTL: ttl}
}

func TestP6F21ReplicaInvariantWeightedConcurrency(t *testing.T) {
	server := miniredis.RunT(t)
	options := testOptions()
	options.GlobalCapacity = 4
	options.OrgCapacity = 3
	options.Stream = ClassPolicy{Weight: 3, MaxWait: 100 * time.Millisecond}
	options.Interactive = ClassPolicy{Weight: 1, MaxWait: 300 * time.Millisecond}
	options.StoreTimeout = 500 * time.Millisecond
	options.ActivePollWindow = 50 * time.Millisecond
	first, _ := newTestService(t, server, options)
	second, _ := newTestService(t, server, options)
	third, _ := newTestService(t, server, options)
	services := []*Service{first, second, third}

	var admitted atomic.Int64
	var denied atomic.Int64
	var unexpected atomic.Int64
	reservations := make(chan *Reservation, 20)
	var wait sync.WaitGroup
	for index := 0; index < 20; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			reservation, _, err := services[index%len(services)].Reserve(context.Background(), request(
				fmt.Sprintf("replica-%02d", index), 7, ClassInteractive, time.Minute,
			))
			switch {
			case err == nil:
				admitted.Add(1)
				reservations <- reservation
			case errors.Is(err, ErrCapacityExceeded):
				denied.Add(1)
			default:
				unexpected.Add(1)
			}
		}(index)
	}
	wait.Wait()
	close(reservations)
	if admitted.Load() != 3 || denied.Load() != 17 || unexpected.Load() != 0 {
		t.Fatalf("admitted=%d denied=%d unexpected=%d", admitted.Load(), denied.Load(), unexpected.Load())
	}
	for reservation := range reservations {
		if err := first.Release(context.Background(), reservation); err != nil {
			t.Fatal(err)
		}
	}
}

func TestP6F21StreamWeightPreservesShortCapacity(t *testing.T) {
	server := miniredis.RunT(t)
	options := testOptions()
	options.GlobalCapacity = 10
	options.OrgCapacity = 10
	options.GlobalShortReserve = 2
	options.OrgShortReserve = 2
	options.Stream = ClassPolicy{Weight: 4, MaxWait: 20 * time.Millisecond}
	service, _ := newTestService(t, server, options)

	first, _, err := service.Reserve(context.Background(), request("stream-one", 7, ClassStream, time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := service.Reserve(context.Background(), request("stream-two", 7, ClassStream, time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, decision, err := service.Reserve(context.Background(), request("stream-three", 7, ClassStream, time.Minute)); !errors.Is(err, ErrCapacityExceeded) {
		t.Fatalf("third stream error=%v decision=%#v", err, decision)
	} else if decision.Reason != ReasonCapacityExceeded || decision.RetryAfter <= 0 || decision.Waited > 50*time.Millisecond {
		t.Fatalf("bounded denial decision=%#v", decision)
	}

	shortOne, decision, err := service.Reserve(context.Background(), request("short-one", 7, ClassInteractive, time.Minute))
	if err != nil {
		t.Fatalf("reserved short capacity was unavailable: %v", err)
	}
	if decision.GlobalUsed != 9 || decision.OrgUsed != 9 {
		t.Fatalf("weighted use=%d/%d, want 9/9", decision.GlobalUsed, decision.OrgUsed)
	}
	shortTwo, _, err := service.Reserve(context.Background(), request("short-two", 7, ClassEmbedding, time.Minute))
	if err != nil {
		t.Fatalf("second short unit was unavailable: %v", err)
	}
	for _, reservation := range []*Reservation{first, second, shortOne, shortTwo} {
		if err := service.Release(context.Background(), reservation); err != nil {
			t.Fatal(err)
		}
	}
}

func TestP6F21FairQueueEventuallyAdmitsLongWorkUnderShortContention(t *testing.T) {
	server := miniredis.RunT(t)
	options := testOptions()
	options.GlobalCapacity = 4
	options.OrgCapacity = 4
	options.Stream = ClassPolicy{Weight: 4, MaxWait: 500 * time.Millisecond}
	options.Interactive = ClassPolicy{Weight: 1, MaxWait: 500 * time.Millisecond}
	options.ActivePollWindow = 20 * time.Millisecond
	// The scenario intentionally runs one long waiter plus twelve short
	// contenders against miniredis. Under full-repository race instrumentation,
	// a 50ms fixture timeout can expire while the in-memory Lua executor is
	// serialized, turning a successful release into an unrelated store outage.
	// Keep the product defaults unchanged and give this contention fixture the
	// same 500ms store budget as its bounded fair-wait policy.
	options.StoreTimeout = 500 * time.Millisecond
	service, _ := newTestService(t, server, options)

	held := make([]*Reservation, 0, 4)
	for index := 0; index < 4; index++ {
		reservation, _, err := service.Reserve(context.Background(), request(fmt.Sprintf("held-%d", index), 7, ClassInteractive, time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, reservation)
	}

	streamResult := make(chan error, 1)
	go func() {
		reservation, _, err := service.Reserve(context.Background(), request("waiting-stream", 7, ClassStream, time.Minute))
		if err == nil {
			err = service.Release(context.Background(), reservation)
		}
		streamResult <- err
	}()
	time.Sleep(20 * time.Millisecond)

	var shortWait sync.WaitGroup
	for index := 0; index < 12; index++ {
		shortWait.Add(1)
		go func(index int) {
			defer shortWait.Done()
			reservation, _, err := service.Reserve(context.Background(), request(fmt.Sprintf("churn-%02d", index), 7, ClassInteractive, time.Minute))
			if err == nil {
				_ = service.Release(context.Background(), reservation)
			}
		}(index)
	}
	for _, reservation := range held {
		if err := service.Release(context.Background(), reservation); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case err := <-streamResult:
		if err != nil {
			t.Fatalf("long work starved behind short contention: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("long work did not receive a bounded fair turn")
	}
	shortWait.Wait()
}

func TestP6F21SaturatedOrganizationDoesNotBlockAnotherTenant(t *testing.T) {
	server := miniredis.RunT(t)
	options := testOptions()
	options.GlobalCapacity = 3
	options.OrgCapacity = 1
	options.Stream = ClassPolicy{Weight: 1, MaxWait: 100 * time.Millisecond}
	options.Interactive = ClassPolicy{Weight: 1, MaxWait: 100 * time.Millisecond}
	options.MaxQueueDepth = 3
	options.MaxOrgQueueDepth = 1
	options.MaxQueueScan = 3
	service, _ := newTestService(t, server, options)
	held, _, err := service.Reserve(context.Background(), request("org-a-held", 7, ClassInteractive, time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	blocked := make(chan error, 1)
	go func() {
		_, _, reserveErr := service.Reserve(context.Background(), request("org-a-waiting", 7, ClassInteractive, time.Minute))
		blocked <- reserveErr
	}()
	time.Sleep(10 * time.Millisecond)
	if _, decision, err := service.Reserve(context.Background(), request("org-a-overflow", 7, ClassInteractive, time.Minute)); !errors.Is(err, ErrCapacityExceeded) || decision.Reason != ReasonQueueFull {
		t.Fatalf("noisy tenant queue overflow err=%v decision=%#v", err, decision)
	}
	other, _, err := service.Reserve(context.Background(), request("org-b", 8, ClassInteractive, time.Minute))
	if err != nil {
		t.Fatalf("one saturated organization blocked another tenant: %v", err)
	}
	if err := service.Release(context.Background(), other); err != nil {
		t.Fatal(err)
	}
	if err := service.Release(context.Background(), held); err != nil {
		t.Fatal(err)
	}
	select {
	case reserveErr := <-blocked:
		if reserveErr != nil {
			t.Fatalf("waiting organization did not recover after its own release: %v", reserveErr)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("waiting organization did not complete within its bound")
	}
}

func TestP6F21QueueDepthIsBounded(t *testing.T) {
	server := miniredis.RunT(t)
	options := testOptions()
	options.GlobalCapacity = 1
	options.OrgCapacity = 1
	options.Stream = ClassPolicy{Weight: 1, MaxWait: 250 * time.Millisecond}
	options.Interactive = ClassPolicy{Weight: 1, MaxWait: 250 * time.Millisecond}
	options.MaxQueueDepth = 2
	options.MaxOrgQueueDepth = 2
	options.MaxQueueScan = 2
	service, _ := newTestService(t, server, options)
	held, _, err := service.Reserve(context.Background(), request("queue-held", 7, ClassInteractive, time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	results := make(chan error, 2)
	for index := 0; index < 2; index++ {
		go func(index int) {
			reservation, _, reserveErr := service.Reserve(context.Background(), request(fmt.Sprintf("queue-wait-%d", index), 7, ClassInteractive, time.Minute))
			if reserveErr == nil {
				reserveErr = service.Release(context.Background(), reservation)
			}
			results <- reserveErr
		}(index)
	}
	time.Sleep(20 * time.Millisecond)
	started := time.Now()
	if _, decision, err := service.Reserve(context.Background(), request("queue-overflow", 7, ClassInteractive, time.Minute)); !errors.Is(err, ErrCapacityExceeded) || decision.Reason != ReasonQueueFull {
		t.Fatalf("queue overflow err=%v decision=%#v", err, decision)
	} else if time.Since(started) > 50*time.Millisecond {
		t.Fatalf("bounded queue overflow waited too long: %v", time.Since(started))
	}
	if err := service.Release(context.Background(), held); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if err := <-results; err != nil {
			t.Fatalf("queued request %d failed after capacity release: %v", index, err)
		}
	}
}

func TestP6F21LeaseExpiryRecoversFromCrashedHolder(t *testing.T) {
	server := miniredis.RunT(t)
	options := testOptions()
	options.GlobalCapacity = 1
	options.OrgCapacity = 1
	options.Stream = ClassPolicy{Weight: 1, MaxWait: 20 * time.Millisecond}
	service, _ := newTestService(t, server, options)
	baseTime := time.Now()
	server.SetTime(baseTime)

	if _, _, err := service.Reserve(context.Background(), request("crashed", 7, ClassInteractive, 20*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	server.SetTime(baseTime.Add(30 * time.Millisecond))
	recovered, _, err := service.Reserve(context.Background(), request("after-crash", 7, ClassInteractive, time.Minute))
	if err != nil {
		t.Fatalf("expired lease retained capacity: %v", err)
	}
	if err := service.Release(context.Background(), recovered); err != nil {
		t.Fatal(err)
	}
}

func TestP6F21RedisOutageUsesOneShortLastKnownSafeAdmission(t *testing.T) {
	server := miniredis.RunT(t)
	service, _ := newTestService(t, server, testOptions())
	primed, _, err := service.Reserve(context.Background(), request("prime", 7, ClassInteractive, time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Release(context.Background(), primed); err != nil {
		t.Fatal(err)
	}
	server.Close()

	fallback, decision, err := service.Reserve(context.Background(), request("fallback", 7, ClassInteractive, time.Minute))
	if err != nil || fallback == nil || !fallback.Fallback || !decision.Fallback || decision.Reason != ReasonLastKnownSafe {
		t.Fatalf("fallback=%#v decision=%#v err=%v", fallback, decision, err)
	}
	if _, decision, err := service.Reserve(context.Background(), request("closed", 7, ClassInteractive, time.Minute)); !errors.Is(err, ErrStateUnavailable) || decision.Reason != ReasonStateUnavailable {
		t.Fatalf("second outage admission err=%v decision=%#v", err, decision)
	}
}

func TestP6F21CancelledCallerCannotConsumeEmergencyAdmission(t *testing.T) {
	server := miniredis.RunT(t)
	service, _ := newTestService(t, server, testOptions())
	primed, _, err := service.Reserve(context.Background(), request("cancel-prime", 7, ClassInteractive, time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Release(context.Background(), primed); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if reservation, decision, err := service.Reserve(cancelled, request("cancelled", 7, ClassInteractive, time.Minute)); !errors.Is(err, ErrStateUnavailable) || reservation != nil || decision.Fallback {
		t.Fatalf("cancelled caller reservation=%#v decision=%#v err=%v", reservation, decision, err)
	}
	server.Close()
	if reservation, decision, err := service.Reserve(context.Background(), request("after-cancel", 7, ClassInteractive, time.Minute)); err != nil || reservation == nil || !reservation.Fallback || !decision.Fallback {
		t.Fatalf("cancelled caller consumed emergency lease: reservation=%#v decision=%#v err=%v", reservation, decision, err)
	}
}

func TestP6F21OperationIdentityAndClusterSlot(t *testing.T) {
	server := miniredis.RunT(t)
	service, _ := newTestService(t, server, testOptions())
	first, _, err := service.Reserve(context.Background(), request("same", 7, ClassInteractive, time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	replayed, _, err := service.Reserve(context.Background(), request("same", 7, ClassInteractive, time.Minute))
	if err != nil || !replayed.Idempotent {
		t.Fatalf("replay=%#v err=%v", replayed, err)
	}
	if _, _, err := service.Reserve(context.Background(), request("same", 8, ClassInteractive, time.Minute)); !errors.Is(err, ErrConflict) {
		t.Fatalf("cross-org rebound=%v, want ErrConflict", err)
	}
	if err := service.Release(context.Background(), first); err != nil {
		t.Fatal(err)
	}

	keys := runtimeKeys(7)
	tag := clusterTag(keys[0])
	if tag == "" {
		t.Fatal("missing Redis Cluster hash tag")
	}
	for _, key := range keys[1:] {
		if clusterTag(key) != tag {
			t.Fatalf("key %q does not share tag %q", key, tag)
		}
	}
}

func TestP6F21MixedConfigurationFailsClosedUntilOldLeasesDrain(t *testing.T) {
	server := miniredis.RunT(t)
	firstOptions := testOptions()
	first, _ := newTestService(t, server, firstOptions)
	held, _, err := first.Reserve(context.Background(), request("old-config", 7, ClassInteractive, time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	secondOptions := firstOptions
	secondOptions.GlobalCapacity++
	second, _ := newTestService(t, server, secondOptions)
	if _, decision, err := second.Reserve(context.Background(), request("mixed-config", 7, ClassInteractive, time.Minute)); !errors.Is(err, ErrConfigConflict) || decision.Reason != ReasonConfigConflict {
		t.Fatalf("mixed configuration err=%v decision=%#v", err, decision)
	}
	if err := first.Release(context.Background(), held); err != nil {
		t.Fatal(err)
	}
	newReservation, _, err := second.Reserve(context.Background(), request("new-config", 7, ClassInteractive, time.Minute))
	if err != nil {
		t.Fatalf("new configuration did not activate after drain: %v", err)
	}
	if err := second.Release(context.Background(), newReservation); err != nil {
		t.Fatal(err)
	}
}

func clusterTag(key string) string {
	start := -1
	for index, value := range key {
		if value == '{' {
			start = index + 1
		}
		if value == '}' && start > 0 {
			return key[start:index]
		}
	}
	return ""
}
