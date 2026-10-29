package application

import (
	"context"
	"errors"
	"fmt"

	"goodfood/payment-service/internal/domain"
)

// OrderIntentInput is sent by order-service over the internal API. The amount
// is the one order-service priced from the catalog: customers never reach this
// use case, so they can never choose what they pay.
type OrderIntentInput struct {
	OrderID     string
	CustomerID  string
	AmountCents int64
	Currency    string
	// PaymentMethodID optionally selects one of the customer's saved cards.
	PaymentMethodID string
}

// IntentOutput is the payment plus the client secret the browser needs to
// complete it with the provider.
type IntentOutput struct {
	Payment      *domain.Payment
	ClientSecret string
}

// CreateIntentForOrder creates (or resumes) the payment of an order.
// Idempotent: a payment still awaiting the customer is reused, its amount
// realigned on the order's if it changed, and its client secret returned again.
func (uc *UseCases) CreateIntentForOrder(ctx context.Context, in OrderIntentInput) (*IntentOutput, error) {
	if in.OrderID == "" || in.CustomerID == "" {
		return nil, domain.NewValidationError("order id and customer id are required")
	}
	if in.AmountCents <= 0 {
		return nil, domain.NewValidationError("amount must be greater than zero")
	}
	if in.Currency == "" {
		in.Currency = "eur"
	}

	existing, err := uc.payments.GetByOrderID(ctx, in.OrderID)
	switch {
	case err == nil:
		return uc.resumeIntent(ctx, existing, in)
	case !isNotFound(err):
		return nil, err
	}

	req := domain.IntentRequest{OrderID: in.OrderID, AmountCents: in.AmountCents, Currency: in.Currency}
	if in.PaymentMethodID != "" {
		if err := uc.withSavedCard(ctx, in.CustomerID, in.PaymentMethodID, &req); err != nil {
			return nil, err
		}
	}
	// The key covers the amount: a re-priced order gets a new intent instead of
	// an idempotency mismatch, while concurrent identical calls get one intent.
	idempotencyKey := fmt.Sprintf("intent-%s-%d", in.OrderID, in.AmountCents)
	intent, err := uc.gateway.CreateIntent(ctx, req, idempotencyKey)
	if err != nil {
		return nil, domain.NewGatewayError("payment provider error: " + err.Error())
	}

	payment, err := domain.NewPayment(in.OrderID, in.CustomerID, intent.ID, in.AmountCents, in.Currency)
	if err != nil {
		return nil, err
	}
	if err := uc.payments.Create(ctx, payment); err != nil {
		var derr *domain.Error
		if errors.As(err, &derr) && derr.Code == domain.ErrCodeConflict {
			// A concurrent call recorded the payment first: resume it.
			current, getErr := uc.payments.GetByOrderID(ctx, in.OrderID)
			if getErr != nil {
				return nil, getErr
			}
			return uc.resumeIntent(ctx, current, in)
		}
		return nil, err
	}
	if err := uc.applyProviderStatus(ctx, payment, intent.Status); err != nil {
		return nil, err
	}
	return &IntentOutput{Payment: payment, ClientSecret: intent.ClientSecret}, nil
}

func (uc *UseCases) resumeIntent(ctx context.Context, p *domain.Payment, in OrderIntentInput) (*IntentOutput, error) {
	if !p.IsOwnedBy(in.CustomerID) {
		return nil, domain.NewForbiddenError("this order's payment belongs to another customer")
	}
	if !p.IsAwaitingPayment() {
		return nil, domain.NewConflictError("this order has already been paid or closed")
	}
	intent, err := uc.gateway.GetIntent(ctx, p.StripeIntentID)
	if err != nil {
		return nil, domain.NewGatewayError("payment provider error: " + err.Error())
	}
	if p.AmountCents != in.AmountCents {
		if intent, err = uc.gateway.UpdateIntentAmount(ctx, p.StripeIntentID, in.AmountCents); err != nil {
			return nil, domain.NewGatewayError("payment provider error: " + err.Error())
		}
		p.AmountCents = in.AmountCents
		if err := uc.payments.UpdateAmount(ctx, p); err != nil {
			return nil, err
		}
	}
	if err := uc.applyProviderStatus(ctx, p, intent.Status); err != nil {
		return nil, err
	}
	return &IntentOutput{Payment: p, ClientSecret: intent.ClientSecret}, nil
}

