package callback

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"payment-routing-service/internal/domain"
	"payment-routing-service/internal/ports"
)

type parserTestGateway struct {
	name  domain.GatewayName
	calls int
}

func (g *parserTestGateway) Name() domain.GatewayName {
	return g.name
}

func (*parserTestGateway) Initiate(context.Context, *domain.Transaction) error {
	return nil
}

func (g *parserTestGateway) ParseCallback(payload []byte) (domain.CallbackResult, error) {
	g.calls++
	var raw struct {
		OrderID string `json:"order_id"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return domain.CallbackResult{}, err
	}
	return domain.CallbackResult{OrderID: raw.OrderID, Gateway: g.name, Status: domain.TransactionStatusSuccess}, nil
}

func TestParserDelegatesToGatewayFromPayload(t *testing.T) {
	razorpay := &parserTestGateway{name: domain.GatewayRazorpay}
	payu := &parserTestGateway{name: domain.GatewayPayU}
	parser := NewParser([]ports.GatewayClient{
		razorpay,
		payu,
	})

	result, err := parser.Parse([]byte(`{"gateway":"payu","order_id":"ORD123"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.Gateway != domain.GatewayPayU {
		t.Fatalf("got gateway %s want payu", result.Gateway)
	}
	if razorpay.calls != 0 {
		t.Fatalf("razorpay parser called %d times", razorpay.calls)
	}
	if payu.calls != 1 {
		t.Fatalf("payu parser called %d times", payu.calls)
	}
}

func TestParserRejectsMissingOrUnknownGateway(t *testing.T) {
	parser := NewParser(nil)

	tests := [][]byte{
		[]byte(`{"order_id":"ORD123"}`),
		[]byte(`{"gateway":"unknown","order_id":"ORD123"}`),
	}
	for _, payload := range tests {
		_, err := parser.Parse(payload)
		if !errors.Is(err, domain.ErrInvalidCallback) {
			t.Fatalf("payload %s got err %v want invalid callback", payload, err)
		}
	}
}
