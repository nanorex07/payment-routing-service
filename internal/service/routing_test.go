package service

import (
	"testing"

	"payment-routing-service/internal/domain"
)

type fixedRandom struct {
	value int
}

func (r fixedRandom) IntN(int) int {
	return r.value
}

func TestSelectWeighted(t *testing.T) {
	gateways := []domain.Gateway{
		{Name: domain.GatewayRazorpay, Weight: 50, Enabled: true},
		{Name: domain.GatewayPayU, Weight: 30, Enabled: true},
		{Name: domain.GatewayCashfree, Weight: 20, Enabled: true},
	}

	tests := []struct {
		name string
		pick int
		want domain.GatewayName
	}{
		{name: "first bucket", pick: 0, want: domain.GatewayRazorpay},
		{name: "razorpay upper edge", pick: 49, want: domain.GatewayRazorpay},
		{name: "payu lower edge", pick: 50, want: domain.GatewayPayU},
		{name: "payu upper edge", pick: 79, want: domain.GatewayPayU},
		{name: "cashfree lower edge", pick: 80, want: domain.GatewayCashfree},
		{name: "cashfree upper edge", pick: 99, want: domain.GatewayCashfree},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := SelectWeighted(gateways, fixedRandom{value: tt.pick})
			if !ok {
				t.Fatal("expected gateway")
			}
			if got.Name != tt.want {
				t.Fatalf("got %s want %s", got.Name, tt.want)
			}
		})
	}
}

func TestSelectWeightedSkipsDisabledAndZeroWeight(t *testing.T) {
	gateways := []domain.Gateway{
		{Name: domain.GatewayRazorpay, Weight: 0, Enabled: true},
		{Name: domain.GatewayPayU, Weight: 30, Enabled: false},
		{Name: domain.GatewayCashfree, Weight: 20, Enabled: true},
	}

	got, ok := SelectWeighted(gateways, fixedRandom{value: 0})
	if !ok {
		t.Fatal("expected gateway")
	}
	if got.Name != domain.GatewayCashfree {
		t.Fatalf("got %s want %s", got.Name, domain.GatewayCashfree)
	}
}
