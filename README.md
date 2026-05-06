# payment-routing-service

Dynamic payment gateway routing service in Go.

## Run

```sh
go run ./cmd/api
```

Docker:

```sh
docker compose up api
```

Tests:

```sh
go test ./...
docker compose run --rm test
```

## APIs

### Initiate Transaction

```sh
curl -i -X POST localhost:8080/transactions/initiate \
  -H 'content-type: application/json' \
  -d '{
    "order_id": "ORD123",
    "amount": 499.0,
    "payment_instrument": {
      "type": "card",
      "card_number": "****",
      "expiry": "12/29"
    }
  }'
```

Duplicate `order_id` returns `409 Conflict`.

### Callback

Generic:

```sh
curl -i -X POST localhost:8080/transactions/callback \
  -H 'content-type: application/json' \
  -d '{
    "order_id": "ORD123",
    "gateway": "razorpay",
    "status": "success"
  }'
```

Gateway-specific payloads are parsed by callback adapters for Razorpay, PayU, and Cashfree.
Every callback payload must include `gateway`; that key selects provider-specific parsing.

## Routing Defaults

- Razorpay: 50
- PayU: 30
- Cashfree: 20
- Sliding window: 15 one-minute buckets
- Unhealthy threshold: success rate below 90%
- Minimum sample size: 10
- Cooldown: 30 minutes

All transaction and metrics storage is in memory behind ports.
