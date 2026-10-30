// Package stripe implements the PaymentGateway port against Stripe's REST API
// (no SDK: a handful of form-encoded calls), plus a fake for offline demos.
package stripe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"goodfood/payment-service/internal/domain"
)

const apiBase = "https://api.stripe.com/v1"

// Client is the real Stripe gateway.
type Client struct {
	secretKey string
	baseURL   string
	http      *http.Client
}

// NewClient builds the real gateway for a Stripe secret key.
func NewClient(secretKey string) *Client {
	return &Client{secretKey: secretKey, baseURL: apiBase, http: &http.Client{Timeout: 10 * time.Second}}
}

type apiError struct {
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// statusError is a non-2xx answer from Stripe.
type statusError struct {
	Status  int
	Code    string
	Message string
}

func (e *statusError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("stripe returned %d", e.Status)
	}
	return fmt.Sprintf("stripe %d: %s", e.Status, e.Message)
}

type intentResponse struct {
	ID           string `json:"id"`
	ClientSecret string `json:"client_secret"`
	Status       string `json:"status"`
	Amount       int64  `json:"amount"`
	Currency     string `json:"currency"`
}

func (r intentResponse) toDomain() *domain.PaymentIntent {
	return &domain.PaymentIntent{ID: r.ID, ClientSecret: r.ClientSecret, Status: r.Status, AmountCents: r.Amount, Currency: r.Currency}
}

func (c *Client) CreateIntent(ctx context.Context, req domain.IntentRequest, idempotencyKey string) (*domain.PaymentIntent, error) {
	form := url.Values{}
	form.Set("amount", strconv.FormatInt(req.AmountCents, 10))
	form.Set("currency", req.Currency)
	form.Set("metadata[order_id]", req.OrderID)
	form.Set("automatic_payment_methods[enabled]", "true")
	form.Set("automatic_payment_methods[allow_redirects]", "never")
	if req.ProviderCustomerID != "" {
		form.Set("customer", req.ProviderCustomerID)
	}
	if req.ProviderPaymentMethodID != "" {
		form.Set("payment_method", req.ProviderPaymentMethodID)
	}
	var out intentResponse
	if err := c.do(ctx, http.MethodPost, "/payment_intents", form, idempotencyKey, &out); err != nil {
		return nil, err
	}
	return out.toDomain(), nil
}

func (c *Client) GetIntent(ctx context.Context, intentID string) (*domain.PaymentIntent, error) {
	var out intentResponse
	if err := c.do(ctx, http.MethodGet, "/payment_intents/"+url.PathEscape(intentID), nil, "", &out); err != nil {
		return nil, err
	}
	return out.toDomain(), nil
}

func (c *Client) UpdateIntentAmount(ctx context.Context, intentID string, amountCents int64) (*domain.PaymentIntent, error) {
	form := url.Values{}
	form.Set("amount", strconv.FormatInt(amountCents, 10))
	var out intentResponse
	if err := c.do(ctx, http.MethodPost, "/payment_intents/"+url.PathEscape(intentID), form, "", &out); err != nil {
		return nil, err
	}
	return out.toDomain(), nil
}

func (c *Client) CancelIntent(ctx context.Context, intentID string) error {
	return c.do(ctx, http.MethodPost, "/payment_intents/"+url.PathEscape(intentID)+"/cancel", url.Values{}, "", nil)
}

func (c *Client) Refund(ctx context.Context, intentID, idempotencyKey string) error {
	form := url.Values{}
	form.Set("payment_intent", intentID)
	return c.do(ctx, http.MethodPost, "/refunds", form, idempotencyKey, nil)
}

func (c *Client) CreateCustomer(ctx context.Context, customerID string) (string, error) {
	form := url.Values{}
	form.Set("metadata[customer_id]", customerID)
	var out struct {
		ID string `json:"id"`
	}
	if err := c.do(ctx, http.MethodPost, "/customers", form, "customer-"+customerID, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

func (c *Client) HasCustomer(ctx context.Context, providerCustomerID string) (bool, error) {
	var out struct {
		Deleted bool `json:"deleted"`
	}
	err := c.do(ctx, http.MethodGet, "/customers/"+url.PathEscape(providerCustomerID), nil, "", &out)
	var failure *statusError
	if errors.As(err, &failure) && (failure.Status == http.StatusNotFound || failure.Code == "resource_missing") {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return !out.Deleted, nil
}

func (c *Client) CreateSetupIntent(ctx context.Context, providerCustomerID string) (string, error) {
	form := url.Values{}
	form.Set("customer", providerCustomerID)
	form.Set("usage", "on_session")
	form.Set("automatic_payment_methods[enabled]", "true")
	form.Set("automatic_payment_methods[allow_redirects]", "never")
	var out struct {
		ClientSecret string `json:"client_secret"`
	}
	if err := c.do(ctx, http.MethodPost, "/setup_intents", form, "", &out); err != nil {
		return "", err
	}
	return out.ClientSecret, nil
}

type paymentMethodResponse struct {
	ID             string `json:"id"`
	BillingDetails struct {
		Name string `json:"name"`
	} `json:"billing_details"`
	Card *struct {
		Brand    string `json:"brand"`
		Last4    string `json:"last4"`
		ExpMonth int    `json:"exp_month"`
		ExpYear  int    `json:"exp_year"`
	} `json:"card"`
}

func (c *Client) GetCustomerCard(ctx context.Context, providerCustomerID, providerPaymentMethodID string) (*domain.CardDetails, error) {
	var out paymentMethodResponse
	path := "/customers/" + url.PathEscape(providerCustomerID) + "/payment_methods/" + url.PathEscape(providerPaymentMethodID)
	if err := c.do(ctx, http.MethodGet, path, nil, "", &out); err != nil {
		return nil, err
	}
	if out.Card == nil {
		return nil, fmt.Errorf("payment method %s is not a card", out.ID)
	}
	return &domain.CardDetails{
		ProviderID: out.ID,
		HolderName: out.BillingDetails.Name,
		Brand:      displayBrand(out.Card.Brand),
		Last4:      out.Card.Last4,
		ExpMonth:   out.Card.ExpMonth,
		ExpYear:    out.Card.ExpYear,
	}, nil
}

func (c *Client) DetachCard(ctx context.Context, providerPaymentMethodID string) error {
	return c.do(ctx, http.MethodPost, "/payment_methods/"+url.PathEscape(providerPaymentMethodID)+"/detach", url.Values{}, "", nil)
}

// do sends one API call. A non-2xx status is always an error, whatever the
// body looks like; out may be nil when the response is not needed.
func (c *Client) do(ctx context.Context, method, path string, form url.Values, idempotencyKey string, out any) error {
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.secretKey, "")
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		failure := &statusError{Status: resp.StatusCode}
		var apiErr apiError
		if json.NewDecoder(resp.Body).Decode(&apiErr) == nil && apiErr.Error != nil {
			failure.Code, failure.Message = apiErr.Error.Code, apiErr.Error.Message
		}
		return failure
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func displayBrand(brand string) string {
	switch brand {
	case "visa":
		return "Visa"
	case "mastercard":
		return "Mastercard"
	case "amex":
		return "American Express"
	case "":
		return "Carte"
	default:
		return strings.ToUpper(brand[:1]) + brand[1:]
	}
}
