package gateway

import (
	"context"
	"encoding/json"
	"strings"

	"payment-routing-service/internal/domain"
)

type MockGateway struct {
	name domain.GatewayName
}

func NewMockGateway(name domain.GatewayName) *MockGateway {
	return &MockGateway{name: name}
}

func (g *MockGateway) Name() domain.GatewayName {
	return g.name
}

func (g *MockGateway) Initiate(_ context.Context, _ *domain.Transaction) error {
	return nil
}

func (g *MockGateway) ParseCallback(payload []byte) (domain.CallbackResult, error) {
	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return domain.CallbackResult{}, domain.ErrInvalidCallback
	}

	if result, ok := g.parseGeneric(raw); ok {
		return result, nil
	}

	switch g.name {
	case domain.GatewayRazorpay:
		return g.parseRazorpay(raw)
	case domain.GatewayPayU:
		return g.parsePayU(raw)
	case domain.GatewayCashfree:
		return g.parseCashfree(raw)
	default:
		return domain.CallbackResult{}, domain.ErrInvalidCallback
	}
}

func (g *MockGateway) parseGeneric(raw map[string]any) (domain.CallbackResult, bool) {
	orderID := stringValue(raw, "order_id")
	status := normalizeStatus(stringValue(raw, "status"))
	if orderID == "" || status == "" {
		return domain.CallbackResult{}, false
	}
	return domain.CallbackResult{
		TransactionID: stringValue(raw, "transaction_id"),
		OrderID:       orderID,
		Gateway:       g.name,
		Status:        status,
		Reason:        stringValue(raw, "reason"),
	}, true
}

func (g *MockGateway) parseRazorpay(raw map[string]any) (domain.CallbackResult, error) {
	orderID := stringValue(raw, "razorpay_order_id")
	status := normalizeStatus(stringValue(raw, "event"))
	if orderID == "" || status == "" {
		return domain.CallbackResult{}, domain.ErrInvalidCallback
	}
	return domain.CallbackResult{
		TransactionID: stringValue(raw, "transaction_id"),
		OrderID:       orderID,
		Gateway:       g.name,
		Status:        status,
		Reason:        stringValue(raw, "error_reason"),
	}, nil
}

func (g *MockGateway) parsePayU(raw map[string]any) (domain.CallbackResult, error) {
	orderID := stringValue(raw, "txnid")
	status := normalizeStatus(stringValue(raw, "unmappedstatus"))
	if orderID == "" || status == "" {
		return domain.CallbackResult{}, domain.ErrInvalidCallback
	}
	return domain.CallbackResult{
		TransactionID: stringValue(raw, "transaction_id"),
		OrderID:       orderID,
		Gateway:       g.name,
		Status:        status,
		Reason:        stringValue(raw, "field9"),
	}, nil
}

func (g *MockGateway) parseCashfree(raw map[string]any) (domain.CallbackResult, error) {
	orderID := stringValue(raw, "orderId")
	status := normalizeStatus(stringValue(raw, "txStatus"))
	if orderID == "" || status == "" {
		return domain.CallbackResult{}, domain.ErrInvalidCallback
	}
	return domain.CallbackResult{
		TransactionID: stringValue(raw, "transaction_id"),
		OrderID:       orderID,
		Gateway:       g.name,
		Status:        status,
		Reason:        stringValue(raw, "txMsg"),
	}, nil
}

func stringValue(raw map[string]any, key string) string {
	value, ok := raw[key]
	if !ok || value == nil {
		return ""
	}
	str, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(str)
}

func normalizeStatus(value string) domain.TransactionStatus {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "success", "succeeded", "payment.captured", "captured":
		return domain.TransactionStatusSuccess
	case "failure", "failed", "payment.failed", "failure_completed":
		return domain.TransactionStatusFailure
	default:
		return ""
	}
}
