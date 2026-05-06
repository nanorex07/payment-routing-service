package memory

import (
	"context"
	"sync"
	"time"

	"payment-routing-service/internal/domain"
)

type TransactionRepository struct {
	mu       sync.RWMutex
	byID     map[string]*domain.Transaction
	orderIDs map[string]string
}

func NewTransactionRepository() *TransactionRepository {
	return &TransactionRepository{
		byID:     make(map[string]*domain.Transaction),
		orderIDs: make(map[string]string),
	}
}

func (r *TransactionRepository) Create(_ context.Context, tx *domain.Transaction) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.orderIDs[tx.OrderID]; exists {
		return domain.ErrDuplicateOrder
	}
	copied := cloneTransaction(tx)
	r.byID[tx.ID] = copied
	r.orderIDs[tx.OrderID] = tx.ID
	return nil
}

func (r *TransactionRepository) ExistsByOrderID(_ context.Context, orderID string) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.orderIDs[orderID]
	return exists, nil
}

func (r *TransactionRepository) FindByID(_ context.Context, id string) (*domain.Transaction, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tx, exists := r.byID[id]
	if !exists {
		return nil, domain.ErrTransactionNotFound
	}
	return cloneTransaction(tx), nil
}

func (r *TransactionRepository) FindByOrderAndGateway(_ context.Context, orderID string, gateway domain.GatewayName) (*domain.Transaction, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, exists := r.orderIDs[orderID]
	if !exists {
		return nil, domain.ErrTransactionNotFound
	}
	tx := r.byID[id]
	if tx.Gateway != gateway {
		return nil, domain.ErrTransactionNotFound
	}
	return cloneTransaction(tx), nil
}

func (r *TransactionRepository) UpdateStatus(_ context.Context, id string, status domain.TransactionStatus, reason string) (*domain.Transaction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	tx, exists := r.byID[id]
	if !exists {
		return nil, domain.ErrTransactionNotFound
	}
	tx.Status = status
	tx.FailureReason = reason
	tx.UpdatedAt = time.Now().UTC()
	return cloneTransaction(tx), nil
}

func (r *TransactionRepository) CountByOrderID(_ context.Context, orderID string) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, exists := r.orderIDs[orderID]; exists {
		return 1, nil
	}
	return 0, nil
}

func cloneTransaction(tx *domain.Transaction) *domain.Transaction {
	if tx == nil {
		return nil
	}
	copied := *tx
	if tx.PaymentInstrument.Metadata != nil {
		copied.PaymentInstrument.Metadata = make(map[string]any, len(tx.PaymentInstrument.Metadata))
		for k, v := range tx.PaymentInstrument.Metadata {
			copied.PaymentInstrument.Metadata[k] = v
		}
	}
	return &copied
}
