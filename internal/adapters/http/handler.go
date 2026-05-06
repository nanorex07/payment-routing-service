package httpadapter

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"payment-routing-service/internal/domain"
	"payment-routing-service/internal/ports"
)

type Handler struct {
	service ports.PaymentService
	timeout time.Duration
}

func NewHandler(service ports.PaymentService, timeout time.Duration) *Handler {
	return &Handler{service: service, timeout: timeout}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.health)
	mux.HandleFunc("POST /transactions/initiate", h.initiate)
	mux.HandleFunc("POST /transactions/callback", h.callback)
	return requestLogMiddleware(mux)
}

func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) initiate(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()

	var req domain.InitiateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return
	}

	res, err := h.service.InitiateTransaction(ctx, req)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, res.Transaction)
}

func (h *Handler) callback(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()

	var raw json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return
	}
	res, err := h.service.ProcessCallback(ctx, raw)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func requestLogMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
}

func writeDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidRequest), errors.Is(err, domain.ErrInvalidCallback):
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
	case errors.Is(err, domain.ErrDuplicateOrder):
		writeError(w, http.StatusConflict, "duplicate_order", err.Error())
	case errors.Is(err, domain.ErrNoHealthyGateway):
		writeError(w, http.StatusServiceUnavailable, "no_healthy_gateway", err.Error())
	case errors.Is(err, domain.ErrGatewayUnavailable):
		writeError(w, http.StatusServiceUnavailable, "gateway_unavailable", err.Error())
	case errors.Is(err, domain.ErrTransactionNotFound):
		writeError(w, http.StatusNotFound, "transaction_not_found", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}

func writeError(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, map[string]string{"error": code, "message": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
