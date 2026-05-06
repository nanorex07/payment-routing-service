package service

import (
	"math/rand/v2"

	"payment-routing-service/internal/domain"
)

type RandomSource interface {
	IntN(n int) int
}

type RandSource struct{}

func (RandSource) IntN(n int) int {
	return rand.IntN(n)
}

func SelectWeighted(gateways []domain.Gateway, rnd RandomSource) (domain.Gateway, bool) {
	total := 0
	for _, gw := range gateways {
		if gw.Enabled && gw.Weight > 0 {
			total += gw.Weight
		}
	}
	if total == 0 {
		return domain.Gateway{}, false
	}

	pick := rnd.IntN(total)
	running := 0
	for _, gw := range gateways {
		if !gw.Enabled || gw.Weight <= 0 {
			continue
		}
		running += gw.Weight
		if pick < running {
			return gw, true
		}
	}
	return domain.Gateway{}, false
}
