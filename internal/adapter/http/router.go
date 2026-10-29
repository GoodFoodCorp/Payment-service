package http

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"
)

type HealthChecker func(ctx context.Context) error

// RouterConfig groups what the router needs besides the handlers.
type RouterConfig struct {
	JWT           JWTConfig
	ServiceAPIKey string
	Log           zerolog.Logger
	DBCheck       HealthChecker
}

func NewRouter(handler *PaymentHandler, webhooks *WebhookHandler, cfg RouterConfig) http.Handler {
	r := chi.NewRouter()

	r.Use(chimw.Recoverer)
	r.Use(RequestID)
	r.Use(Logger(cfg.Log))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/readyz", func(w http.ResponseWriter, req *http.Request) {
		ctx, cancel := context.WithTimeout(req.Context(), 2*time.Second)
		defer cancel()
		if err := cfg.DBCheck(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "db unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})

	// Provider notifications: authenticated by their signature, not by a JWT.
	r.Post("/api/payments/webhooks/stripe", webhooks.Stripe)

	// Customer-facing reads and saved cards. Creating or confirming a payment
	// is deliberately absent: only order-service may do it (internal routes),
	// with the amount it priced itself.
	r.Route("/api/payments", func(r chi.Router) {
		r.Use(Auth(cfg.JWT))
		r.Get("/me", handler.ListMine)
		r.Get("/methods", handler.ListMethods)
		r.Post("/methods/setup-intent", handler.CreateCardSetup)
		r.Post("/methods", handler.AddMethod)
		r.Delete("/methods/{id}", handler.DeleteMethod)
		r.Get("/{orderId}", handler.GetByOrder)
	})

	// Service-to-service routes, never exposed by the ingress (/api only).
	r.Route("/internal/payments", func(r chi.Router) {
		r.Use(ServiceKey(cfg.ServiceAPIKey))
		r.Post("/intents", handler.CreateIntentInternal)
		r.Get("/{orderId}", handler.SyncInternal)
		r.Post("/{orderId}/refund", handler.RefundInternal)
	})

	return r
}
