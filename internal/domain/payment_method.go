package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

type PaymentMethod struct {
	ID                     string `json:"id"`
	CustomerID             string `json:"customer_id"`
	ProviderPaymentMethodID string `json:"provider_payment_method_id"`
	Brand                  string `json:"brand"`
	Last4                  string `json:"last4"`
	ExpiryMonth            int `json:"expiry_month"`
	ExpiryYear             int `json:"expiry_year"`
	IsDefault              bool `json:"is_default"`
	CreatedAt              time.Time `json:"created_at"`
}

func NewPaymentMethod(customerID, providerID, brand, last4 string, month, year int, isDefault bool) (*PaymentMethod, error) {
	if customerID == "" || providerID == "" || brand == "" || len(last4) != 4 {
		return nil, NewValidationError("invalid payment method")
	}
	if month < 1 || month > 12 || year < time.Now().Year() {
		return nil, NewValidationError("invalid card expiry")
	}
	return &PaymentMethod{ID: uuid.NewString(), CustomerID: customerID, ProviderPaymentMethodID: strings.TrimSpace(providerID), Brand: strings.TrimSpace(brand), Last4: last4, ExpiryMonth: month, ExpiryYear: year, IsDefault: isDefault, CreatedAt: time.Now().UTC()}, nil
}