func (uc *UseCases) withSavedCard(ctx context.Context, customerID, methodID string, req *domain.IntentRequest) error {
	method, err := uc.paymentMethods.GetByID(ctx, methodID)
	if err != nil {
		return err
	}
	if !method.IsOwnedBy(customerID) {
		return domain.NewForbiddenError("this payment method belongs to another user")
	}
	if method.ProviderPaymentMethodID == "" {
		return domain.NewValidationError("this card must be registered again before it can be used")
	}
	providerCustomer, err := uc.customers.GetProviderCustomerID(ctx, customerID)
	if err != nil {
		return err
	}
	req.ProviderCustomerID = providerCustomer
	req.ProviderPaymentMethodID = method.ProviderPaymentMethodID
	return nil
}

// SyncPayment returns an order's payment after reconciling it with the
// provider, so a payment completed in the browser is seen even before (or
// without) its webhook. Used by order-service to confirm an order.
func (uc *UseCases) SyncPayment(ctx context.Context, orderID string) (*domain.Payment, error) {
	payment, err := uc.payments.GetByOrderID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if !payment.IsAwaitingPayment() {
		return payment, nil
	}
	intent, err := uc.gateway.GetIntent(ctx, payment.StripeIntentID)
	if err != nil {
		return nil, domain.NewGatewayError("payment provider error: " + err.Error())
	}
	if err := uc.applyProviderStatus(ctx, payment, intent.Status); err != nil {
		return nil, err
	}
	return payment, nil
}

// RefundOrder pays a cancelled order back. Idempotent: an order already
// refunded (or never charged) is left as is. A payment still awaiting the
// customer is checked with the provider first, so a charge that went through
// at the last moment is refunded rather than silently kept.
func (uc *UseCases) RefundOrder(ctx context.Context, orderID string) (*domain.Payment, error) {
	payment, err := uc.SyncPayment(ctx, orderID)
	if err != nil {
		return nil, err
	}
	switch payment.Status {
	case domain.StatusRefunded, domain.StatusCanceled:
		return payment, nil
	case domain.StatusSucceeded:
		if err := uc.gateway.Refund(ctx, payment.StripeIntentID, "refund-"+payment.OrderID); err != nil {
			return nil, domain.NewGatewayError("payment provider error: " + err.Error())
		}
		payment.MarkRefunded()
		return payment, uc.transition(ctx, payment, domain.StatusSucceeded, domain.EventPaymentRefunded)
	default: // still awaiting payment: close the intent so it can never be charged
		from := payment.Status
		if err := uc.gateway.CancelIntent(ctx, payment.StripeIntentID); err != nil {
			return nil, domain.NewGatewayError("payment provider error: " + err.Error())
		}
		payment.MarkCanceled()
		return payment, uc.transition(ctx, payment, from, "")
	}
}

// applyProviderStatus moves the payment to what the provider reports. Only
// success and cancellation are terminal; anything else leaves it pending.
func (uc *UseCases) applyProviderStatus(ctx context.Context, p *domain.Payment, providerStatus string) error {
	target := domain.StatusFromProvider(providerStatus)
	if target == p.Status || target == domain.StatusPending {
		return nil
	}
	from := p.Status
	switch target {
	case domain.StatusSucceeded:
		p.MarkSucceeded()
		return uc.transition(ctx, p, from, domain.EventPaymentSucceeded)
	case domain.StatusCanceled:
		p.MarkCanceled()
		return uc.transition(ctx, p, from, "")
	}
	return nil
}

// transition persists a status change with its event (if any). When a
// concurrent writer got there first, the payment is reloaded so the caller
// returns the stored truth.
func (uc *UseCases) transition(ctx context.Context, p *domain.Payment, from domain.PaymentStatus, eventType string) error {
	var event *domain.Event
	if eventType != "" {
		event = domain.NewPaymentEvent(eventType, p)
	}
	applied, err := uc.payments.Transition(ctx, p, from, event)
	if err != nil {
		return err
	}
	if !applied {
		current, err := uc.payments.GetByOrderID(ctx, p.OrderID)
		if err != nil {
			return err
		}
		*p = *current
	}
	return nil
}

func isNotFound(err error) bool {
	var derr *domain.Error
	return errors.As(err, &derr) && derr.Code == domain.ErrCodeNotFound
}
