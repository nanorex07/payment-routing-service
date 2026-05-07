package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"payment-routing-service/internal/adapters/callback"
	"payment-routing-service/internal/adapters/memory"
	"payment-routing-service/internal/domain"
	"payment-routing-service/internal/ports"
	"payment-routing-service/internal/service"
)

type nopLogger struct{}

func (nopLogger) Info(context.Context, string, ...any)  {}
func (nopLogger) Error(context.Context, string, ...any) {}

type testGatewayClient struct {
	name domain.GatewayName
}

func (c testGatewayClient) Name() domain.GatewayName {
	return c.name
}

func (testGatewayClient) Initiate(context.Context, *domain.Transaction) error {
	return nil
}

func (c testGatewayClient) ParseCallback(payload []byte) (domain.CallbackResult, error) {
	var raw struct {
		TransactionID string                   `json:"transaction_id"`
		OrderID       string                   `json:"order_id"`
		Status        domain.TransactionStatus `json:"status"`
		Reason        string                   `json:"reason"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return domain.CallbackResult{}, domain.ErrInvalidCallback
	}
	if raw.OrderID == "" || raw.Status == "" {
		return domain.CallbackResult{}, domain.ErrInvalidCallback
	}
	return domain.CallbackResult{
		TransactionID: raw.TransactionID,
		OrderID:       raw.OrderID,
		Gateway:       c.name,
		Status:        raw.Status,
		Reason:        raw.Reason,
	}, nil
}

type seqID struct {
	next int
}

func (g *seqID) NewID() string {
	g.next++
	return "txn_test_" + string(rune('0'+g.next))
}

type mutableClock struct {
	now time.Time
}

func (c *mutableClock) Now() time.Time {
	return c.now
}

type fixedRandom struct {
	value int
}

func (r fixedRandom) IntN(int) int {
	return r.value
}

func newTestService(clock *mutableClock, random service.RandomSource) (*service.PaymentService, *memory.TransactionRepository) {
	repo := memory.NewTransactionRepository()
	metrics := memory.NewMetricsStore(clock, memory.DefaultMetricsConfig())
	clients := []ports.GatewayClient{
		testGatewayClient{name: domain.GatewayRazorpay},
		testGatewayClient{name: domain.GatewayPayU},
		testGatewayClient{name: domain.GatewayCashfree},
	}
	return service.NewPaymentService(
		repo,
		metrics,
		[]domain.Gateway{
			{Name: domain.GatewayRazorpay, Weight: 50, Enabled: true},
			{Name: domain.GatewayPayU, Weight: 30, Enabled: true},
			{Name: domain.GatewayCashfree, Weight: 20, Enabled: true},
		},
		clients,
		callback.NewParser(clients),
		nopLogger{},
		clock,
		&seqID{},
		random,
	), repo
}

func TestInitiateRejectsDuplicateOrderID(t *testing.T) {
	clock := &mutableClock{now: time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC)}
	svc, repo := newTestService(clock, fixedRandom{value: 0})
	ctx := context.Background()
	req := domain.InitiateRequest{OrderID: "ORD123", Amount: 499, PaymentInstrument: domain.PaymentInstrument{Type: "card"}}

	if _, err := svc.InitiateTransaction(ctx, req); err != nil {
		t.Fatal(err)
	}
	_, err := svc.InitiateTransaction(ctx, req)
	if !errors.Is(err, domain.ErrDuplicateOrder) {
		t.Fatalf("got err %v want duplicate order", err)
	}
	count, err := repo.CountByOrderID(ctx, "ORD123")
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("got count %d want 1", count)
	}
}

func TestInitiateReadsBlockStatusWithoutSnapshot(t *testing.T) {
	clock := &mutableClock{now: time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC)}
	repo := memory.NewTransactionRepository()
	metrics := &initiateMetricsStore{
		blocked: map[domain.GatewayName]bool{
			domain.GatewayRazorpay: true,
		},
	}
	clients := []ports.GatewayClient{
		testGatewayClient{name: domain.GatewayRazorpay},
		testGatewayClient{name: domain.GatewayPayU},
	}
	svc := service.NewPaymentService(
		repo,
		metrics,
		[]domain.Gateway{
			{Name: domain.GatewayRazorpay, Weight: 1, Enabled: true},
			{Name: domain.GatewayPayU, Weight: 1, Enabled: true},
		},
		clients,
		callback.NewParser(clients),
		nopLogger{},
		clock,
		&seqID{},
		fixedRandom{value: 0},
	)

	res, err := svc.InitiateTransaction(context.Background(), domain.InitiateRequest{
		OrderID:           "ORD-BLOCK-STATUS",
		Amount:            499,
		PaymentInstrument: domain.PaymentInstrument{Type: "card"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Transaction.Gateway != domain.GatewayPayU {
		t.Fatalf("got gateway %s want payu", res.Transaction.Gateway)
	}
	if metrics.snapshotCalled {
		t.Fatal("initiate should not read metrics snapshots")
	}
}

func TestCallbackUpdatesStatusAndMetrics(t *testing.T) {
	clock := &mutableClock{now: time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC)}
	svc, _ := newTestService(clock, fixedRandom{value: 0})
	ctx := context.Background()
	initRes, err := svc.InitiateTransaction(ctx, domain.InitiateRequest{OrderID: "ORD123", Amount: 499, PaymentInstrument: domain.PaymentInstrument{Type: "card"}})
	if err != nil {
		t.Fatal(err)
	}

	payload, _ := json.Marshal(map[string]string{
		"transaction_id": initRes.Transaction.ID,
		"order_id":       "ORD123",
		"gateway":        "razorpay",
		"status":         "success",
	})
	callbackRes, err := svc.ProcessCallback(ctx, payload)
	if err != nil {
		t.Fatal(err)
	}
	if callbackRes.Transaction.Status != domain.TransactionStatusSuccess {
		t.Fatalf("got status %s want success", callbackRes.Transaction.Status)
	}
	if callbackRes.Metrics.Successes != 1 || callbackRes.Metrics.Failures != 0 {
		t.Fatalf("bad metrics: %+v", callbackRes.Metrics)
	}
}

type initiateMetricsStore struct {
	blocked        map[domain.GatewayName]bool
	snapshotCalled bool
}

func (s *initiateMetricsStore) Record(context.Context, domain.GatewayName, bool) (domain.MetricsSnapshot, error) {
	return domain.MetricsSnapshot{}, nil
}

func (s *initiateMetricsStore) BlockStatus(_ context.Context, gateway domain.GatewayName) (domain.GatewayBlockStatus, error) {
	if s.blocked[gateway] {
		return domain.GatewayBlockStatus{Gateway: gateway, Blocked: true, Reason: "cooldown_active"}, nil
	}
	return domain.GatewayBlockStatus{Gateway: gateway}, nil
}

func (s *initiateMetricsStore) Snapshot(context.Context, domain.GatewayName) (domain.MetricsSnapshot, error) {
	s.snapshotCalled = true
	return domain.MetricsSnapshot{Healthy: true}, nil
}
