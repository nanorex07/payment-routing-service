package redis

import (
	"context"
	"fmt"
	"strconv"
	"time"

	redisc "github.com/redis/go-redis/v9"

	metricsconfig "payment-routing-service/internal/adapters/metrics"
	"payment-routing-service/internal/domain"
	"payment-routing-service/internal/service"
)

const defaultKeyPrefix = "prs:metrics"

var recordScript = redisc.NewScript(`
local success_flag = ARGV[1]
local now = tonumber(ARGV[2])
local window_size = tonumber(ARGV[3])
local threshold = tonumber(ARGV[4])
local min_samples = tonumber(ARGV[5])
local cooldown_ms = tonumber(ARGV[6])
local bucket_ttl_ms = tonumber(ARGV[7])
local new_blocked_until = ARGV[8]

if success_flag == "1" then
	redis.call("INCR", KEYS[2])
	redis.call("PEXPIRE", KEYS[2], bucket_ttl_ms)
else
	redis.call("INCR", KEYS[3])
	redis.call("PEXPIRE", KEYS[3], bucket_ttl_ms)
end

local successes = 0
local failures = 0
for i = 1, window_size do
	local success_value = redis.call("GET", KEYS[3 + i])
	if success_value then
		successes = successes + tonumber(success_value)
	end
	local failure_value = redis.call("GET", KEYS[3 + window_size + i])
	if failure_value then
		failures = failures + tonumber(failure_value)
	end
end

local total = successes + failures
local rate = 1
if total > 0 then
	rate = successes / total
end

local blocked_until = redis.call("GET", KEYS[1])
if blocked_until and tonumber(blocked_until) > now then
	return {successes, failures, total, tostring(rate), 0, blocked_until, "cooldown_active"}
end
if blocked_until then
	redis.call("DEL", KEYS[1])
end

if total >= min_samples and rate < threshold then
	redis.call("SET", KEYS[1], new_blocked_until, "PX", cooldown_ms)
	return {successes, failures, total, tostring(rate), 0, new_blocked_until, "success_rate_below_threshold"}
end

return {successes, failures, total, tostring(rate), 1, "", "healthy"}
`)

type MetricsConfig = metricsconfig.Config

type Option func(*MetricsStore)

type MetricsStore struct {
	client    redisc.Cmdable
	clock     service.Clock
	config    MetricsConfig
	keyPrefix string
}

func NewMetricsStore(client redisc.Cmdable, clock service.Clock, config MetricsConfig, options ...Option) *MetricsStore {
	store := &MetricsStore{
		client:    client,
		clock:     clock,
		config:    config,
		keyPrefix: defaultKeyPrefix,
	}
	for _, option := range options {
		option(store)
	}
	return store
}

func DefaultMetricsConfig() MetricsConfig {
	return metricsconfig.DefaultConfig()
}

func WithKeyPrefix(prefix string) Option {
	return func(store *MetricsStore) {
		if prefix != "" {
			store.keyPrefix = prefix
		}
	}
}

func (s *MetricsStore) Record(ctx context.Context, gateway domain.GatewayName, success bool) (domain.MetricsSnapshot, error) {
	now := s.clock.Now()
	keys := s.recordKeys(gateway, now)
	successFlag := "0"
	if success {
		successFlag = "1"
	}

	result, err := recordScript.Run(ctx, s.client, keys,
		successFlag,
		now.UnixNano(),
		s.config.WindowSize,
		strconv.FormatFloat(s.config.Threshold, 'f', -1, 64),
		s.config.MinSamples,
		durationMillis(s.config.Cooldown),
		durationMillis(s.config.WindowDuration()+s.config.BucketDuration),
		strconv.FormatInt(now.Add(s.config.Cooldown).UnixNano(), 10),
	).Result()
	if err != nil {
		return domain.MetricsSnapshot{}, err
	}

	return snapshotFromScript(gateway, result)
}

func (s *MetricsStore) BlockStatus(ctx context.Context, gateway domain.GatewayName) (domain.GatewayBlockStatus, error) {
	now := s.clock.Now()
	blockedUntil, err := s.readBlockedUntil(ctx, gateway, now)
	if err != nil {
		return domain.GatewayBlockStatus{}, err
	}
	if blockedUntil == nil {
		return domain.GatewayBlockStatus{Gateway: gateway}, nil
	}
	return domain.GatewayBlockStatus{
		Gateway:      gateway,
		Blocked:      true,
		BlockedUntil: blockedUntil,
		Reason:       "cooldown_active",
	}, nil
}

func (s *MetricsStore) Snapshot(ctx context.Context, gateway domain.GatewayName) (domain.MetricsSnapshot, error) {
	now := s.clock.Now()
	keys := s.snapshotKeys(gateway, now)
	values, err := s.client.MGet(ctx, keys...).Result()
	if err != nil {
		return domain.MetricsSnapshot{}, err
	}

	successes, failures := 0, 0
	for i := 0; i < s.config.WindowSize; i++ {
		successes += intValue(values[1+i])
		failures += intValue(values[1+s.config.WindowSize+i])
	}
	total := successes + failures
	rate := 1.0
	if total > 0 {
		rate = float64(successes) / float64(total)
	}

	snapshot := domain.MetricsSnapshot{
		Gateway:     gateway,
		Successes:   successes,
		Failures:    failures,
		Total:       total,
		SuccessRate: rate,
		Healthy:     true,
		Reason:      "healthy",
	}

	blockedUntil := blockedUntilFromValue(values[0], now)
	if blockedUntil != nil {
		snapshot.Healthy = false
		snapshot.BlockedUntil = blockedUntil
		snapshot.Reason = "cooldown_active"
	}
	return snapshot, nil
}

