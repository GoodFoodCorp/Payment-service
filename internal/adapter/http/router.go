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

func NewRouter(handler *PaymentHandler, jwtSecret string, log zerolog.Logger, dbCheck HealthChecker) http.Handler {
	r := chi.NewRouter()

	r.Use(chimw.Recoverer)
	r.Use(RequestID)
	r.Use(Logger(log))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/readyz", func(w http.ResponseWriter, req *http.Request) {
		ctx, cancel := context.WithTimeout(req.Context(), 2*time.Second)
		defer cancel()
		if err := dbCheck(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "db unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})

	// All payment endpoints require the customer's JWT (forwarded by order-service).
	r.Route("/api/payments", func(r chi.Router) {
		r.Use(Auth(jwtSecret))
		r.Post("/intents", handler.CreateIntent)
		r.Post("/{orderId}/confirm", handler.Confirm)
		r.Get("/{orderId}", handler.GetByOrder)
	})

	return r
}
