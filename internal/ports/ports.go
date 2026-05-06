package ports

import (
	"context"

	"payment-routing-service/internal/domain"
)

type PaymentService interface {
	InitiateTransaction(ctx context.Context, req domain.InitiateRequest) (*domain.InitiateResponse, error)
	ProcessCallback(ctx context.Context, payload []byte) (*domain.CallbackResponse, error)
}

type TransactionRepository interface {
	Create(ctx context.Context, tx *domain.Transaction) error
	ExistsByOrderID(ctx context.Context, orderID string) (bool, error)
	FindByID(ctx context.Context, id string) (*domain.Transaction, error)
	FindByOrderAndGateway(ctx context.Context, orderID string, gateway domain.GatewayName) (*domain.Transaction, error)
	UpdateStatus(ctx context.Context, id string, status domain.TransactionStatus, reason string) (*domain.Transaction, error)
	CountByOrderID(ctx context.Context, orderID string) (int, error)
}

type MetricsStore interface {
	Record(ctx context.Context, gateway domain.GatewayName, success bool) (domain.MetricsSnapshot, error)
	Snapshot(ctx context.Context, gateway domain.GatewayName) (domain.MetricsSnapshot, error)
}

type GatewayClient interface {
	Name() domain.GatewayName
	Initiate(ctx context.Context, tx *domain.Transaction) error
	ParseCallback(payload []byte) (domain.CallbackResult, error)
}

type CallbackParser interface {
	Parse(payload []byte) (domain.CallbackResult, error)
}

type Logger interface {
	Info(ctx context.Context, message string, attrs ...any)
	Error(ctx context.Context, message string, attrs ...any)
}
