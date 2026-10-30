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
	// StatusPending: the intent exists, the customer has not paid yet.
	StatusPending PaymentStatus = "PENDING"
	// StatusSucceeded: the provider confirmed the charge.
	StatusSucceeded PaymentStatus = "SUCCEEDED"
	// StatusFailed: the last attempt was declined. The intent stays usable,
	// so the customer may retry with another card.
	StatusFailed PaymentStatus = "FAILED"
	// StatusRefunded: the charge was paid back (order cancelled).
	StatusRefunded PaymentStatus = "REFUNDED"
	// StatusCanceled: the order was cancelled before any charge.
	StatusCanceled PaymentStatus = "CANCELED"
)

// Payment is the record of a customer paying for one order. This service is
// the single owner of payment data. The amount is always the one order-service
// computed: nothing a customer sends ever sets it.
type Payment struct {
	ID             string
	OrderID        string
	CustomerID     string
	StripeIntentID string
	Status         PaymentStatus
	AmountCents    int64
	Currency       string
	PaidAt         *time.Time
	RefundedAt     *time.Time
	CreatedAt      time.Time
}

func NewPayment(orderID, customerID, intentID string, amountCents int64, currency string) (*Payment, error) {
	if orderID == "" {
		return nil, NewValidationError("order id is required")
	}
	if customerID == "" {
		return nil, NewValidationError("customer id is required")
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

// IsAwaitingPayment reports whether the customer may (still) pay: a declined
// attempt can be retried on the same intent.
func (p *Payment) IsAwaitingPayment() bool {
	return p.Status == StatusPending || p.Status == StatusFailed
}

// MarkSucceeded stamps the payment as paid.
func (p *Payment) MarkSucceeded() {
	now := time.Now().UTC()
	p.Status = StatusSucceeded
	p.PaidAt = &now
}

// MarkFailed records a declined attempt.
func (p *Payment) MarkFailed() { p.Status = StatusFailed }

// MarkRefunded records the reimbursement of a paid order.
func (p *Payment) MarkRefunded() {
	now := time.Now().UTC()
	p.Status = StatusRefunded
	p.RefundedAt = &now
}

// MarkCanceled records the cancellation of an unpaid intent.
func (p *Payment) MarkCanceled() { p.Status = StatusCanceled }

// IsOwnedBy reports whether the payment belongs to the given customer.
func (p *Payment) IsOwnedBy(customerID string) bool { return p.CustomerID == customerID }

// StatusFromProvider maps a provider intent status onto the payment lifecycle.
// Statuses that still need the customer (requires_action, processing...) keep
// the payment pending.
func StatusFromProvider(providerStatus string) PaymentStatus {
	switch providerStatus {
	case "succeeded":
		return StatusSucceeded
	case "canceled":
		return StatusCanceled
	default:
		return StatusPending
	}
}

// ── Ports ───────────────────────────────────────────────────

// Page bounds a list query.
type Page struct {
	Limit  int
	Offset int
}

type PaymentRepository interface {
	GetByOrderID(ctx context.Context, orderID string) (*Payment, error)
	GetByIntentID(ctx context.Context, intentID string) (*Payment, error)
	// ListByCustomer returns a page of a customer's payment history, most
	// recent first.
	ListByCustomer(ctx context.Context, customerID string, page Page) ([]Payment, error)
	Create(ctx context.Context, payment *Payment) error
	// UpdateAmount changes the amount of a payment still awaiting payment.
	UpdateAmount(ctx context.Context, payment *Payment) error
	// Transition persists payment.Status (and its timestamps) only if the
	// stored status is still from, and records event in the same transaction
	// when the write applies. applied=false means a concurrent update won.
	Transition(ctx context.Context, payment *Payment, from PaymentStatus, event *Event) (applied bool, err error)
}

// IntentRequest is what the provider needs to create an intent.
type IntentRequest struct {
	OrderID     string
	AmountCents int64
	Currency    string
	// ProviderCustomerID and ProviderPaymentMethodID are set when the
	// customer pays with a saved card.
	ProviderCustomerID      string
	ProviderPaymentMethodID string
}

// PaymentIntent is the provider's view of an intent.
type PaymentIntent struct {
	ID           string
	ClientSecret string
	Status       string
	AmountCents  int64
	Currency     string
}

// CardDetails is what the provider tells about a saved card. The card number
// itself never reaches this service.
type CardDetails struct {
	ProviderID string
	HolderName string
	Brand      string
	Last4      string
	ExpMonth   int
	ExpYear    int
}

// PaymentGateway is the outbound port to the payment provider (Stripe, or a
// fake for offline demos).
type PaymentGateway interface {
	// CreateIntent is idempotent per idempotencyKey on the provider side.
	CreateIntent(ctx context.Context, req IntentRequest, idempotencyKey string) (*PaymentIntent, error)
	GetIntent(ctx context.Context, intentID string) (*PaymentIntent, error)
	UpdateIntentAmount(ctx context.Context, intentID string, amountCents int64) (*PaymentIntent, error)
	CancelIntent(ctx context.Context, intentID string) error
	// Refund pays the whole charge of the intent back.
	Refund(ctx context.Context, intentID, idempotencyKey string) error

	CreateCustomer(ctx context.Context, customerID string) (string, error)
	// HasCustomer reports whether the provider knows this customer: a mapping
	// made in demo mode or with another provider account does not.
	HasCustomer(ctx context.Context, providerCustomerID string) (bool, error)
	// CreateSetupIntent lets the browser register a card directly with the
	// provider; it returns the client secret.
	CreateSetupIntent(ctx context.Context, providerCustomerID string) (string, error)
	// GetCustomerCard returns a card only if it is attached to that provider
	// customer: nobody can register a card set up by another account.
	GetCustomerCard(ctx context.Context, providerCustomerID, providerPaymentMethodID string) (*CardDetails, error)
	DetachCard(ctx context.Context, providerPaymentMethodID string) error
}

// GatewayEvent is a verified notification from the provider (webhook).
type GatewayEvent struct {
	ID       string
	Type     string
	IntentID string
}

// Provider event types the service reacts to.
const (
	GatewayIntentSucceeded = "payment_intent.succeeded"
	GatewayIntentFailed    = "payment_intent.payment_failed"
	GatewayIntentCanceled  = "payment_intent.canceled"
	GatewayChargeRefunded  = "charge.refunded"
)
