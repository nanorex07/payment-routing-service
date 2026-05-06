package httpadapter_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"payment-routing-service/internal/adapters/callback"
	httpadapter "payment-routing-service/internal/adapters/http"
	"payment-routing-service/internal/adapters/memory"
	"payment-routing-service/internal/domain"
	"payment-routing-service/internal/ports"
	"payment-routing-service/internal/service"
)

type nopLogger struct{}

func (nopLogger) Info(context.Context, string, ...any)  {}
func (nopLogger) Error(context.Context, string, ...any) {}

type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	return c.now
}

type fixedRandom struct {
	value int
}

func (r fixedRandom) IntN(int) int {
	return r.value
}

type seqID struct {
	next atomic.Int64
}

func (g *seqID) NewID() string {
	next := g.next.Add(1)
	return fmt.Sprintf("txn_http_%d", next)
}

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

func TestGatewayOutageStopsSelectingGateway(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC)}
	repo := memory.NewTransactionRepository()
	metrics := memory.NewMetricsStore(clock, memory.DefaultMetricsConfig())
	clients := []ports.GatewayClient{
		testGatewayClient{name: domain.GatewayRazorpay},
		testGatewayClient{name: domain.GatewayPayU},
		testGatewayClient{name: domain.GatewayCashfree},
	}
	svc := service.NewPaymentService(
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
		fixedRandom{value: 0},
	)
	handler := httpadapter.NewHandler(svc, time.Second).Routes()

	tx := initiate(t, handler, "ORD-OUTAGE-1")
	if tx.Gateway != domain.GatewayRazorpay {
		t.Fatalf("got initial gateway %s want razorpay", tx.Gateway)
	}

	for i := 0; i < 15; i++ {
		callbackPayload := map[string]string{
			"transaction_id": tx.ID,
			"order_id":       tx.OrderID,
			"gateway":        string(tx.Gateway),
			"status":         "failure",
			"reason":         "outage",
		}
		status := postJSON(t, handler, "/transactions/callback", callbackPayload, nil)
		if status != http.StatusOK {
			t.Fatalf("callback status got %d want 200", status)
		}
	}

	for i := 0; i < 10; i++ {
		next := initiate(t, handler, fmt.Sprintf("ORD-AFTER-%d", i))
		if next.Gateway == domain.GatewayRazorpay {
			t.Fatalf("razorpay selected during cooldown for transaction %+v", next)
		}
	}
}

