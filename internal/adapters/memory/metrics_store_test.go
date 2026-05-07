package memory

import (
	"context"
	"testing"
	"time"

	"payment-routing-service/internal/domain"
)

type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	return c.now
}

func TestMetricsStoreCircuitBreaker(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC)}
	store := NewMetricsStore(clock, DefaultMetricsConfig())
	ctx := context.Background()

	for i := 0; i < 9; i++ {
		snapshot, err := store.Record(ctx, domain.GatewayRazorpay, false)
		if err != nil {
			t.Fatal(err)
		}
		if !snapshot.Healthy {
			t.Fatalf("gateway unhealthy before min samples at sample %d", i+1)
		}
	}

	snapshot, err := store.Record(ctx, domain.GatewayRazorpay, false)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Healthy {
		t.Fatal("expected unhealthy gateway")
	}
	if snapshot.BlockedUntil == nil {
		t.Fatal("expected blocked_until")
	}
	if snapshot.Reason != "success_rate_below_threshold" {
		t.Fatalf("got reason %q", snapshot.Reason)
	}

	clock.now = clock.now.Add(29 * time.Minute)
	status, err := store.BlockStatus(ctx, domain.GatewayRazorpay)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Blocked {
		t.Fatal("expected cooldown still active")
	}

	clock.now = clock.now.Add(2 * time.Minute)
	status, err = store.BlockStatus(ctx, domain.GatewayRazorpay)
	if err != nil {
		t.Fatal(err)
	}
	if status.Blocked {
		t.Fatal("expected cooldown expired")
	}
}

func TestMetricsStoreBlockStatusDoesNotCreateGatewayMetrics(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC)}
	store := NewMetricsStore(clock, DefaultMetricsConfig())

	status, err := store.BlockStatus(context.Background(), domain.GatewayRazorpay)
	if err != nil {
		t.Fatal(err)
	}
	if status.Blocked {
		t.Fatal("expected new gateway to be unblocked")
	}
	if len(store.gateways) != 0 {
		t.Fatalf("block status created gateway metrics: %d", len(store.gateways))
	}
}

func TestMetricsStoreSnapshotDoesNotApplyNewBlock(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC)}
	store := NewMetricsStore(clock, MetricsConfig{
		WindowSize:     5,
		BucketDuration: time.Minute,
		Threshold:      0.90,
		MinSamples:     1,
		Cooldown:       30 * time.Minute,
	})
	metrics := store.getOrCreate(domain.GatewayPayU)
	metrics.buckets[0] = metricBucket{
		lastUpdated: clock.now.Truncate(time.Minute),
		failures:    10,
	}

	snapshot, err := store.Snapshot(context.Background(), domain.GatewayPayU)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Healthy {
		t.Fatalf("snapshot applied block: %+v", snapshot)
	}
	status, err := store.BlockStatus(context.Background(), domain.GatewayPayU)
	if err != nil {
		t.Fatal(err)
	}
	if status.Blocked {
		t.Fatalf("snapshot wrote block status: %+v", status)
	}
}

func TestMetricsStoreSlidingWindowPrunesOldBuckets(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC)}
	store := NewMetricsStore(clock, DefaultMetricsConfig())
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if _, err := store.Record(ctx, domain.GatewayPayU, false); err != nil {
			t.Fatal(err)
		}
	}
	clock.now = clock.now.Add(16 * time.Minute)
	for i := 0; i < 10; i++ {
		if _, err := store.Record(ctx, domain.GatewayPayU, true); err != nil {
			t.Fatal(err)
		}
	}

	snapshot, err := store.Snapshot(ctx, domain.GatewayPayU)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Failures != 0 {
		t.Fatalf("old failures not pruned: %d", snapshot.Failures)
	}
	if snapshot.Successes != 10 {
		t.Fatalf("got successes %d want 10", snapshot.Successes)
	}
	if !snapshot.Healthy {
		t.Fatal("expected healthy gateway")
	}
}

