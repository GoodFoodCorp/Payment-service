package domain

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
)

// PaymentMethod is a saved card. Demo mode only: the raw card number is never
// persisted, only the brand and last 4 digits derived from it once, exactly
// like a real processor would return — no real charge ever touches it.
type PaymentMethod struct {
	ID             string
	CustomerID     string
	CardholderName string
	Brand          string
	Last4          string
	ExpMonth       int
	ExpYear        int
	IsDefault      bool
	CreatedAt      time.Time
}

type PaymentMethodInput struct {
	CardholderName string
	CardNumber     string
	ExpMonth       int
	ExpYear        int
	IsDefault      bool
}

func detectBrand(digits string) string {
	switch {
	case strings.HasPrefix(digits, "4"):
		return "Visa"
	case strings.HasPrefix(digits, "5"):
		return "Mastercard"
	case strings.HasPrefix(digits, "34"), strings.HasPrefix(digits, "37"):
		return "American Express"
	default:
		return "Carte"
	}
}

// NewPaymentMethod validates the input and derives brand/last4 from the card
// number — the number itself is discarded right after.
func NewPaymentMethod(customerID string, in PaymentMethodInput) (*PaymentMethod, error) {
	if customerID == "" {
		return nil, NewValidationError("customer id is required")
	}
	if strings.TrimSpace(in.CardholderName) == "" {
		return nil, NewValidationError("cardholder name is required")
	}
	digits := strings.ReplaceAll(strings.ReplaceAll(in.CardNumber, " ", ""), "-", "")
	if len(digits) < 12 || len(digits) > 19 {
		return nil, NewValidationError("card number must be between 12 and 19 digits")
	}
	for _, c := range digits {
		if c < '0' || c > '9' {
			return nil, NewValidationError("card number must contain digits only")
		}
	}
	if in.ExpMonth < 1 || in.ExpMonth > 12 {
		return nil, NewValidationError("expiry month must be between 1 and 12")
	}
	now := time.Now().UTC()
	if in.ExpYear < now.Year() || (in.ExpYear == now.Year() && in.ExpMonth < int(now.Month())) {
		return nil, NewValidationError("card has already expired")
	}

	return &PaymentMethod{
		ID:             uuid.NewString(),
		CustomerID:     customerID,
		CardholderName: strings.TrimSpace(in.CardholderName),
		Brand:          detectBrand(digits),
		Last4:          digits[len(digits)-4:],
		ExpMonth:       in.ExpMonth,
		ExpYear:        in.ExpYear,
		IsDefault:      in.IsDefault,
		CreatedAt:      now,
	}, nil
}

// IsOwnedBy reports whether the payment method belongs to the given customer.
func (m *PaymentMethod) IsOwnedBy(customerID string) bool { return m.CustomerID == customerID }

type PaymentMethodRepository interface {
	ListByCustomer(ctx context.Context, customerID string) ([]PaymentMethod, error)
	GetByID(ctx context.Context, id string) (*PaymentMethod, error)
	Create(ctx context.Context, method *PaymentMethod) error
	Delete(ctx context.Context, id string) error
	ClearDefault(ctx context.Context, customerID string) error
}
