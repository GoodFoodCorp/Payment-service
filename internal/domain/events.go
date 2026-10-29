package domain

import (
	"time"

	"github.com/google/uuid"
)

// Routing keys of the events this service publishes on goodfood.events.
const (
	EventPaymentSucceeded = "payment.succeeded"
	EventPaymentFailed    = "payment.failed"
	EventPaymentRefunded  = "payment.refunded"
)

// Event is a domain event, stored in the outbox in the same transaction as the
// state change it describes, then relayed to the broker (at-least-once).
type Event struct {
	ID         string
	Type       string
	OccurredAt time.Time
	Data       PaymentEventData
}

// PaymentEventData is the payload shared by every payment event.
type PaymentEventData struct {
	PaymentID   string `json:"paymentId"`
	OrderID     string `json:"orderId"`
	CustomerID  string `json:"customerId"`
	AmountCents int64  `json:"amountCents"`
	Currency    string `json:"currency"`
	Status      string `json:"status"`
}

// NewPaymentEvent snapshots a payment into an event of the given type.
func NewPaymentEvent(eventType string, p *Payment) *Event {
	return &Event{
		ID:         uuid.NewString(),
		Type:       eventType,
		OccurredAt: time.Now().UTC(),
		Data: PaymentEventData{
			PaymentID:   p.ID,
			OrderID:     p.OrderID,
			CustomerID:  p.CustomerID,
			AmountCents: p.AmountCents,
			Currency:    p.Currency,
			Status:      string(p.Status),
		},
	}
}
