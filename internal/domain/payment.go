package domain

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ── Typed business errors ───────────────────────────────────

type ErrorCode string

const (
	ErrCodeValidation ErrorCode = "VALIDATION_ERROR"
	ErrCodeNotFound   ErrorCode = "NOT_FOUND"
	ErrCodeForbidden  ErrorCode = "FORBIDDEN"
	ErrCodeConflict   ErrorCode = "CONFLICT"
	ErrCodeGateway    ErrorCode = "PAYMENT_ERROR"
)

type Error struct {
	Code    ErrorCode
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }

func NewValidationError(msg string) *Error { return &Error{Code: ErrCodeValidation, Message: msg} }
func NewNotFoundError(msg string) *Error   { return &Error{Code: ErrCodeNotFound, Message: msg} }
func NewForbiddenError(msg string) *Error  { return &Error{Code: ErrCodeForbidden, Message: msg} }
func NewConflictError(msg string) *Error   { return &Error{Code: ErrCodeConflict, Message: msg} }
func NewGatewayError(msg string) *Error    { return &Error{Code: ErrCodeGateway, Message: msg} }

// ── Payment aggregate ───────────────────────────────────────

type PaymentStatus string

const (
	StatusPending   PaymentStatus = "PENDING"
	StatusSucceeded PaymentStatus = "SUCCEEDED"
	StatusFailed    PaymentStatus = "FAILED"
)

// Payment is the record of a customer paying for one order. This service is
// the single owner of payment data (extracted from order-service).
type Payment struct {
	ID             string
	OrderID        string
	CustomerID     string
	StripeIntentID string
	Status         PaymentStatus
	AmountCents    int64
	Currency       string
	PaidAt         *time.Time
	CreatedAt      time.Time
}

func NewPayment(orderID, customerID, intentID string, amountCents int64, currency string) (*Payment, error) {
	if orderID == "" {
		return nil, NewValidationError("order id is required")
	}
	if amountCents <= 0 {
		return nil, NewValidationError("amount must be greater than zero")
	}
	if currency == "" {
		currency = "eur"
	}
	return &Payment{
		ID:             uuid.NewString(),
		OrderID:        orderID,
		CustomerID:     customerID,
		StripeIntentID: intentID,
		Status:         StatusPending,
		AmountCents:    amountCents,
		Currency:       currency,
		CreatedAt:      time.Now().UTC(),
	}, nil
}

// MarkSucceeded stamps the payment as paid.
func (p *Payment) MarkSucceeded() {
	now := time.Now().UTC()
	p.Status = StatusSucceeded
	p.PaidAt = &now
}

// MarkFailed records a definitive failure.
func (p *Payment) MarkFailed() { p.Status = StatusFailed }

// IsOwnedBy reports whether the payment belongs to the given customer.
func (p *Payment) IsOwnedBy(customerID string) bool { return p.CustomerID == customerID }

// ── Ports ───────────────────────────────────────────────────

type PaymentRepository interface {
	GetByOrderID(ctx context.Context, orderID string) (*Payment, error)
	Create(ctx context.Context, payment *Payment) error
	Update(ctx context.Context, payment *Payment) error
}

// PaymentIntent is what the provider returns when an intent is created.
type PaymentIntent struct {
	ID           string
	ClientSecret string
	Status       string
}

// PaymentGateway is the outbound port to the payment provider (Stripe test
// mode, or a fake for offline demos).
type PaymentGateway interface {
	CreateIntent(ctx context.Context, amountCents int64, currency, orderID string) (*PaymentIntent, error)
	GetIntentStatus(ctx context.Context, intentID string) (string, error)
}
