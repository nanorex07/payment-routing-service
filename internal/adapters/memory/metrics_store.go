package memory

import (
	"context"
	"sync"
	"time"

	metricsconfig "payment-routing-service/internal/adapters/metrics"
	"payment-routing-service/internal/domain"
	"payment-routing-service/internal/service"
)

type MetricsConfig = metricsconfig.Config

type MetricsStore struct {
	mu       sync.RWMutex // protects gateways map only
	clock    service.Clock
	config   MetricsConfig
	gateways map[domain.GatewayName]*gatewayMetrics
}

type gatewayMetrics struct {
	mu           sync.Mutex
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
	return metricsconfig.DefaultConfig()
}

func (s *MetricsStore) Record(_ context.Context, gateway domain.GatewayName, success bool) (domain.MetricsSnapshot, error) {
	now := s.clock.Now()
	metrics := s.getOrCreate(gateway)
	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	s.addSample(metrics, now, success)
	return s.evaluateLocked(gateway, metrics, now, true), nil
}

func (s *MetricsStore) BlockStatus(_ context.Context, gateway domain.GatewayName) (domain.GatewayBlockStatus, error) {
	now := s.clock.Now()
	metrics := s.get(gateway)
	if metrics == nil {
		return domain.GatewayBlockStatus{Gateway: gateway}, nil
	}

	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	if metrics.blockedUntil.IsZero() {
		return domain.GatewayBlockStatus{Gateway: gateway}, nil
	}
	if now.Before(metrics.blockedUntil) {
		blockedUntil := metrics.blockedUntil
		return domain.GatewayBlockStatus{
			Gateway:      gateway,
			Blocked:      true,
			BlockedUntil: &blockedUntil,
			Reason:       "cooldown_active",
		}, nil
	}
	metrics.blockedUntil = time.Time{}
	return domain.GatewayBlockStatus{Gateway: gateway}, nil
}

func (s *MetricsStore) Snapshot(_ context.Context, gateway domain.GatewayName) (domain.MetricsSnapshot, error) {
	now := s.clock.Now()
	metrics := s.getOrCreate(gateway)
	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	return s.evaluateLocked(gateway, metrics, now, false), nil
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

func (s *MetricsStore) evaluateLocked(gateway domain.GatewayName, metrics *gatewayMetrics, now time.Time, applyBlock bool) domain.MetricsSnapshot {
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

	if applyBlock && total >= s.config.MinSamples && rate < s.config.Threshold {
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
	s.mu.RLock()
	metrics, exists := s.gateways[gateway]
	if exists {
		s.mu.RUnlock()
		return metrics
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	metrics, exists = s.gateways[gateway]
	if exists {
		return metrics
	}
	metrics = &gatewayMetrics{buckets: make([]metricBucket, s.config.WindowSize)}
	s.gateways[gateway] = metrics
	return metrics
}

func (s *MetricsStore) get(gateway domain.GatewayName) *gatewayMetrics {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.gateways[gateway]
}
