package config

import "testing"

func baseEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("JWT_SECRET", "secret")
	t.Setenv("SERVICE_API_KEY", "key")
	t.Setenv("ENV", "")
	t.Setenv("PAYMENT_GATEWAY", "")
	t.Setenv("STRIPE_SECRET_KEY", "")
	t.Setenv("STRIPE_WEBHOOK_SECRET", "")
}

func TestLoad_DefaultsToFakeGatewayOutsideProduction(t *testing.T) {
	baseEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Gateway != GatewayFake {
		t.Fatalf("gateway = %q", cfg.Gateway)
	}
}

func TestLoad_PicksStripeWhenAKeyIsSet(t *testing.T) {
	baseEnv(t)
	t.Setenv("STRIPE_SECRET_KEY", "sk_test_123")
	cfg, err := Load()
	if err != nil || cfg.Gateway != GatewayStripe {
		t.Fatalf("got %v, %v", cfg, err)
	}
}

func TestLoad_RefusesUnsafeProductionSetups(t *testing.T) {
	cases := map[string]map[string]string{
		"fake gateway":        {"ENV": "production"},
		"explicit fake":       {"ENV": "production", "PAYMENT_GATEWAY": "fake", "STRIPE_SECRET_KEY": "sk_live_1", "STRIPE_WEBHOOK_SECRET": "whsec"},
		"no webhook secret":   {"ENV": "production", "STRIPE_SECRET_KEY": "sk_live_1"},
		"stripe without key":  {"PAYMENT_GATEWAY": "stripe"},
		"unknown gateway":     {"PAYMENT_GATEWAY": "paypal"},
		"missing service key": {"SERVICE_API_KEY": ""},
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			baseEnv(t)
			for k, v := range env {
				t.Setenv(k, v)
			}
			if _, err := Load(); err == nil {
				t.Fatal("expected a configuration error")
			}
		})
	}
}

func TestLoad_AcceptsProductionStripe(t *testing.T) {
	baseEnv(t)
	t.Setenv("ENV", "production")
	t.Setenv("STRIPE_SECRET_KEY", "sk_live_1")
	t.Setenv("STRIPE_WEBHOOK_SECRET", "whsec_1")
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
}