func (s *MetricsStore) readBlockedUntil(ctx context.Context, gateway domain.GatewayName, now time.Time) (*time.Time, error) {
	key := s.blockKey(gateway)
	value, err := s.client.Get(ctx, key).Result()
	if err == redisc.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	blockedUntil := blockedUntilFromString(value, now)
	if blockedUntil == nil {
		if err := s.client.Del(ctx, key).Err(); err != nil {
			return nil, err
		}
	}
	return blockedUntil, nil
}

func (s *MetricsStore) recordKeys(gateway domain.GatewayName, now time.Time) []string {
	successKeys, failureKeys := s.counterKeys(gateway, now)
	keys := make([]string, 0, 3+len(successKeys)+len(failureKeys))
	keys = append(keys, s.blockKey(gateway), s.counterKey(gateway, s.bucketStart(now), true), s.counterKey(gateway, s.bucketStart(now), false))
	keys = append(keys, successKeys...)
	keys = append(keys, failureKeys...)
	return keys
}

func (s *MetricsStore) snapshotKeys(gateway domain.GatewayName, now time.Time) []string {
	successKeys, failureKeys := s.counterKeys(gateway, now)
	keys := make([]string, 0, 1+len(successKeys)+len(failureKeys))
	keys = append(keys, s.blockKey(gateway))
	keys = append(keys, successKeys...)
	keys = append(keys, failureKeys...)
	return keys
}

func (s *MetricsStore) counterKeys(gateway domain.GatewayName, now time.Time) ([]string, []string) {
	successKeys := make([]string, 0, s.config.WindowSize)
	failureKeys := make([]string, 0, s.config.WindowSize)
	current := s.bucketStart(now)
	for i := 0; i < s.config.WindowSize; i++ {
		start := current - int64(i)*int64(s.config.BucketDuration)
		successKeys = append(successKeys, s.counterKey(gateway, start, true))
		failureKeys = append(failureKeys, s.counterKey(gateway, start, false))
	}
	return successKeys, failureKeys
}

func (s *MetricsStore) bucketStart(now time.Time) int64 {
	duration := int64(s.config.BucketDuration)
	return now.UnixNano() / duration * duration
}

func (s *MetricsStore) blockKey(gateway domain.GatewayName) string {
	return fmt.Sprintf("%s:{%s}:blocked", s.keyPrefix, gateway)
}

func (s *MetricsStore) counterKey(gateway domain.GatewayName, bucketStart int64, success bool) string {
	outcome := "failure"
	if success {
		outcome = "success"
	}
	return fmt.Sprintf("%s:{%s}:bucket:%d:%s", s.keyPrefix, gateway, bucketStart, outcome)
}

func snapshotFromScript(gateway domain.GatewayName, result any) (domain.MetricsSnapshot, error) {
	values, ok := result.([]any)
	if !ok {
		return domain.MetricsSnapshot{}, fmt.Errorf("unexpected redis script result %T", result)
	}
	if len(values) != 7 {
		return domain.MetricsSnapshot{}, fmt.Errorf("unexpected redis script result length %d", len(values))
	}

	successes := intValue(values[0])
	failures := intValue(values[1])
	total := intValue(values[2])
	rate, err := strconv.ParseFloat(stringValue(values[3]), 64)
	if err != nil {
		return domain.MetricsSnapshot{}, err
	}
	healthy := intValue(values[4]) == 1
	snapshot := domain.MetricsSnapshot{
		Gateway:     gateway,
		Successes:   successes,
		Failures:    failures,
		Total:       total,
		SuccessRate: rate,
		Healthy:     healthy,
		Reason:      stringValue(values[6]),
	}
	if !healthy {
		blockedUntilUnix, err := strconv.ParseInt(stringValue(values[5]), 10, 64)
		if err != nil {
			return domain.MetricsSnapshot{}, err
		}
		blockedUntil := time.Unix(0, blockedUntilUnix)
		snapshot.BlockedUntil = &blockedUntil
	}
	return snapshot, nil
}

func intValue(value any) int {
	switch typed := value.(type) {
	case nil:
		return 0
	case int64:
		return int(typed)
	case string:
		parsed, _ := strconv.Atoi(typed)
		return parsed
	case []byte:
		parsed, _ := strconv.Atoi(string(typed))
		return parsed
	default:
		return 0
	}
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	default:
		return fmt.Sprint(typed)
	}
}

func blockedUntilFromValue(value any, now time.Time) *time.Time {
	return blockedUntilFromString(stringValue(value), now)
}

func blockedUntilFromString(value string, now time.Time) *time.Time {
	if value == "" || value == "<nil>" {
		return nil
	}
	unixNano, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return nil
	}
	blockedUntil := time.Unix(0, unixNano)
	if !now.Before(blockedUntil) {
		return nil
	}
	return &blockedUntil
}

func durationMillis(duration time.Duration) int64 {
	millis := duration.Milliseconds()
	if millis < 1 {
		return 1
	}
	return millis
}
