FROM golang:1.24 AS builder

WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/payment-routing-service ./cmd/api

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app
COPY --from=builder /out/payment-routing-service /app/payment-routing-service
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/payment-routing-service"]