func TestMetricsStoreUsesFixedBucketsAndResetsStaleSlots(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC)}
	store := NewMetricsStore(clock, DefaultMetricsConfig())
	ctx := context.Background()

	if _, err := store.Record(ctx, domain.GatewayCashfree, false); err != nil {
		t.Fatal(err)
	}
	metrics := store.gateways[domain.GatewayCashfree]
	if len(metrics.buckets) != store.config.WindowSize {
		t.Fatalf("got buckets %d want %d", len(metrics.buckets), store.config.WindowSize)
	}

	clock.now = clock.now.Add(time.Duration(store.config.WindowSize) * store.config.BucketDuration)
	if _, err := store.Record(ctx, domain.GatewayCashfree, true); err != nil {
		t.Fatal(err)
	}
	if len(metrics.buckets) != store.config.WindowSize {
		t.Fatalf("bucket count changed: %d", len(metrics.buckets))
	}

	snapshot, err := store.Snapshot(ctx, domain.GatewayCashfree)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Failures != 0 {
		t.Fatalf("stale failure not reset: %d", snapshot.Failures)
	}
	if snapshot.Successes != 1 {
		t.Fatalf("got successes %d want 1", snapshot.Successes)
	}
}

func TestMetricsStoreRollingWindowWithOneRequestPerMinute(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC)}
	store := NewMetricsStore(clock, MetricsConfig{
		WindowSize:     5,
		BucketDuration: time.Minute,
		Threshold:      -1,
		MinSamples:     0,
		Cooldown:       30 * time.Minute,
	})
	ctx := context.Background()

	requests := []bool{true, false, true, true, false, true, false}
	want := []struct {
		successes int
		failures  int
	}{
		{successes: 1, failures: 0},
		{successes: 1, failures: 1},
		{successes: 2, failures: 1},
		{successes: 3, failures: 1},
		{successes: 3, failures: 2},
		{successes: 3, failures: 2},
		{successes: 3, failures: 2},
	}

	for i, success := range requests {
		if i > 0 {
			clock.now = clock.now.Add(time.Minute)
		}
		snapshot, err := store.Record(ctx, domain.GatewayRazorpay, success)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.Successes != want[i].successes || snapshot.Failures != want[i].failures {
			t.Fatalf("minute %d got successes=%d failures=%d want successes=%d failures=%d", i, snapshot.Successes, snapshot.Failures, want[i].successes, want[i].failures)
		}
		if snapshot.Total != want[i].successes+want[i].failures {
			t.Fatalf("minute %d got total=%d want %d", i, snapshot.Total, want[i].successes+want[i].failures)
		}
	}
}

func TestMetricsStoreRollingWindowResetsExpiredBucketsOnSnapshot(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC)}
	store := NewMetricsStore(clock, MetricsConfig{
		WindowSize:     5,
		BucketDuration: time.Minute,
		Threshold:      -1,
		MinSamples:     0,
		Cooldown:       30 * time.Minute,
	})
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if i > 0 {
			clock.now = clock.now.Add(time.Minute)
		}
		if _, err := store.Record(ctx, domain.GatewayPayU, i%2 == 0); err != nil {
			t.Fatal(err)
		}
	}

	snapshot, err := store.Snapshot(ctx, domain.GatewayPayU)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Successes != 3 || snapshot.Failures != 2 {
		t.Fatalf("before expiry got successes=%d failures=%d want successes=3 failures=2", snapshot.Successes, snapshot.Failures)
	}

	clock.now = clock.now.Add(5 * time.Minute)
	snapshot, err = store.Snapshot(ctx, domain.GatewayPayU)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Successes != 0 || snapshot.Failures != 0 || snapshot.Total != 0 {
		t.Fatalf("after expiry got successes=%d failures=%d total=%d want all zero", snapshot.Successes, snapshot.Failures, snapshot.Total)
	}
	if len(store.gateways[domain.GatewayPayU].buckets) != 5 {
		t.Fatalf("bucket count changed: %d", len(store.gateways[domain.GatewayPayU].buckets))
	}
}