func TestTransactionEndpointsParallelStressSnapshots(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC)}
	metrics := memory.NewMetricsStore(clock, memory.MetricsConfig{
		WindowSize:     15,
		BucketDuration: time.Minute,
		Threshold:      -1,
		MinSamples:     0,
		Cooldown:       30 * time.Minute,
	})
	clients := []ports.GatewayClient{
		testGatewayClient{name: domain.GatewayRazorpay},
		testGatewayClient{name: domain.GatewayPayU},
		testGatewayClient{name: domain.GatewayCashfree},
	}
	svc := service.NewPaymentService(
		memory.NewTransactionRepository(),
		metrics,
		[]domain.Gateway{
			{Name: domain.GatewayRazorpay, Weight: 1, Enabled: true},
			{Name: domain.GatewayPayU, Weight: 1, Enabled: true},
			{Name: domain.GatewayCashfree, Weight: 1, Enabled: true},
		},
		clients,
		callback.NewParser(clients),
		nopLogger{},
		clock,
		&seqID{},
		&cyclicRandom{values: []int{0, 1, 2}},
	)
	handler := httpadapter.NewHandler(svc, time.Second).Routes()

	const requestCount = 120
	transactions := make([]domain.Transaction, requestCount)
	runParallel(t, requestCount, func(i int) error {
		var tx domain.Transaction
		status, err := postJSONStress(handler, "/transactions/initiate", map[string]any{
			"order_id": fmt.Sprintf("ORD-STRESS-%03d", i),
			"amount":   float64(100 + i),
			"payment_instrument": map[string]string{
				"type": "card",
			},
		}, &tx)
		if err != nil {
			return err
		}
		if status != http.StatusCreated {
			return fmt.Errorf("initiate %d status got %d want %d", i, status, http.StatusCreated)
		}
		transactions[i] = tx
		return nil
	})

	initCounts := countTransactionsByGateway(transactions)
	assertCount(t, "razorpay initiate count", initCounts[domain.GatewayRazorpay], 40)
	assertCount(t, "payu initiate count", initCounts[domain.GatewayPayU], 40)
	assertCount(t, "cashfree initiate count", initCounts[domain.GatewayCashfree], 40)

	expected := map[domain.GatewayName]struct {
		successes int
		failures  int
	}{
		domain.GatewayRazorpay: {},
		domain.GatewayPayU:     {},
		domain.GatewayCashfree: {},
	}
	for i, tx := range transactions {
		current := expected[tx.Gateway]
		if stressStatus(i) == domain.TransactionStatusFailure {
			current.failures++
		} else {
			current.successes++
		}
		expected[tx.Gateway] = current
	}

	runParallel(t, requestCount, func(i int) error {
		tx := transactions[i]
		status := stressStatus(i)

		code, err := postJSONStress(handler, "/transactions/callback", map[string]string{
			"transaction_id": tx.ID,
			"order_id":       tx.OrderID,
			"gateway":        string(tx.Gateway),
			"status":         string(status),
			"reason":         "stress test",
		}, nil)
		if err != nil {
			return err
		}
		if code != http.StatusOK {
			return fmt.Errorf("callback %d status got %d want %d", i, code, http.StatusOK)
		}
		return nil
	})

	for gateway, want := range expected {
		snapshot, err := metrics.Snapshot(context.Background(), gateway)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.Successes != want.successes || snapshot.Failures != want.failures {
			t.Fatalf("%s snapshot got successes=%d failures=%d want successes=%d failures=%d", gateway, snapshot.Successes, snapshot.Failures, want.successes, want.failures)
		}
		if snapshot.Total != want.successes+want.failures {
			t.Fatalf("%s snapshot total got %d want %d", gateway, snapshot.Total, want.successes+want.failures)
		}
		if !snapshot.Healthy {
			t.Fatalf("%s snapshot got unhealthy: %+v", gateway, snapshot)
		}
	}
}

type cyclicRandom struct {
	mu     sync.Mutex
	values []int
	next   int
}

func (r *cyclicRandom) IntN(n int) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	value := r.values[r.next%len(r.values)]
	r.next++
	return value % n
}

func stressStatus(index int) domain.TransactionStatus {
	if index%4 == 0 {
		return domain.TransactionStatusFailure
	}
	return domain.TransactionStatusSuccess
}

func countTransactionsByGateway(transactions []domain.Transaction) map[domain.GatewayName]int {
	counts := map[domain.GatewayName]int{}
	for _, tx := range transactions {
		counts[tx.Gateway]++
	}
	return counts
}

func assertCount(t *testing.T, name string, got int, want int) {
	t.Helper()
	if got != want {
		t.Fatalf("%s got %d want %d", name, got, want)
	}
}

func runParallel(t *testing.T, count int, fn func(int) error) {
	t.Helper()
	var wg sync.WaitGroup
	errs := make(chan error, count)
	start := make(chan struct{})
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			if err := fn(index); err != nil {
				errs <- err
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func postJSONStress(handler http.Handler, path string, payload any, target any) (int, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if target != nil {
		if err := json.NewDecoder(res.Body).Decode(target); err != nil {
			return res.Code, err
		}
	}
	return res.Code, nil
}

func initiate(t *testing.T, handler http.Handler, orderID string) domain.Transaction {
	t.Helper()
	var tx domain.Transaction
	status := postJSON(t, handler, "/transactions/initiate", map[string]any{
		"order_id": orderID,
		"amount":   499.0,
		"payment_instrument": map[string]string{
			"type": "card",
		},
	}, &tx)
	if status != http.StatusCreated {
		t.Fatalf("initiate status got %d want 201", status)
	}
	return tx
}

func postJSON(t *testing.T, handler http.Handler, path string, payload any, target any) int {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if target != nil {
		if err := json.NewDecoder(res.Body).Decode(target); err != nil {
			t.Fatal(err)
		}
	}
	return res.Code
}
