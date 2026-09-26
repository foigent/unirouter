package workloadadmission

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
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
	// These are functional miniredis fixture budgets, not product defaults.
	// Race/coverage instrumentation makes the emulator's Go Lua VM slower than
	// Redis; real-Redis integration owns the store/fault and multi-instance
	// assertions.
	options.Interactive = ClassPolicy{Weight: 1, MaxWait: 100 * time.Millisecond}
	options.Stream = ClassPolicy{Weight: 4, MaxWait: 250 * time.Millisecond}
	options.Embedding = ClassPolicy{Weight: 1, MaxWait: 100 * time.Millisecond}
	options.PollInterval = time.Millisecond
	options.ActivePollWindow = 10 * time.Millisecond
	options.StoreTimeout = 500 * time.Millisecond
	options.LeaseGrace = 5 * time.Millisecond
	options.MaxQueueDepth = 128
	options.MaxOrgQueueDepth = 64
	options.MaxQueueScan = 64
	return options
}

func newTestService(t *testing.T, server *miniredis.Miniredis, options Options) (*Service, *redis.Client) {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), ContextTimeoutEnabled: true, MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	loadCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for _, script := range []*redis.Script{reserveScript, releaseScript, abandonScript} {
		if err := script.Load(loadCtx, client).Err(); err != nil {
			t.Fatalf("preload miniredis script: %v", err)
		}
	}
	service, err := NewService(client, options)
	if err != nil {
		t.Fatal(err)
	}
	return service, client
}

func request(operation string, orgID int64, class string, ttl time.Duration) Request {
	return Request{OperationID: operation, OrgID: orgID, Class: class, LeaseTTL: ttl}
}

