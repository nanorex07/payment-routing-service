package memory

import (
	"context"
	"sync"
	"time"

	"payment-routing-service/internal/domain"
	"payment-routing-service/internal/service"
)

type MetricsConfig struct {
	WindowSize     int
	BucketDuration time.Duration
	Threshold      float64
	MinSamples     int
	Cooldown       time.Duration
}

type MetricsStore struct {
	mu       sync.RWMutex
	clock    service.Clock
	config   MetricsConfig
	gateways map[domain.GatewayName]*gatewayMetrics
}

type gatewayMetrics struct {
	buckets      []metricBucket
	blockedUntil time.Time
}

type metricBucket struct {
	lastUpdated time.Time
	successes   int
	failures    int
}

func NewMetricsStore(clock service.Clock, config MetricsConfig) *MetricsStore {
	return &MetricsStore{
		clock:    clock,
		config:   config,
		gateways: make(map[domain.GatewayName]*gatewayMetrics),
	}
}

func DefaultMetricsConfig() MetricsConfig {
	return MetricsConfig{
		WindowSize:     15,
		BucketDuration: time.Minute,
		Threshold:      0.90,
		MinSamples:     10,
		Cooldown:       30 * time.Minute,
	}
}

func (s *MetricsStore) Record(_ context.Context, gateway domain.GatewayName, success bool) (domain.MetricsSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock.Now()
	metrics := s.getOrCreate(gateway)
	s.addSample(metrics, now, success)
	return s.evaluateLocked(gateway, metrics, now), nil
}

func (s *MetricsStore) Snapshot(_ context.Context, gateway domain.GatewayName) (domain.MetricsSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock.Now()
	metrics := s.getOrCreate(gateway)
	return s.evaluateLocked(gateway, metrics, now), nil
}

func (s *MetricsStore) addSample(metrics *gatewayMetrics, now time.Time, success bool) {
	s.resetStaleBuckets(metrics, now)
	index := s.bucketIndex(now)
	bucket := &metrics.buckets[index]
	if s.isStale(*bucket, now) {
		*bucket = metricBucket{}
	}
	bucket.lastUpdated = now.Truncate(s.config.BucketDuration)
	if success {
		bucket.successes++
	} else {
		bucket.failures++
	}
}

func (s *MetricsStore) evaluateLocked(gateway domain.GatewayName, metrics *gatewayMetrics, now time.Time) domain.MetricsSnapshot {
	s.resetStaleBuckets(metrics, now)
	successes, failures := 0, 0
	for _, bucket := range metrics.buckets {
		successes += bucket.successes
		failures += bucket.failures
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
	}

	if !metrics.blockedUntil.IsZero() {
		if now.Before(metrics.blockedUntil) {
			blockedUntil := metrics.blockedUntil
			snapshot.Healthy = false
			snapshot.BlockedUntil = &blockedUntil
			snapshot.Reason = "cooldown_active"
			return snapshot
		}
		metrics.blockedUntil = time.Time{}
	}

	if total >= s.config.MinSamples && rate < s.config.Threshold {
		metrics.blockedUntil = now.Add(s.config.Cooldown)
		blockedUntil := metrics.blockedUntil
		snapshot.Healthy = false
		snapshot.BlockedUntil = &blockedUntil
		snapshot.Reason = "success_rate_below_threshold"
		return snapshot
	}

	snapshot.Reason = "healthy"
	return snapshot
}

func (s *MetricsStore) resetStaleBuckets(metrics *gatewayMetrics, now time.Time) {
	for i := range metrics.buckets {
		if s.isStale(metrics.buckets[i], now) {
			metrics.buckets[i] = metricBucket{}
		}
	}
}

func (s *MetricsStore) isStale(bucket metricBucket, now time.Time) bool {
	if bucket.lastUpdated.IsZero() {
		return false
	}
	windowDuration := time.Duration(s.config.WindowSize) * s.config.BucketDuration
	return !bucket.lastUpdated.After(now.Add(-windowDuration))
}

func (s *MetricsStore) bucketIndex(now time.Time) int {
	return int((now.UnixNano() / int64(s.config.BucketDuration)) % int64(s.config.WindowSize))
}

func (s *MetricsStore) getOrCreate(gateway domain.GatewayName) *gatewayMetrics {
	metrics, exists := s.gateways[gateway]
	if exists {
		return metrics
	}
	metrics = &gatewayMetrics{buckets: make([]metricBucket, s.config.WindowSize)}
	s.gateways[gateway] = metrics
	return metrics
}
