package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"goodfood/payment-service/internal/application"
	"goodfood/payment-service/internal/domain"
)

type PaymentHandler struct {
	uc *application.UseCases
}

func NewPaymentHandler(uc *application.UseCases) *PaymentHandler {
	return &PaymentHandler{uc: uc}
}

type createIntentRequest struct {
	OrderID     string `json:"order_id"`
	AmountCents int64  `json:"amount_cents"`
	Currency    string `json:"currency"`
}

type paymentResponse struct {
	ID           string     `json:"id"`
	OrderID      string     `json:"order_id"`
	IntentID     string     `json:"intent_id"`
	ClientSecret string     `json:"client_secret,omitempty"`
	Status       string     `json:"status"`
	AmountCents  int64      `json:"amount_cents"`
	Currency     string     `json:"currency"`
	PaidAt       *time.Time `json:"paid_at,omitempty"`
}

type errorResponse struct {
	Error     string `json:"error"`
	RequestID string `json:"request_id,omitempty"`
}

type paymentMethodRequest struct {
	CardholderName string `json:"cardholder_name"`
	CardNumber     string `json:"card_number"`
	ExpMonth       int    `json:"exp_month"`
	ExpYear        int    `json:"exp_year"`
	IsDefault      bool   `json:"is_default"`
}

type paymentMethodResponse struct {
	ID             string    `json:"id"`
	CardholderName string    `json:"cardholder_name"`
	Brand          string    `json:"brand"`
	Last4          string    `json:"last4"`
	ExpMonth       int       `json:"exp_month"`
	ExpYear        int       `json:"exp_year"`
	IsDefault      bool      `json:"is_default"`
	CreatedAt      time.Time `json:"created_at"`
}

func toPaymentMethodResponse(m *domain.PaymentMethod) paymentMethodResponse {
	return paymentMethodResponse{
		ID: m.ID, CardholderName: m.CardholderName, Brand: m.Brand, Last4: m.Last4,
		ExpMonth: m.ExpMonth, ExpYear: m.ExpYear, IsDefault: m.IsDefault, CreatedAt: m.CreatedAt,
	}
}

func toResponse(p *domain.Payment, clientSecret string) paymentResponse {
	return paymentResponse{
		ID:           p.ID,
		OrderID:      p.OrderID,
		IntentID:     p.StripeIntentID,
		ClientSecret: clientSecret,
		Status:       string(p.Status),
		AmountCents:  p.AmountCents,
		Currency:     p.Currency,
		PaidAt:       p.PaidAt,
	}
}

// POST /api/payments/intents
func (h *PaymentHandler) CreateIntent(w http.ResponseWriter, r *http.Request) {
	var req createIntentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid JSON body")
		return
	}
	payment, clientSecret, err := h.uc.CreateIntent(r.Context(), actorFrom(r), application.CreateIntentInput{
		OrderID:     req.OrderID,
		AmountCents: req.AmountCents,
		Currency:    req.Currency,
	})
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toResponse(payment, clientSecret))
}

// POST /api/payments/{orderId}/confirm
func (h *PaymentHandler) Confirm(w http.ResponseWriter, r *http.Request) {
	payment, err := h.uc.ConfirmPayment(r.Context(), actorFrom(r), chi.URLParam(r, "orderId"))
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toResponse(payment, ""))
}

// GET /api/payments/{orderId}
func (h *PaymentHandler) GetByOrder(w http.ResponseWriter, r *http.Request) {
	payment, err := h.uc.GetPayment(r.Context(), actorFrom(r), chi.URLParam(r, "orderId"))
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toResponse(payment, ""))
}

// GET /api/payments/me  (the caller's own payment history)
func (h *PaymentHandler) ListMine(w http.ResponseWriter, r *http.Request) {
	list, err := h.uc.ListMyPayments(r.Context(), actorFrom(r))
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	out := make([]paymentResponse, 0, len(list))
	for i := range list {
		out = append(out, toResponse(&list[i], ""))
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /api/payments/methods
func (h *PaymentHandler) ListMethods(w http.ResponseWriter, r *http.Request) {
	list, err := h.uc.ListMyPaymentMethods(r.Context(), actorFrom(r))
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	out := make([]paymentMethodResponse, 0, len(list))
	for i := range list {
		out = append(out, toPaymentMethodResponse(&list[i]))
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/payments/methods  (demo mode — see domain.NewPaymentMethod)
func (h *PaymentHandler) AddMethod(w http.ResponseWriter, r *http.Request) {
	var req paymentMethodRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid JSON body")
		return
	}
	method, err := h.uc.AddPaymentMethod(r.Context(), actorFrom(r), domain.PaymentMethodInput{
		CardholderName: req.CardholderName,
		CardNumber:     req.CardNumber,
		ExpMonth:       req.ExpMonth,
		ExpYear:        req.ExpYear,
		IsDefault:      req.IsDefault,
	})
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toPaymentMethodResponse(method))
}

// DELETE /api/payments/methods/{id}
func (h *PaymentHandler) DeleteMethod(w http.ResponseWriter, r *http.Request) {
	if err := h.uc.DeletePaymentMethod(r.Context(), actorFrom(r), chi.URLParam(r, "id")); err != nil {
		writeDomainError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── JSON helpers ────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	reqID, _ := r.Context().Value(ctxKeyRequestID).(string)
	writeJSON(w, status, errorResponse{Error: msg, RequestID: reqID})
}

func writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	var derr *domain.Error
	if errors.As(err, &derr) {
		status := map[domain.ErrorCode]int{
			domain.ErrCodeValidation: http.StatusBadRequest,
			domain.ErrCodeNotFound:   http.StatusNotFound,
			domain.ErrCodeForbidden:  http.StatusForbidden,
			domain.ErrCodeConflict:   http.StatusConflict,
			domain.ErrCodeGateway:    http.StatusBadGateway,
		}[derr.Code]
		if status == 0 {
			status = http.StatusInternalServerError
		}
		writeError(w, r, status, derr.Message)
		return
	}
	writeError(w, r, http.StatusInternalServerError, "internal server error")
}
