package callback

import (
	"encoding/json"
	"strings"

	"payment-routing-service/internal/domain"
	"payment-routing-service/internal/ports"
)

type Parser struct {
	gateways map[domain.GatewayName]ports.GatewayClient
}

func NewParser(gateways []ports.GatewayClient) *Parser {
	gatewayMap := make(map[domain.GatewayName]ports.GatewayClient, len(gateways))
	for _, gateway := range gateways {
		gatewayMap[gateway.Name()] = gateway
	}
	return &Parser{gateways: gatewayMap}
}

func (p *Parser) Parse(payload []byte) (domain.CallbackResult, error) {
	var raw struct {
		Gateway string `json:"gateway"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return domain.CallbackResult{}, domain.ErrInvalidCallback
	}

	gatewayName := domain.GatewayName(strings.ToLower(strings.TrimSpace(raw.Gateway)))
	if gatewayName == "" {
		return domain.CallbackResult{}, domain.ErrInvalidCallback
	}

	gateway, ok := p.gateways[gatewayName]
	if !ok {
		return domain.CallbackResult{}, domain.ErrInvalidCallback
	}
	return gateway.ParseCallback(payload)
}
