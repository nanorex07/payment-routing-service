package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"payment-routing-service/internal/domain"
	"payment-routing-service/internal/ports"
)

type PaymentService struct {
	repo     ports.TransactionRepository
	metrics  ports.MetricsStore
	gateways []domain.Gateway
	clients  map[domain.GatewayName]ports.GatewayClient
	parser   ports.CallbackParser
	logger   ports.Logger
	clock    Clock
	idgen    IDGenerator
	random   RandomSource
}

func NewPaymentService(
	repo ports.TransactionRepository,
	metrics ports.MetricsStore,
	gateways []domain.Gateway,
	clients []ports.GatewayClient,
	parser ports.CallbackParser,
	logger ports.Logger,
	clock Clock,
	idgen IDGenerator,
	random RandomSource,
) *PaymentService {
	clientMap := make(map[domain.GatewayName]ports.GatewayClient, len(clients))
	for _, client := range clients {
		clientMap[client.Name()] = client
	}
	return &PaymentService{
		repo:     repo,
		metrics:  metrics,
		gateways: gateways,
		clients:  clientMap,
		parser:   parser,
		logger:   logger,
		clock:    clock,
		idgen:    idgen,
		random:   random,
	}
}

func (s *PaymentService) InitiateTransaction(ctx context.Context, req domain.InitiateRequest) (*domain.InitiateResponse, error) {
	req.OrderID = strings.TrimSpace(req.OrderID)
	req.PaymentInstrument.Type = strings.TrimSpace(req.PaymentInstrument.Type)
	if req.OrderID == "" || req.Amount <= 0 || req.PaymentInstrument.Type == "" {
		return nil, domain.ErrInvalidRequest
	}

	s.logger.Info(ctx, "initiate request received", slog.String("order_id", req.OrderID))
	exists, err := s.repo.ExistsByOrderID(ctx, req.OrderID)
	if err != nil {
		return nil, err
	}
	if exists {
		s.logger.Info(ctx, "initiate duplicate rejected", slog.String("order_id", req.OrderID), slog.String("decision", "reject_duplicate"))
		return nil, domain.ErrDuplicateOrder
	}

	healthy := make([]domain.Gateway, 0, len(s.gateways))
	for _, gw := range s.gateways {
		if !gw.Enabled {
			continue
		}
		blockStatus, err := s.metrics.BlockStatus(ctx, gw.Name)
		if err != nil {
			return nil, err
		}
		if !blockStatus.Blocked {
			healthy = append(healthy, gw)
			s.logger.Info(ctx, "gateway eligible", slog.String("order_id", req.OrderID), slog.String("gateway", string(gw.Name)))
			continue
		}
		attrs := []any{slog.String("order_id", req.OrderID), slog.String("gateway", string(gw.Name)), slog.String("decision", "skip_blocked"), slog.String("reason", blockStatus.Reason)}
		if blockStatus.BlockedUntil != nil {
			attrs = append(attrs, slog.Time("blocked_until", *blockStatus.BlockedUntil))
		}
		s.logger.Info(ctx, "gateway skipped", attrs...)
	}

	selected, ok := SelectWeighted(healthy, s.random)
	if !ok {
		s.logger.Info(ctx, "initiate failed no healthy gateway", slog.String("order_id", req.OrderID), slog.String("decision", "no_healthy_gateway"))
		return nil, domain.ErrNoHealthyGateway
	}

	now := s.clock.Now()
	tx := &domain.Transaction{
		ID:                s.idgen.NewID(),
		OrderID:           req.OrderID,
		Amount:            req.Amount,
		PaymentInstrument: req.PaymentInstrument,
		Gateway:           selected.Name,
		Status:            domain.TransactionStatusPending,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	s.logger.Info(ctx, "gateway selected", slog.String("order_id", tx.OrderID), slog.String("transaction_id", tx.ID), slog.String("gateway", string(tx.Gateway)), slog.String("decision", "selected"))

	client, ok := s.clients[selected.Name]
	if !ok {
		s.logger.Error(ctx, "gateway client unavailable", slog.String("order_id", tx.OrderID), slog.String("transaction_id", tx.ID), slog.String("gateway", string(tx.Gateway)))
		return nil, domain.ErrGatewayUnavailable
	}
	if err := s.repo.Create(ctx, tx); err != nil {
		if errors.Is(err, domain.ErrDuplicateOrder) {
			s.logger.Info(ctx, "initiate duplicate rejected on create", slog.String("order_id", tx.OrderID), slog.String("decision", "reject_duplicate"))
		}
		return nil, err
	}
	s.logger.Info(ctx, "transaction persisted", slog.String("order_id", tx.OrderID), slog.String("transaction_id", tx.ID), slog.String("gateway", string(tx.Gateway)), slog.String("status", string(tx.Status)))
	if err := client.Initiate(ctx, tx); err != nil {
		s.logger.Error(ctx, "gateway initiate failed", slog.String("order_id", tx.OrderID), slog.String("transaction_id", tx.ID), slog.String("gateway", string(tx.Gateway)), slog.String("reason", err.Error()))
		return nil, err
	}
	return &domain.InitiateResponse{Transaction: tx}, nil
}

func (s *PaymentService) ProcessCallback(ctx context.Context, payload []byte) (*domain.CallbackResponse, error) {
	s.logger.Info(ctx, "callback request received")
	result, err := s.parser.Parse(payload)
	if err != nil {
		s.logger.Error(ctx, "callback parse failed", slog.String("reason", err.Error()))
		return nil, err
	}
	if result.Status != domain.TransactionStatusSuccess && result.Status != domain.TransactionStatusFailure {
		return nil, domain.ErrInvalidCallback
	}
	s.logger.Info(ctx, "callback parsed", slog.String("order_id", result.OrderID), slog.String("transaction_id", result.TransactionID), slog.String("gateway", string(result.Gateway)), slog.String("status", string(result.Status)))

	var tx *domain.Transaction
	if result.TransactionID != "" {
		tx, err = s.repo.FindByID(ctx, result.TransactionID)
	} else {
		tx, err = s.repo.FindByOrderAndGateway(ctx, result.OrderID, result.Gateway)
	}
	if err != nil {
		return nil, err
	}
	if tx.Gateway != result.Gateway {
		return nil, domain.ErrInvalidCallback
	}

	updated, err := s.repo.UpdateStatus(ctx, tx.ID, result.Status, result.Reason)
	if err != nil {
		return nil, err
	}
	s.logger.Info(ctx, "transaction status updated", slog.String("order_id", updated.OrderID), slog.String("transaction_id", updated.ID), slog.String("gateway", string(updated.Gateway)), slog.String("status", string(updated.Status)))

	snapshot, err := s.metrics.Record(ctx, updated.Gateway, updated.Status == domain.TransactionStatusSuccess)
	if err != nil {
		return nil, err
	}
	attrs := []any{slog.String("order_id", updated.OrderID), slog.String("transaction_id", updated.ID), slog.String("gateway", string(updated.Gateway)), slog.Float64("success_rate", snapshot.SuccessRate), slog.Int("sample_count", snapshot.Total), slog.Bool("healthy", snapshot.Healthy), slog.String("reason", snapshot.Reason)}
	if snapshot.BlockedUntil != nil {
		attrs = append(attrs, slog.Time("blocked_until", *snapshot.BlockedUntil))
	}
	s.logger.Info(ctx, "metrics recorded", attrs...)
	return &domain.CallbackResponse{Transaction: updated, Metrics: snapshot}, nil
}
