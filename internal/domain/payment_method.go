package domain

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
)

// PaymentMethod is a card saved with the provider. The browser hands the card
// to the provider directly (SetupIntent): this service only ever sees the
// provider's payment method id and the display details it returns, so no card
// number crosses our infrastructure (PCI-DSS SAQ A).
type PaymentMethod struct {
	ID                      string
	CustomerID              string
	ProviderPaymentMethodID string
	CardholderName          string
	Brand                   string
	Last4                   string
	ExpMonth                int
	ExpYear                 int
	IsDefault               bool
	CreatedAt               time.Time
}

// PaymentMethodInput registers a card the provider already holds.
type PaymentMethodInput struct {
	ProviderPaymentMethodID string
	IsDefault               bool
}

// NewPaymentMethod builds a saved card from the provider's details.
func NewPaymentMethod(customerID string, card *CardDetails, isDefault bool) (*PaymentMethod, error) {
	if customerID == "" {
		return nil, NewValidationError("customer id is required")
	}
	if card == nil || card.ProviderID == "" {
		return nil, NewValidationError("payment method id is required")
	}
	now := time.Now().UTC()
	if card.ExpYear < now.Year() || (card.ExpYear == now.Year() && card.ExpMonth < int(now.Month())) {
		return nil, NewValidationError("card has already expired")
	}
	return &PaymentMethod{
		ID:                      uuid.NewString(),
		CustomerID:              customerID,
		ProviderPaymentMethodID: card.ProviderID,
		CardholderName:          strings.TrimSpace(card.HolderName),
		Brand:                   card.Brand,
		Last4:                   card.Last4,
		ExpMonth:                card.ExpMonth,
		ExpYear:                 card.ExpYear,
		IsDefault:               isDefault,
		CreatedAt:               now,
	}, nil
}

// IsOwnedBy reports whether the payment method belongs to the given customer.
func (m *PaymentMethod) IsOwnedBy(customerID string) bool { return m.CustomerID == customerID }

type PaymentMethodRepository interface {
	ListByCustomer(ctx context.Context, customerID string) ([]PaymentMethod, error)
	GetByID(ctx context.Context, id string) (*PaymentMethod, error)
	// Create stores the method; when it is the default, the previous default
	// is cleared in the same transaction.
	Create(ctx context.Context, method *PaymentMethod) error
	Delete(ctx context.Context, id string) error
	// DeleteAllOf removes a customer's saved cards (their provider customer
	// was replaced: the cards belonged to the old one).
	DeleteAllOf(ctx context.Context, customerID string) error
}

// CustomerRepository maps our customers to the provider's customer objects,
// which saved cards are attached to.
type CustomerRepository interface {
	GetProviderCustomerID(ctx context.Context, customerID string) (string, error)
	SaveProviderCustomerID(ctx context.Context, customerID, providerCustomerID string) error
	// ReplaceProviderCustomerID swaps a stale mapping, only if it is still
	// the stale one (a concurrent replacement wins).
	ReplaceProviderCustomerID(ctx context.Context, customerID, stale, replacement string) error
}
