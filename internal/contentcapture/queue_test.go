package contentcapture

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func testQueue(t *testing.T, items int, total, project, chunk int64) *Queue {
	t.Helper()
	q, err := NewQueue(QueueLimits{MaxItems: items, MaxBytes: total, MaxProjectBytes: project, MaxChunkBytes: chunk})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(q.Close)
	return q
}

func TestQueueBudgetIncludesConsumer(t *testing.T) {
	q := testQueue(t, 4, 8, 4, 4)
	ctx := context.Background()
	payload := []byte("abcd")
	if err := q.Offer(ctx, 1, payload); err != nil {
		t.Fatal(err)
	}
	payload[0] = 'X'
	item, err := q.Take(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if string(item.Bytes()) != "abcd" || item.ProjectID() != 1 {
		t.Fatal("queue did not own a copy")
	}
	if err := q.Offer(ctx, 1, []byte("x")); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("consumer bytes not counted: %v", err)
	}
	if err := q.Offer(ctx, 2, []byte("1234")); err != nil {
		t.Fatal(err)
	}
	if err := q.Offer(ctx, 3, []byte("x")); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("node bytes not counted: %v", err)
	}
	u := q.Usage(1)
	if u.Items != 2 || u.QueuedItems != 1 || u.Bytes != 8 || u.ProjectBytes != 4 {
		t.Fatalf("usage: %#v", u)
	}
	item.Release()
	item.Release()
	if item.Bytes() != nil {
		t.Fatal("released bytes still retained")
	}
	if err := q.Offer(ctx, 1, []byte("next")); err != nil {
		t.Fatal(err)
	}
	q.Close()
	if u := q.Usage(1); u != (QueueUsage{}) {
		t.Fatalf("close leak: %#v", u)
	}
}

func TestQueueEmptyItemsAndFIFO(t *testing.T) {
	q := testQueue(t, 2, 10, 10, 5)
	ctx := context.Background()
	if err := q.Offer(ctx, 1, nil); err != nil {
		t.Fatal(err)
	}
	first, err := q.Take(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Offer(ctx, 2, []byte("2")); err != nil {
		t.Fatal(err)
	}
	if err := q.Offer(ctx, 3, nil); !errors.Is(err, ErrQueueFull) {
		t.Fatal("empty consumer bypassed item bound")
	}
	first.Release()
	if err := q.Offer(ctx, 3, []byte("3")); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"2", "3"} {
		item, err := q.Take(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if string(item.Bytes()) != expected {
			t.Fatal("FIFO changed")
		}
		item.Release()
	}
}

func TestQueueCancellationAndClose(t *testing.T) {
	q := testQueue(t, 2, 8, 8, 4)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := q.Offer(ctx, 1, []byte("x")); !errors.Is(err, context.Canceled) {
		t.Fatalf("offer cancellation: %v", err)
	}
	if _, err := q.Take(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("take cancellation: %v", err)
	}
	waitCtx, waitCancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { _, err := q.Take(waitCtx); result <- err }()
	waitCancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Take did not cancel")
	}
	if err := q.Offer(context.Background(), 1, []byte("live")); err != nil {
		t.Fatal(err)
	}
	item, err := q.Take(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Offer(context.Background(), 1, []byte("wait")); err != nil {
		t.Fatal(err)
	}
	q.Close()
	q.Close()
	if u := q.Usage(1); u.Items != 1 || u.Bytes != 4 || u.QueuedItems != 0 {
		t.Fatalf("close lost consumer budget: %#v", u)
	}
	if string(item.Bytes()) != "live" {
		t.Fatal("close changed consumer bytes")
	}
	item.Release()
	if u := q.Usage(1); u != (QueueUsage{}) {
		t.Fatal(u)
	}
	if err := q.Offer(context.Background(), 1, nil); !errors.Is(err, ErrQueueClosed) {
		t.Fatal(err)
	}
	if _, err := q.Take(context.Background()); !errors.Is(err, ErrQueueClosed) {
		t.Fatal(err)
	}
	q2 := testQueue(t, 1, 1, 1, 1)
	go func() { _, err := q2.Take(context.Background()); result <- err }()
	q2.Close()
	select {
	case err := <-result:
		if !errors.Is(err, ErrQueueClosed) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not wake waiter")
	}
}

func TestQueueRejectsBeforeCopy(t *testing.T) {
	q := testQueue(t, 1, 4, 4, 4)
	ctx := context.Background()
	if err := q.Offer(ctx, 1, []byte("12345")); !errors.Is(err, ErrChunkTooLarge) {
		t.Fatal(err)
	}
	if err := q.Offer(ctx, 1, []byte("1234")); err != nil {
		t.Fatal(err)
	}
	bytes := []byte("abcd")
	if allocations := testing.AllocsPerRun(100, func() {
		if !errors.Is(q.Offer(ctx, 2, bytes), ErrQueueFull) {
			panic("not full")
		}
	}); allocations != 0 {
		t.Fatalf("full offer allocated: %v", allocations)
	}
	if u := q.Usage(2); u.Items != 1 || u.Bytes != 4 || u.ProjectBytes != 0 {
		t.Fatal(u)
	}
}

func TestQueueConcurrentProducersAndRelease(t *testing.T) {
	q := testQueue(t, 8, 32, 8, 4)
	ctx := context.Background()
	var producers sync.WaitGroup
	var consumers sync.WaitGroup
	for i := 0; i < 3; i++ {
		consumers.Add(1)
		go func() {
			defer consumers.Done()
			for {
				item, err := q.Take(ctx)
				if errors.Is(err, ErrQueueClosed) {
					return
				}
				if err != nil {
					t.Error(err)
					return
				}
				item.Release()
			}
		}()
	}
	for project := int64(1); project <= 8; project++ {
		producers.Add(1)
		go func(id int64) {
			defer producers.Done()
			for i := 0; i < 100; i++ {
				if err := q.Offer(ctx, id, []byte("1234")); err != nil && !errors.Is(err, ErrQueueFull) {
					t.Error(err)
				}
				u := q.Usage(id)
				if u.Items > 8 || u.Bytes > 32 || u.ProjectBytes > 8 {
					t.Errorf("over budget: %#v", u)
				}
			}
		}(project)
	}
	producers.Wait()
	q.Close()
	consumers.Wait()
	if u := q.Usage(1); u != (QueueUsage{}) {
		t.Fatalf("leak: %#v", u)
	}
}
