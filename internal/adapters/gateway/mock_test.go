package gateway

import (
	"testing"

	"payment-routing-service/internal/domain"
)

func TestMockGatewayParsesProviderSpecificCallbacks(t *testing.T) {
	tests := []struct {
		name    string
		gateway *MockGateway
		payload string
		want    domain.CallbackResult
	}{
		{
			name:    "razorpay",
			gateway: NewMockGateway(domain.GatewayRazorpay),
			payload: `{"gateway":"razorpay","razorpay_order_id":"ORD123","event":"payment.captured","transaction_id":"txn_1"}`,
			want: domain.CallbackResult{
				TransactionID: "txn_1",
				OrderID:       "ORD123",
				Gateway:       domain.GatewayRazorpay,
				Status:        domain.TransactionStatusSuccess,
			},
		},
		{
			name:    "payu",
			gateway: NewMockGateway(domain.GatewayPayU),
			payload: `{"gateway":"payu","txnid":"ORD124","unmappedstatus":"failed","field9":"bank down"}`,
			want: domain.CallbackResult{
				OrderID: "ORD124",
				Gateway: domain.GatewayPayU,
				Status:  domain.TransactionStatusFailure,
				Reason:  "bank down",
			},
		},
		{
			name:    "cashfree",
			gateway: NewMockGateway(domain.GatewayCashfree),
			payload: `{"gateway":"cashfree","orderId":"ORD125","txStatus":"SUCCESS"}`,
			want: domain.CallbackResult{
				OrderID: "ORD125",
				Gateway: domain.GatewayCashfree,
				Status:  domain.TransactionStatusSuccess,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.gateway.ParseCallback([]byte(tt.payload))
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got %+v want %+v", got, tt.want)
			}
		})
	}
}
