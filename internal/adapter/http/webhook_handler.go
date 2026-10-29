package http

import (
	"io"
	"net/http"

	"goodfood/payment-service/internal/application"
	"goodfood/payment-service/internal/domain"
)

// maxWebhookBytes bounds a provider notification (Stripe events are small).
const maxWebhookBytes = 512 << 10

// EventVerifier authenticates a provider notification.
type EventVerifier interface {
	Verify(payload []byte, signatureHeader string) (*domain.GatewayEvent, error)
}

// WebhookHandler receives the provider's notifications.
type WebhookHandler struct {
	uc       *application.UseCases
	verifier EventVerifier
}

func NewWebhookHandler(uc *application.UseCases, verifier EventVerifier) *WebhookHandler {
	return &WebhookHandler{uc: uc, verifier: verifier}
}

// POST /api/payments/webhooks/stripe
//
// A bad signature is a 400 (Stripe does not retry it). A processing failure
// is a 500 so Stripe retries the delivery later; the use case is idempotent.
func (h *WebhookHandler) Stripe(w http.ResponseWriter, r *http.Request) {
	payload, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBytes))
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "unreadable body")
		return
	}
	event, err := h.verifier.Verify(payload, r.Header.Get("Stripe-Signature"))
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid signature")
		return
	}
	if err := h.uc.HandleProviderEvent(r.Context(), *event); err != nil {
		writeError(w, r, http.StatusInternalServerError, "event not processed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"received": true})
}