func TestP6F21StreamWeightPreservesShortCapacity(t *testing.T) {
	server := miniredis.RunT(t)
	options := testOptions()
	options.GlobalCapacity = 10
	options.OrgCapacity = 10
	options.GlobalShortReserve = 2
	options.OrgShortReserve = 2
	options.Stream = ClassPolicy{Weight: 4, MaxWait: 100 * time.Millisecond}
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
	} else if decision.Reason != ReasonCapacityExceeded || decision.RetryAfter <= 0 || decision.Waited > 150*time.Millisecond {
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
	if _, decision, err := service.Reserve(context.Background(), request("queue-overflow", 7, ClassInteractive, time.Minute)); !errors.Is(err, ErrCapacityExceeded) || decision.Reason != ReasonQueueFull {
		t.Fatalf("queue overflow err=%v decision=%#v", err, decision)
	} else if decision.Waited != 0 {
		t.Fatalf("queue overflow was not immediate in Redis decision time: %#v", decision)
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

func TestP6F21RedisOutageFailsClosedAfterHealthyAdmission(t *testing.T) {
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

	reservation, decision, err := service.Reserve(context.Background(), request("outage", 7, ClassInteractive, time.Minute))
	if !errors.Is(err, ErrStateUnavailable) || reservation != nil || decision.Decision != "deny" || decision.Reason != ReasonStateUnavailable || decision.Fallback {
		t.Fatalf("outage reservation=%#v decision=%#v err=%v", reservation, decision, err)
	}
	if _, decision, err := service.Reserve(context.Background(), request("closed", 7, ClassInteractive, time.Minute)); !errors.Is(err, ErrStateUnavailable) || decision.Reason != ReasonStateUnavailable {
		t.Fatalf("subsequent outage admission err=%v decision=%#v", err, decision)
	}
}

func TestP6F21RedisOutageFailsClosedForEveryClassAndOrganization(t *testing.T) {
	server := miniredis.RunT(t)
	service, _ := newTestService(t, server, testOptions())
	primed, _, err := service.Reserve(context.Background(), request("prime-all-classes", 7, ClassInteractive, time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Release(context.Background(), primed); err != nil {
		t.Fatal(err)
	}
	server.Close()

	fixtures := []struct {
		class string
		orgID int64
	}{
		{class: ClassInteractive, orgID: 7},
		{class: ClassStream, orgID: 7},
		{class: ClassEmbedding, orgID: 8},
	}
	for index, fixture := range fixtures {
		reservation, decision, err := service.Reserve(context.Background(), request(fmt.Sprintf("outage-class-%d", index), fixture.orgID, fixture.class, time.Minute))
		if !errors.Is(err, ErrStateUnavailable) || reservation != nil || decision.Decision != "deny" || decision.Reason != ReasonStateUnavailable || decision.Fallback {
			t.Fatalf("class=%s org=%d reservation=%#v decision=%#v err=%v", fixture.class, fixture.orgID, reservation, decision, err)
		}
	}
}

func TestP6F21RedisOutageFailsClosedWithoutUntrackedAdmissionAcrossReplicas(t *testing.T) {
	server := miniredis.RunT(t)
	options := testOptions()
	options.GlobalCapacity = 12
	options.OrgCapacity = 12
	services := make([]*Service, 3)
	for index := range services {
		services[index], _ = newTestService(t, server, options)
		primed, _, err := services[index].Reserve(context.Background(), request(fmt.Sprintf("prime-%d", index), 7, ClassInteractive, time.Minute))
		if err != nil {
			t.Fatalf("prime replica %d: %v", index, err)
		}
		if err := services[index].Release(context.Background(), primed); err != nil {
			t.Fatalf("release prime replica %d: %v", index, err)
		}
	}

	for index := 0; index < 12; index++ {
		if _, _, err := services[0].Reserve(context.Background(), request(fmt.Sprintf("tracked-%02d", index), 7, ClassInteractive, time.Minute)); err != nil {
			t.Fatalf("fill tracked capacity at unit %d: %v", index, err)
		}
	}
	server.Close()

	for index, service := range services {
		reservation, decision, err := service.Reserve(context.Background(), request(fmt.Sprintf("outage-stream-%d", index), 7, ClassStream, time.Minute))
		if !errors.Is(err, ErrStateUnavailable) || reservation != nil || decision.Decision != "deny" || decision.Reason != ReasonStateUnavailable || decision.Fallback {
			t.Fatalf("replica %d admitted untracked weighted work: reservation=%#v decision=%#v err=%v", index, reservation, decision, err)
		}
	}
}

func TestP6F21CancelledCallerAndSubsequentOutageFailClosed(t *testing.T) {
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
	if reservation, decision, err := service.Reserve(context.Background(), request("after-cancel", 7, ClassInteractive, time.Minute)); !errors.Is(err, ErrStateUnavailable) || reservation != nil || decision.Decision != "deny" || decision.Reason != ReasonStateUnavailable || decision.Fallback {
		t.Fatalf("subsequent outage was not fail-closed: reservation=%#v decision=%#v err=%v", reservation, decision, err)
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

// cc13ReserveHook controls only the local emulator's Redis clock and the order
// of real script calls. It does not replace a Lua result or change the service.
type cc13ReserveHook struct {
	after func(context.Context, redis.Cmder) error
}

func (h cc13ReserveHook) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h cc13ReserveHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func (h cc13ReserveHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if err := next(ctx, cmd); err != nil {
			return err
		}
		return h.after(ctx, cmd)
	}
}

func TestCC13MaxWaitCannotRestartAfterQueueExpiry(t *testing.T) {
	cases := []struct {
		name                              string
		elapsed                           time.Duration
		peerCleanup, release, wantAllowed bool
	}{
		{name: "before_deadline_capacity_released", elapsed: 49 * time.Millisecond, release: true, wantAllowed: true},
		{name: "equal_deadline_capacity_held", elapsed: 50 * time.Millisecond},
		{name: "equal_deadline_capacity_released", elapsed: 50 * time.Millisecond, release: true, wantAllowed: true},
		{name: "self_cleanup_capacity_held", elapsed: 51 * time.Millisecond},
		{name: "self_cleanup_capacity_released", elapsed: 51 * time.Millisecond, release: true},
		{name: "peer_cleanup_capacity_held", elapsed: 51 * time.Millisecond, peerCleanup: true},
		{name: "peer_cleanup_capacity_released", elapsed: 51 * time.Millisecond, peerCleanup: true, release: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := miniredis.RunT(t)
			base := time.Unix(1800000000, 0)
			server.SetTime(base)
			options := testOptions()
			options.GlobalCapacity, options.OrgCapacity = 2, 1
			options.Interactive = ClassPolicy{Weight: 1, MaxWait: 50 * time.Millisecond}
			options.Stream = ClassPolicy{Weight: 1, MaxWait: 50 * time.Millisecond}
			options.Embedding = ClassPolicy{Weight: 1, MaxWait: 50 * time.Millisecond}
			service, client := newTestService(t, server, options)
			peer, observer := newTestService(t, server, options)
			holder, _, err := service.Reserve(context.Background(), request("cc13-holder", 7, ClassInteractive, time.Minute))
			if err != nil {
				t.Fatal(err)
			}
			target := request("cc13-waiter", 7, ClassInteractive, time.Minute)
			member := operationMember(target.OperationID)
			keys := runtimeKeys(target.OrgID)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			calls := 0
			var observed []storeResult
			var deadlines []int64
			var fairnessBeforeRetry string
			peerRemoved := false
			client.AddHook(cc13ReserveHook{after: func(_ context.Context, cmd redis.Cmder) error {
				args := cmd.Args()
				if cmd.Name() != "evalsha" || len(args) <= 3+len(keys) || fmt.Sprint(args[1]) != reserveScript.Hash() || fmt.Sprint(args[3+len(keys)]) != member {
					return nil
				}
				calls++
				raw, err := cmd.(*redis.Cmd).Result()
				if err != nil {
					return err
				}
				result, err := parseStoreResult(raw)
				if err != nil {
					return err
				}
				observed = append(observed, result)
				deadline, err := observer.ZScore(context.Background(), keys[8], member).Result()
				if err != nil && !errors.Is(err, redis.Nil) {
					return err
				}
				deadlines = append(deadlines, int64(deadline))
				if calls == 1 {
					if result.Code != storeWait || result.WaitedMS != 0 {
						return fmt.Errorf("fixture first call was not newly queued: %#v", result)
					}
					server.SetTime(base.Add(tc.elapsed))
					if tc.peerCleanup {
						peerReservation, _, err := peer.Reserve(context.Background(), request("cc13-peer", 8, ClassInteractive, time.Minute))
						if err != nil || peerReservation == nil {
							return fmt.Errorf("peer cleanup failed: %v", err)
						}
						exists, err := observer.HExists(context.Background(), keys[9], member+":state").Result()
						if err != nil {
							return err
						}
						peerRemoved = !exists
						if !peerRemoved {
							return fmt.Errorf("peer did not remove expired original member")
						}
					}
					if tc.release {
						if err := service.Release(context.Background(), holder); err != nil {
							return err
						}
					}
					fairnessBeforeRetry = observer.HGet(context.Background(), keys[10], "last:interactive").Val()
				} else if result.Code == storeWait {
					// Stop the defective loop after observing one real retry. This guard is
					// not evidence of a product timeout: the script result and reset score are.
					cancel()
				}
				return nil
			}})
			reservation, decision, reserveErr := service.Reserve(ctx, target)
			t.Logf("redis_elapsed_ms=%d calls=%d peer_removed=%v script_results=%+v deadlines_ms=%v reservation=%v decision=%+v err=%v", tc.elapsed/time.Millisecond, calls, peerRemoved, observed, deadlines, reservation != nil, decision, reserveErr)
			if tc.elapsed > options.Interactive.MaxWait {
				if current := observer.HGet(context.Background(), keys[10], "last:interactive").Val(); current != fairnessBeforeRetry {
					t.Errorf("expired poll advanced fairness: %s -> %s", fairnessBeforeRetry, current)
				}
				if present := observer.HExists(context.Background(), keys[9], member+":state").Val(); present {
					t.Error("expired waiter metadata retained")
				}
				if _, err := observer.ZScore(context.Background(), keys[0], member).Result(); !errors.Is(err, redis.Nil) {
					t.Errorf("expired waiter acquired lease: %v", err)
				}
				if depth := observer.ZCard(context.Background(), keys[5]).Val(); depth != 0 {
					t.Errorf("expired queue depth=%d", depth)
				}
				if count := observer.HGet(context.Background(), keys[13], "7").Val(); count != "" {
					t.Errorf("expired org queue count=%q", count)
				}
			}
			if calls != 2 {
				t.Fatalf("fixture expected exactly two actual waiter script calls, got %d", calls)
			}
			if tc.wantAllowed {
				if reserveErr != nil || reservation == nil || decision.Waited != tc.elapsed {
					t.Fatalf("pre-deadline control should admit with original wait: reservation=%v decision=%+v err=%v", reservation, decision, reserveErr)
				}
			} else {
				if observed[1].Code != storeCapacity {
					t.Errorf("at/after original MaxWait: script code=%d, want capacity=%d; old deadline=%d new=%d", observed[1].Code, storeCapacity, deadlines[0], deadlines[1])
				}
				if !errors.Is(reserveErr, ErrCapacityExceeded) || reservation != nil || decision.Decision != "deny" || decision.Reason != ReasonCapacityExceeded {
					t.Errorf("expired original Reserve must deny capacity: reservation=%v decision=%+v err=%v", reservation != nil, decision, reserveErr)
				}
				if decision.Waited != tc.elapsed {
					t.Errorf("Waited=%v, want original Redis queue age %v", decision.Waited, tc.elapsed)
				}
			}
		})
	}
}

func cc13WorkloadOptions() Options {
	options := testOptions()
	options.GlobalCapacity, options.OrgCapacity = 2, 1
	options.Interactive = ClassPolicy{Weight: 1, MaxWait: 50 * time.Millisecond}
	options.Stream = ClassPolicy{Weight: 1, MaxWait: 50 * time.Millisecond}
	options.Embedding = ClassPolicy{Weight: 1, MaxWait: 50 * time.Millisecond}
	return options
}

// This is the exact pre-fix canonical, not an invented conflicting version.
func cc13LegacyWorkloadVersion(o Options) string {
	canonical := fmt.Sprintf("admission-v2|redis=fail-closed|g=%d|o=%d|gs=%d|os=%d|i=%d/%d|s=%d/%d|e=%d/%d|poll=%d|active=%d|store=%d|grace=%d|queue=%d/%d/%d",
		o.GlobalCapacity, o.OrgCapacity, o.GlobalShortReserve, o.OrgShortReserve,
		o.Interactive.Weight, ceilMilliseconds(o.Interactive.MaxWait), o.Stream.Weight, ceilMilliseconds(o.Stream.MaxWait),
		o.Embedding.Weight, ceilMilliseconds(o.Embedding.MaxWait), ceilMilliseconds(o.PollInterval), ceilMilliseconds(o.ActivePollWindow),
		ceilMilliseconds(o.StoreTimeout), ceilMilliseconds(o.LeaseGrace), o.MaxQueueDepth, o.MaxOrgQueueDepth, o.MaxQueueScan)
	sum := sha256.Sum256([]byte(canonical))
	return fmt.Sprintf("%x", sum[:8])
}

func cc13ReserveScriptOnce(t *testing.T, client *redis.Client, o Options, req Request, version, origin string) storeResult {
	t.Helper()
	policy, _ := o.Policy(req.Class)
	value, err := reserveScript.Run(context.Background(), client, runtimeKeys(req.OrgID),
		operationMember(req.OperationID), operationFingerprint(req, policy.Weight), req.OrgID, req.Class, policy.Weight,
		o.GlobalCapacity, o.OrgCapacity, o.GlobalShortReserve, o.OrgShortReserve,
		ceilMilliseconds(req.LeaseTTL+o.LeaseGrace), ceilMilliseconds(policy.MaxWait), ceilMilliseconds(o.ActivePollWindow),
		o.MaxQueueDepth, o.MaxQueueScan, ceilSeconds(req.LeaseTTL+o.LeaseGrace+policy.MaxWait+time.Second), version, o.MaxOrgQueueDepth, origin).Result()
	if err != nil {
		t.Fatal(err)
	}
	result, err := parseStoreResult(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func cc13Reservation(req Request, weight int64) *Reservation {
	return &Reservation{OperationID: req.OperationID, OrgID: req.OrgID, Class: req.Class, Weight: weight, member: operationMember(req.OperationID), fingerprint: operationFingerprint(req, weight)}
}

func TestCC13WorkloadLegacyCanonicalRequiresDrain(t *testing.T) {
	for _, queued := range []bool{false, true} {
		t.Run(fmt.Sprintf("queued_only_%v", queued), func(t *testing.T) {
			server := miniredis.RunT(t)
			server.SetTime(time.Unix(1800000000, 0))
			o := cc13WorkloadOptions()
			service, client := newTestService(t, server, o)
			legacy := cc13LegacyWorkloadVersion(o)
			if legacy == service.Version() {
				t.Fatal("new wait contract reused old fingerprint")
			}
			holder := request("legacy-holder", 7, ClassInteractive, time.Minute)
			if r := cc13ReserveScriptOnce(t, client, o, holder, legacy, ""); r.Code != storeAdmitted {
				t.Fatalf("legacy holder=%+v", r)
			}
			waiting := request("legacy-waiter", 7, ClassInteractive, time.Minute)
			if queued {
				if r := cc13ReserveScriptOnce(t, client, o, waiting, legacy, ""); r.Code != storeWait {
					t.Fatalf("legacy queued=%+v", r)
				}
				if err := service.Release(context.Background(), cc13Reservation(holder, 1)); err != nil {
					t.Fatal(err)
				}
			}
			if r, d, err := service.Reserve(context.Background(), request("new-before-drain", 8, ClassInteractive, time.Minute)); !errors.Is(err, ErrConfigConflict) || r != nil || d.Reason != ReasonConfigConflict {
				t.Fatalf("new/old mismatch r=%v d=%+v err=%v", r, d, err)
			}
			if got := client.Get(context.Background(), runtimeKeys(7)[12]).Val(); got != legacy {
				t.Fatalf("refused new caller changed old config: %q", got)
			}
			if queued {
				service.abandon(waiting.OrgID, operationMember(waiting.OperationID), operationFingerprint(waiting, 1), time.Minute)
			} else if err := service.Release(context.Background(), cc13Reservation(holder, 1)); err != nil {
				t.Fatal(err)
			}
			fresh, _, err := service.Reserve(context.Background(), request("new-after-drain", 7, ClassInteractive, time.Minute))
			if err != nil {
				t.Fatalf("drained activation: %v", err)
			}
			if got := client.Get(context.Background(), runtimeKeys(7)[12]).Val(); got != service.Version() {
				t.Fatalf("new config=%q", got)
			}
			oldRetry := request("legacy-after-new", 8, ClassInteractive, time.Minute)
			if r := cc13ReserveScriptOnce(t, client, o, oldRetry, legacy, ""); r.Code != storeConfigConflict {
				t.Fatalf("old/new mismatch=%+v", r)
			}
			if err := service.Release(context.Background(), fresh); err != nil {
				t.Fatal(err)
			}
			if r := cc13ReserveScriptOnce(t, client, o, oldRetry, legacy, ""); r.Code != storeAdmitted {
				t.Fatalf("drained legacy activation=%+v", r)
			}
			if err := service.Release(context.Background(), cc13Reservation(oldRetry, 1)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCC13ActiveLeaseReplayPrecedesQueueDeadline(t *testing.T) {
	server := miniredis.RunT(t)
	base := time.Unix(1800000000, 0)
	server.SetTime(base)
	o := cc13WorkloadOptions()
	service, client := newTestService(t, server, o)
	req := request("active-replay", 7, ClassInteractive, time.Minute)
	first, _, err := service.Reserve(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	server.SetTime(base.Add(500 * time.Millisecond))
	result := cc13ReserveScriptOnce(t, client, o, req, service.Version(), fmt.Sprint(base.UnixMilli()))
	if result.Code != storeIdempotent || result.GlobalUsed != 1 || result.OrgUsed != 1 {
		t.Fatalf("active lease was denied or double counted: %+v", result)
	}
	if err := service.Release(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if err := service.Release(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if got := client.Get(context.Background(), runtimeKeys(7)[3]).Val(); got != "0" {
		t.Fatalf("repeated release total=%q", got)
	}
}

func TestCC13WaitOriginSurvivesMultiplePolls(t *testing.T) {
	for _, release := range []bool{false, true} {
		t.Run(fmt.Sprintf("release_%v", release), func(t *testing.T) {
			server := miniredis.RunT(t)
			base := time.Unix(1800000000, 0)
			server.SetTime(base)
			o := cc13WorkloadOptions()
			service, client := newTestService(t, server, o)
			_, observer := newTestService(t, server, o)
			holder, _, err := service.Reserve(context.Background(), request("multi-held", 7, ClassInteractive, time.Minute))
			if err != nil {
				t.Fatal(err)
			}
			req := request("multi-wait", 7, ClassInteractive, time.Minute)
			member := operationMember(req.OperationID)
			keys := runtimeKeys(7)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			calls := 0
			var seen []int64
			var fairness string
			client.AddHook(cc13ReserveHook{after: func(_ context.Context, cmd redis.Cmder) error {
				args := cmd.Args()
				if cmd.Name() != "evalsha" || len(args) <= 3+len(keys) || fmt.Sprint(args[1]) != reserveScript.Hash() || fmt.Sprint(args[3+len(keys)]) != member {
					return nil
				}
				calls++
				value, err := cmd.(*redis.Cmd).Result()
				if err != nil {
					return err
				}
				r, err := parseStoreResult(value)
				if err != nil {
					return err
				}
				seen = append(seen, r.WaitedMS)
				current := observer.HGet(context.Background(), keys[10], "last:interactive").Val()
				if calls == 1 {
					fairness = current
				} else if current != fairness {
					return fmt.Errorf("poll advanced fairness: %s -> %s", fairness, current)
				}
				if calls > 1 && fmt.Sprint(args[3+len(keys)+17]) != fmt.Sprint(base.UnixMilli()) {
					return fmt.Errorf("poll %d changed Redis queue origin: %v", calls, args[3+len(keys)+17])
				}
				switch calls {
				case 1:
					server.SetTime(base.Add(10 * time.Millisecond))
				case 2:
					server.SetTime(base.Add(20 * time.Millisecond))
				case 3:
					elapsed := 51 * time.Millisecond
					if release {
						elapsed = 49 * time.Millisecond
					}
					server.SetTime(base.Add(elapsed))
					if release {
						return service.Release(context.Background(), holder)
					}
				default:
					if r.Code == storeWait {
						cancel()
					}
				}
				return nil
			}})
			reservation, decision, err := service.Reserve(ctx, req)
			want := int64(51)
			if release {
				want = 49
			}
			if fmt.Sprint(seen) != fmt.Sprint([]int64{0, 10, 20, want}) {
				t.Fatalf("wait history=%v", seen)
			}
			if decision.Waited != time.Duration(want)*time.Millisecond {
				t.Fatalf("wait=%v", decision.Waited)
			}
			if release {
				if err != nil || reservation == nil {
					t.Fatalf("in-budget admission=%v err=%v", reservation, err)
				}
			} else if !errors.Is(err, ErrCapacityExceeded) || reservation != nil {
				t.Fatalf("expired admission=%v err=%v", reservation, err)
			}
		})
	}
}

func TestCC13ExpiredWaiterDoesNotDecrementAnotherQueueEntry(t *testing.T) {
	server := miniredis.RunT(t)
	base := time.Unix(1800000000, 0)
	server.SetTime(base)
	o := cc13WorkloadOptions()
	service, client := newTestService(t, server, o)
	holder, _, err := service.Reserve(context.Background(), request("count-holder", 7, ClassInteractive, time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	old := request("count-expired", 7, ClassInteractive, time.Minute)
	if r := cc13ReserveScriptOnce(t, client, o, old, service.Version(), ""); r.Code != storeWait {
		t.Fatalf("first queue=%+v", r)
	}
	server.SetTime(base.Add(25 * time.Millisecond))
	next := request("count-still-live", 7, ClassInteractive, time.Minute)
	if r := cc13ReserveScriptOnce(t, client, o, next, service.Version(), ""); r.Code != storeWait {
		t.Fatalf("second queue=%+v", r)
	}
	keys := runtimeKeys(7)
	fairness := client.HGet(context.Background(), keys[10], "last:interactive").Val()
	server.SetTime(base.Add(51 * time.Millisecond))
	result := cc13ReserveScriptOnce(t, client, o, old, service.Version(), fmt.Sprint(base.UnixMilli()))
	if result.Code != storeCapacity || result.WaitedMS != 51 || result.QueueDepth != 1 || result.GlobalUsed != 1 || result.OrgUsed != 1 {
		t.Fatalf("expired result=%+v", result)
	}
	if got := client.HGet(context.Background(), keys[13], "7").Val(); got != "1" {
		t.Fatalf("other org queue entry lost: %q", got)
	}
	if got := client.ZCard(context.Background(), keys[5]).Val(); got != 1 {
		t.Fatalf("remaining queue=%d", got)
	}
	if got := client.HGet(context.Background(), keys[10], "last:interactive").Val(); got != fairness {
		t.Fatalf("expired poll advanced fairness: %q -> %q", fairness, got)
	}
	if err := service.Release(context.Background(), holder); err != nil {
		t.Fatal(err)
	}
	admitted, decision, err := service.Reserve(context.Background(), next)
	if err != nil || admitted == nil || decision.Waited != 26*time.Millisecond {
		t.Fatalf("surviving queued request r=%v d=%+v err=%v", admitted, decision, err)
	}
	if err := service.Release(context.Background(), admitted); err != nil {
		t.Fatal(err)
	}
}

func TestCC13QueuedFaultsRemainFailClosed(t *testing.T) {
	for _, outage := range []bool{false, true} {
		t.Run(fmt.Sprintf("outage_%v", outage), func(t *testing.T) {
			server := miniredis.RunT(t)
			server.SetTime(time.Unix(1800000000, 0))
			o := cc13WorkloadOptions()
			service, client := newTestService(t, server, o)
			if _, _, err := service.Reserve(context.Background(), request("fault-holder", 7, ClassInteractive, time.Minute)); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			req := request("fault-waiter", 7, ClassInteractive, time.Minute)
			member := operationMember(req.OperationID)
			keys := runtimeKeys(7)
			client.AddHook(cc13ReserveHook{after: func(_ context.Context, cmd redis.Cmder) error {
				args := cmd.Args()
				if cmd.Name() != "evalsha" || len(args) <= 3+len(keys) || fmt.Sprint(args[1]) != reserveScript.Hash() || fmt.Sprint(args[3+len(keys)]) != member {
					return nil
				}
				if outage {
					server.SetError("ERR cc13 local fixture unavailable")
				} else {
					cancel()
				}
				return nil
			}})
			r, d, err := service.Reserve(ctx, req)
			server.SetError("")
			if r != nil || !errors.Is(err, ErrStateUnavailable) || d.Decision != "deny" || d.Reason != ReasonStateUnavailable || d.Fallback {
				t.Fatalf("queued fault r=%v d=%+v err=%v", r, d, err)
			}
			if _, err := client.ZScore(context.Background(), keys[0], member).Result(); !errors.Is(err, redis.Nil) {
				t.Fatalf("fault acquired lease: %v", err)
			}
			if got := client.Get(context.Background(), keys[3]).Val(); got != "1" {
				t.Fatalf("fault changed holder capacity: %q", got)
			}
			if !outage && client.HExists(context.Background(), keys[9], member+":state").Val() {
				t.Fatal("cancelled queue entry retained")
			}
		})
	}
}
