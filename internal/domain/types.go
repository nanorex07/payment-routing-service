package domain

import (
	"errors"
	"time"
)

type GatewayName string

const (
	GatewayRazorpay GatewayName = "razorpay"
	GatewayPayU     GatewayName = "payu"
	GatewayCashfree GatewayName = "cashfree"
)

type TransactionStatus string

const (
	TransactionStatusPending TransactionStatus = "pending"
	TransactionStatusSuccess TransactionStatus = "success"
	TransactionStatusFailure TransactionStatus = "failure"
)

var (
	ErrDuplicateOrder      = errors.New("transaction already exists for order")
	ErrNoHealthyGateway    = errors.New("no healthy gateway available")
	ErrTransactionNotFound = errors.New("transaction not found")
	ErrGatewayUnavailable  = errors.New("gateway client unavailable")
	ErrInvalidCallback     = errors.New("invalid callback payload")
	ErrInvalidRequest      = errors.New("invalid request")
)

type PaymentInstrument struct {
	Type       string         `json:"type"`
	CardNumber string         `json:"card_number,omitempty"`
	Expiry     string         `json:"expiry,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type Transaction struct {
	ID                string            `json:"transaction_id"`
	OrderID           string            `json:"order_id"`
	Amount            float64           `json:"amount"`
	PaymentInstrument PaymentInstrument `json:"payment_instrument,omitempty"`
	Gateway           GatewayName       `json:"gateway"`
	Status            TransactionStatus `json:"status"`
	FailureReason     string            `json:"reason,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

type Gateway struct {
	Name    GatewayName `json:"name"`
	Weight  int         `json:"weight"`
	Enabled bool        `json:"enabled"`
}

type InitiateRequest struct {
	OrderID           string            `json:"order_id"`
	Amount            float64           `json:"amount"`
	PaymentInstrument PaymentInstrument `json:"payment_instrument"`
}

type InitiateResponse struct {
	Transaction *Transaction `json:"transaction"`
}

type CallbackResult struct {
	TransactionID string            `json:"transaction_id,omitempty"`
	OrderID       string            `json:"order_id"`
	Gateway       GatewayName       `json:"gateway"`
	Status        TransactionStatus `json:"status"`
	Reason        string            `json:"reason,omitempty"`
}

type CallbackResponse struct {
	Transaction *Transaction    `json:"transaction"`
	Metrics     MetricsSnapshot `json:"metrics"`
}

type MetricsSnapshot struct {
	Gateway      GatewayName `json:"gateway"`
	Successes    int         `json:"successes"`
	Failures     int         `json:"failures"`
	Total        int         `json:"total"`
	SuccessRate  float64     `json:"success_rate"`
	Healthy      bool        `json:"healthy"`
	BlockedUntil *time.Time  `json:"blocked_until,omitempty"`
	Reason       string      `json:"reason,omitempty"`
}

type GatewayBlockStatus struct {
	Gateway      GatewayName
	Blocked      bool
	BlockedUntil *time.Time
	Reason       string
}
