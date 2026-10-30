package stripe

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"goodfood/payment-service/internal/domain"
)

// FakeGateway is the offline demo provider: every intent succeeds as soon as
// it is checked, and cards are Stripe's public test payment methods
// (pm_card_visa, pm_card_mastercard, pm_card_amex). It is refused in
// production by the configuration.
type FakeGateway struct{}

func NewFakeGateway() *FakeGateway { return &FakeGateway{} }

func (f *FakeGateway) CreateIntent(_ context.Context, req domain.IntentRequest, _ string) (*domain.PaymentIntent, error) {
	id := "pi_fake_" + uuid.NewString()
	return &domain.PaymentIntent{
		ID:           id,
		ClientSecret: id + "_secret_fake",
		Status:       "requires_payment_method",
		AmountCents:  req.AmountCents,
		Currency:     req.Currency,
	}, nil
}

// GetIntent reports success so the demo checkout can complete without a card.
func (f *FakeGateway) GetIntent(_ context.Context, intentID string) (*domain.PaymentIntent, error) {
	return &domain.PaymentIntent{ID: intentID, ClientSecret: intentID + "_secret_fake", Status: "succeeded"}, nil
}

func (f *FakeGateway) UpdateIntentAmount(_ context.Context, intentID string, amountCents int64) (*domain.PaymentIntent, error) {
	return &domain.PaymentIntent{ID: intentID, ClientSecret: intentID + "_secret_fake", Status: "requires_payment_method", AmountCents: amountCents}, nil
}

func (f *FakeGateway) CancelIntent(context.Context, string) error { return nil }

func (f *FakeGateway) Refund(context.Context, string, string) error { return nil }

func (f *FakeGateway) CreateCustomer(_ context.Context, customerID string) (string, error) {
	return "cus_fake_" + customerID, nil
}

func (f *FakeGateway) HasCustomer(_ context.Context, providerCustomerID string) (bool, error) {
	return strings.HasPrefix(providerCustomerID, "cus_fake_"), nil
}

func (f *FakeGateway) CreateSetupIntent(_ context.Context, providerCustomerID string) (string, error) {
	return "seti_fake_" + providerCustomerID + "_secret_fake", nil
}

func (f *FakeGateway) GetCustomerCard(_ context.Context, _, providerPaymentMethodID string) (*domain.CardDetails, error) {
	brand, last4 := "Visa", "4242"
	switch {
	case strings.Contains(providerPaymentMethodID, "mastercard"):
		brand, last4 = "Mastercard", "4444"
	case strings.Contains(providerPaymentMethodID, "amex"):
		brand, last4 = "American Express", "8431"
	}
	return &domain.CardDetails{ProviderID: providerPaymentMethodID, Brand: brand, Last4: last4, ExpMonth: 12, ExpYear: 2034}, nil
}

func (f *FakeGateway) DetachCard(context.Context, string) error { return nil }
