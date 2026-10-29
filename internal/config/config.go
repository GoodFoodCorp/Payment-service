package config

import (
	"fmt"
	"os"
	"strings"
)

// Gateway selects the payment provider adapter.
type Gateway string

const (
	GatewayStripe Gateway = "stripe"
	GatewayFake   Gateway = "fake"
)

type Config struct {
	Port                string
	Env                 string
	DatabaseURL         string
	JWTSecret           string
	JWTIssuer           string
	JWTAudience         string
	ServiceAPIKey       string
	Gateway             Gateway
	StripeSecretKey     string
	StripeWebhookSecret string
	RabbitMQURL         string
	LogLevel            string
}

// Load reads the configuration from the environment and fails fast on any
// unsafe combination. In particular the fake gateway — which approves every
// payment — can never run in production, whatever the other settings.
func Load() (*Config, error) {
	cfg := &Config{
		Port:                getEnv("PORT", "8086"),
		Env:                 getEnv("ENV", "development"),
		DatabaseURL:         os.Getenv("DATABASE_URL"),
		JWTSecret:           os.Getenv("JWT_SECRET"),
		JWTIssuer:           getEnv("JWT_ISSUER", "goodfood-auth"),
		JWTAudience:         getEnv("JWT_AUDIENCE", "goodfood-api"),
		ServiceAPIKey:       os.Getenv("SERVICE_API_KEY"),
		StripeSecretKey:     os.Getenv("STRIPE_SECRET_KEY"),
		StripeWebhookSecret: os.Getenv("STRIPE_WEBHOOK_SECRET"),
		RabbitMQURL:         os.Getenv("RABBITMQ_URL"),
		LogLevel:            getEnv("LOG_LEVEL", "info"),
	}
	cfg.Gateway = Gateway(strings.ToLower(os.Getenv("PAYMENT_GATEWAY")))
	if cfg.Gateway == "" {
		cfg.Gateway = GatewayFake
		if cfg.StripeSecretKey != "" && !strings.Contains(cfg.StripeSecretKey, "placeholder") {
			cfg.Gateway = GatewayStripe
		}
	}

	switch {
	case cfg.DatabaseURL == "":
		return nil, fmt.Errorf("DATABASE_URL is required")
	case cfg.JWTSecret == "":
		return nil, fmt.Errorf("JWT_SECRET is required")
	case cfg.ServiceAPIKey == "":
		return nil, fmt.Errorf("SERVICE_API_KEY is required")
	case cfg.Gateway != GatewayStripe && cfg.Gateway != GatewayFake:
		return nil, fmt.Errorf("PAYMENT_GATEWAY must be %q or %q", GatewayStripe, GatewayFake)
	case cfg.Gateway == GatewayStripe && cfg.StripeSecretKey == "":
		return nil, fmt.Errorf("STRIPE_SECRET_KEY is required with the stripe gateway")
	case cfg.IsProduction() && cfg.Gateway == GatewayFake:
		return nil, fmt.Errorf("the fake payment gateway is forbidden in production")
	case cfg.IsProduction() && cfg.StripeWebhookSecret == "":
		return nil, fmt.Errorf("STRIPE_WEBHOOK_SECRET is required in production")
	}
	return cfg, nil
}

func (c *Config) IsProduction() bool { return c.Env == "production" }

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
