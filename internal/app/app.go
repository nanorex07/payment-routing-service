package app

import (
	"net/http"
	"os"
	"time"

	redisc "github.com/redis/go-redis/v9"

	"payment-routing-service/internal/adapters/callback"
	"payment-routing-service/internal/adapters/gateway"
	httpadapter "payment-routing-service/internal/adapters/http"
	"payment-routing-service/internal/adapters/logging"
	"payment-routing-service/internal/adapters/memory"
	redisadapter "payment-routing-service/internal/adapters/redis"
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
	metrics := newMetricsStore(clock)
	gateways := DefaultGateways()
	clients := []ports.GatewayClient{
		gateway.NewMockGateway(domain.GatewayRazorpay),
		gateway.NewMockGateway(domain.GatewayPayU),
		gateway.NewMockGateway(domain.GatewayCashfree),
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

func newMetricsStore(clock service.Clock) ports.MetricsStore {
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		return memory.NewMetricsStore(clock, memory.DefaultMetricsConfig())
	}

	options := &redisc.Options{Addr: redisAddr}
	if password := os.Getenv("REDIS_PASSWORD"); password != "" {
		options.Password = password
	}

	redisClient := redisc.NewClient(options)
	redisOptions := []redisadapter.Option{}
	if prefix := os.Getenv("REDIS_METRICS_PREFIX"); prefix != "" {
		redisOptions = append(redisOptions, redisadapter.WithKeyPrefix(prefix))
	}
	return redisadapter.NewMetricsStore(redisClient, clock, redisadapter.DefaultMetricsConfig(), redisOptions...)
}

func DefaultGateways() []domain.Gateway {
	return []domain.Gateway{
		{Name: domain.GatewayRazorpay, Weight: 50, Enabled: true},
		{Name: domain.GatewayPayU, Weight: 30, Enabled: true},
		{Name: domain.GatewayCashfree, Weight: 20, Enabled: true},
	}
}
