// Package stripe implements the domain.PaymentGateway port.
//
// Two implementations:
//   - Client: real Stripe REST API calls (test mode keys, sk_test_...)
//   - FakeGateway: in-memory stand-in used when no real key is configured,
//     so the whole flow stays demonstrable offline.
package stripe

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"goodfood/payment-service/internal/domain"
)

const apiBase = "https://api.stripe.com/v1"

// NewGateway picks the real client when a plausible Stripe key is provided,
// the fake otherwise (empty or placeholder value).
func NewGateway(secretKey string) domain.PaymentGateway {
	if secretKey == "" || strings.Contains(secretKey, "placeholder") {
		return NewFakeGateway()
	}
	return &Client{secretKey: secretKey, http: &http.Client{Timeout: 10 * time.Second}}
}

// ── Real Stripe client ──────────────────────────────────────

type Client struct {
	secretKey string
	http      *http.Client
}

type intentResponse struct {
	ID           string `json:"id"`
	ClientSecret string `json:"client_secret"`
	Status       string `json:"status"`
	Error        *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *Client) CreateIntent(ctx context.Context, amountCents int64, currency, orderID string) (*domain.PaymentIntent, error) {
	form := url.Values{}
	form.Set("amount", strconv.FormatInt(amountCents, 10))
	form.Set("currency", currency)
	form.Set("metadata[order_id]", orderID)
	form.Set("automatic_payment_methods[enabled]", "true")

	var out intentResponse
	if err := c.do(ctx, http.MethodPost, "/payment_intents", form, &out); err != nil {
		return nil, err
	}
	return &domain.PaymentIntent{ID: out.ID, ClientSecret: out.ClientSecret, Status: out.Status}, nil
}

func (c *Client) GetIntentStatus(ctx context.Context, intentID string) (string, error) {
	var out intentResponse
	if err := c.do(ctx, http.MethodGet, "/payment_intents/"+intentID, nil, &out); err != nil {
		return "", err
	}
	return out.Status, nil
}

func (c *Client) do(ctx context.Context, method, path string, form url.Values, out *intentResponse) error {
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	req, err := http.NewRequestWithContext(ctx, method, apiBase+path, body)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.secretKey, "")
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return err
	}
	if out.Error != nil {
		return fmt.Errorf("stripe: %s", out.Error.Message)
	}
	return nil
}

// ── Fake gateway (offline demo mode) ────────────────────────

type FakeGateway struct{}

func NewFakeGateway() *FakeGateway { return &FakeGateway{} }

func (f *FakeGateway) CreateIntent(_ context.Context, _ int64, _ string, orderID string) (*domain.PaymentIntent, error) {
	id := "pi_fake_" + uuid.NewString()
	return &domain.PaymentIntent{
		ID:           id,
		ClientSecret: id + "_secret_fake",
		Status:       "requires_payment_method",
	}, nil
}

// GetIntentStatus always succeeds in fake mode so the demo flow can complete.
func (f *FakeGateway) GetIntentStatus(_ context.Context, _ string) (string, error) {
	return "succeeded", nil
}
