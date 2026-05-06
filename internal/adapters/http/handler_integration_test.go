package httpadapter_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
	next int
}

func (g *seqID) NewID() string {
	g.next++
	return fmt.Sprintf("txn_http_%d", g.next)
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
