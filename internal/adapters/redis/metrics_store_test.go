package redis

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	redisc "github.com/redis/go-redis/v9"

	"payment-routing-service/internal/domain"
)

type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	return c.now
}

func TestMetricsStoreRecordAndBlockStatusWithRedis(t *testing.T) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		t.Skip("set REDIS_ADDR to run Redis metrics integration test")
	}

	ctx := context.Background()
	client := redisc.NewClient(&redisc.Options{Addr: addr})
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = client.Close()
	})

	prefix := "prs:metrics:test:" + strconvTime(time.Now())
	t.Cleanup(func() {
		cleanupKeys(ctx, t, client, prefix+":*")
	})

	clock := &fakeClock{now: time.Now().UTC()}
	store := NewMetricsStore(client, clock, MetricsConfig{
		WindowSize:     5,
		BucketDuration: time.Second,
		Threshold:      0.50,
		MinSamples:     3,
		Cooldown:       30 * time.Second,
	}, WithKeyPrefix(prefix))

	for i := 0; i < 3; i++ {
		snapshot, err := store.Record(ctx, domain.GatewayRazorpay, false)
		if err != nil {
			t.Fatal(err)
		}
		if i < 2 && !snapshot.Healthy {
			t.Fatalf("gateway blocked before minimum samples: %+v", snapshot)
		}
	}

	status, err := store.BlockStatus(ctx, domain.GatewayRazorpay)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Blocked {
		t.Fatal("expected blocked gateway")
	}

	snapshot, err := store.Snapshot(ctx, domain.GatewayRazorpay)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Failures != 3 || snapshot.Successes != 0 || snapshot.Healthy {
		t.Fatalf("bad snapshot: %+v", snapshot)
	}
}

func cleanupKeys(ctx context.Context, t *testing.T, client *redisc.Client, pattern string) {
	t.Helper()
	var cursor uint64
	for {
		keys, next, err := client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			t.Fatal(err)
		}
		if len(keys) > 0 {
			if err := client.Del(ctx, keys...).Err(); err != nil {
				t.Fatal(err)
			}
		}
		if next == 0 {
			return
		}
		cursor = next
	}
}

func strconvTime(t time.Time) string {
	return strconv.FormatInt(t.UnixNano(), 10)
}
