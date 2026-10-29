package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	httpadapter "goodfood/payment-service/internal/adapter/http"
	"goodfood/payment-service/internal/adapter/messaging"
	"goodfood/payment-service/internal/adapter/outbox"
	"goodfood/payment-service/internal/adapter/postgres"
	"goodfood/payment-service/internal/adapter/stripe"
	"goodfood/payment-service/internal/application"
	"goodfood/payment-service/internal/config"
	"goodfood/payment-service/internal/domain"
)

func main() {
	log := newLogger()

	cfg, err := config.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("invalid configuration")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := connectWithRetry(ctx, log, cfg.DatabaseURL)
	if err != nil {
		log.Fatal().Err(err).Msg("database unreachable")
	}
	defer pool.Close()

	if err := postgres.Migrate(cfg.DatabaseURL); err != nil {
		log.Fatal().Err(err).Msg("migrations failed")
	}
	log.Info().Msg("migrations applied")

	var gateway domain.PaymentGateway
	if cfg.Gateway == config.GatewayStripe {
		gateway = stripe.NewClient(cfg.StripeSecretKey)
		log.Info().Msg("payments processed by Stripe")
	} else {
		gateway = stripe.NewFakeGateway()
		log.Warn().Msg("fake payment gateway: every payment is approved (demo mode, refused in production)")
	}
	if cfg.StripeWebhookSecret == "" {
		log.Warn().Msg("STRIPE_WEBHOOK_SECRET not set: webhooks are rejected, payments are confirmed by order-service only")
	}

	uc := application.NewUseCases(
		postgres.NewPaymentRepository(pool),
		gateway,
		postgres.NewPaymentMethodRepository(pool),
		postgres.NewCustomerRepository(pool),
	)

	router := httpadapter.NewRouter(
		httpadapter.NewPaymentHandler(uc),
		httpadapter.NewWebhookHandler(uc, stripe.NewWebhookVerifier(cfg.StripeWebhookSecret)),
		httpadapter.RouterConfig{
			JWT:           httpadapter.JWTConfig{Secret: cfg.JWTSecret, Issuer: cfg.JWTIssuer, Audience: cfg.JWTAudience},
			ServiceAPIKey: cfg.ServiceAPIKey,
			Log:           log,
			DBCheck:       func(ctx context.Context) error { return pool.Ping(ctx) },
		},
	)

	runCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if cfg.RabbitMQURL != "" {
		publisher := messaging.NewAMQPPublisher(cfg.RabbitMQURL)
		defer publisher.Close()
		go outbox.NewRelay(postgres.NewOutboxRepository(pool), publisher, log).Run(runCtx)
		log.Info().Msg("payment events relayed to rabbitmq")
	} else {
		log.Warn().Msg("RABBITMQ_URL not set: payment events stay in the outbox until a broker is configured")
	}

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: router, ReadHeaderTimeout: 5 * time.Second}
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-runCtx.Done()
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelShutdown()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Error().Err(err).Msg("graceful shutdown failed")
		}
	}()
	log.Info().Str("port", cfg.Port).Msg("payment-service started")
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal().Err(err).Msg("server stopped")
	}
	<-done
	log.Info().Msg("payment-service stopped")
}

func newLogger() zerolog.Logger {
	level, err := zerolog.ParseLevel(strings.ToLower(os.Getenv("LOG_LEVEL")))
	if err != nil || level == zerolog.NoLevel {
		level = zerolog.InfoLevel
	}
	return zerolog.New(os.Stdout).Level(level).With().Timestamp().Str("service", "payment-service").Logger()
}

func connectWithRetry(ctx context.Context, log zerolog.Logger, url string) (*pgxpool.Pool, error) {
	for i := 1; ; i++ {
		pool, err := postgres.Connect(ctx, url)
		if err == nil {
			return pool, nil
		}
		if i >= 15 {
			return nil, err
		}
		log.Warn().Err(err).Int("attempt", i).Msg("database not ready, retrying in 2s")
		select {
		case <-time.After(2 * time.Second):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}
