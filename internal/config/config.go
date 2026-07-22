package config

import (
	"fmt"
	"os"
)

type Config struct {
	Port            string
	DatabaseURL     string
	JWTSecret       string
	StripeSecretKey string
	LogLevel        string
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:            getEnv("PORT", "8086"),
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		JWTSecret:       os.Getenv("JWT_SECRET"),
		StripeSecretKey: os.Getenv("STRIPE_SECRET_KEY"),
		LogLevel:        getEnv("LOG_LEVEL", "info"),
	}
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.JWTSecret == "" {
		return nil, fmt.Errorf("JWT_SECRET is required")
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
