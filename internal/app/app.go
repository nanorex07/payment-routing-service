package app

import (
	"net/http"
	"time"

	"payment-routing-service/internal/adapters/callback"
	"payment-routing-service/internal/adapters/gateway"
	httpadapter "payment-routing-service/internal/adapters/http"
	"payment-routing-service/internal/adapters/logging"
	"payment-routing-service/internal/adapters/memory"
	"payment-routing-service/internal/domain"
	"payment-routing-service/internal/ports"
	"payment-routing-service/internal/service"
)

type App struct {
	Handler http.Handler
	Service ports.PaymentService
}

func New() *App {
	clock := service.RealClock{}
	logger := logging.NewSlogLogger()
	repo := memory.NewTransactionRepository()
	metrics := memory.NewMetricsStore(clock, memory.DefaultMetricsConfig())
	gateways := DefaultGateways()
	clients := []ports.GatewayClient{
		gateway.NewMockGateway(domain.GatewayRazorpay, 0.98),
		gateway.NewMockGateway(domain.GatewayPayU, 0.96),
		gateway.NewMockGateway(domain.GatewayCashfree, 0.95),
	}
	parser := callback.NewParser(clients)
	paymentService := service.NewPaymentService(
		repo,
		metrics,
		gateways,
		clients,
		parser,
		logger,
		clock,
		service.CryptoIDGenerator{},
		service.RandSource{},
	)
	handler := httpadapter.NewHandler(paymentService, 5*time.Second)
	return &App{Handler: handler.Routes(), Service: paymentService}
}

func DefaultGateways() []domain.Gateway {
	return []domain.Gateway{
		{Name: domain.GatewayRazorpay, Weight: 50, Enabled: true},
		{Name: domain.GatewayPayU, Weight: 30, Enabled: true},
		{Name: domain.GatewayCashfree, Weight: 20, Enabled: true},
	}
}
