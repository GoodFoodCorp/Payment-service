package application

import (
	"context"

	"goodfood/payment-service/internal/domain"
)

// Actor is the authenticated caller. order-service forwards the customer's own
// JWT, so payments stay attributable to the customer who pays.
type Actor struct {
	UserID    string
	RoleSlugs []string
}

func (a Actor) HasRole(slug string) bool {
	for _, r := range a.RoleSlugs {
		if r == slug {
			return true
		}
	}
	return false
}

const RoleAdmin = "admin"

type UseCases struct {
	payments       domain.PaymentRepository
	gateway        domain.PaymentGateway
	paymentMethods domain.PaymentMethodRepository
}

func NewUseCases(payments domain.PaymentRepository, gateway domain.PaymentGateway, paymentMethods domain.PaymentMethodRepository) *UseCases {
	return &UseCases{payments: payments, gateway: gateway, paymentMethods: paymentMethods}
}

type CreateIntentInput struct {
	OrderID     string
	AmountCents int64
	Currency    string
}

// CreateIntent asks the provider for a payment intent covering the order and
// records it. Idempotent: an existing pending intent is reused.
func (uc *UseCases) CreateIntent(ctx context.Context, actor Actor, in CreateIntentInput) (*domain.Payment, string, error) {
	if existing, err := uc.payments.GetByOrderID(ctx, in.OrderID); err == nil && existing != nil {
		if existing.Status == domain.StatusPending {
			return existing, "", nil
		}
		return nil, "", domain.NewConflictError("this order has already been paid")
	}

	currency := in.Currency
	if currency == "" {
		currency = "eur"
	}

	intent, err := uc.gateway.CreateIntent(ctx, in.AmountCents, currency, in.OrderID)
	if err != nil {
		return nil, "", domain.NewGatewayError("payment provider error: " + err.Error())
	}

	payment, err := domain.NewPayment(in.OrderID, actor.UserID, intent.ID, in.AmountCents, currency)
	if err != nil {
		return nil, "", err
	}
	if err := uc.payments.Create(ctx, payment); err != nil {
		return nil, "", err
	}
	return payment, intent.ClientSecret, nil
}

// ConfirmPayment verifies with the provider that the intent succeeded and marks
// the payment as paid. Idempotent.
func (uc *UseCases) ConfirmPayment(ctx context.Context, actor Actor, orderID string) (*domain.Payment, error) {
	payment, err := uc.payments.GetByOrderID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if !payment.IsOwnedBy(actor.UserID) && !actor.HasRole(RoleAdmin) {
		return nil, domain.NewForbiddenError("you can only confirm your own payment")
	}
	if payment.Status == domain.StatusSucceeded {
		return payment, nil
	}

	status, err := uc.gateway.GetIntentStatus(ctx, payment.StripeIntentID)
	if err != nil {
		return nil, domain.NewGatewayError("payment provider error: " + err.Error())
	}
	if status != "succeeded" {
		return nil, domain.NewGatewayError("payment not completed (status: " + status + ")")
	}

	payment.MarkSucceeded()
	if err := uc.payments.Update(ctx, payment); err != nil {
		return nil, err
	}
	return payment, nil
}

// GetPayment returns the payment of an order, for its owner or head office.
func (uc *UseCases) GetPayment(ctx context.Context, actor Actor, orderID string) (*domain.Payment, error) {
	payment, err := uc.payments.GetByOrderID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if !payment.IsOwnedBy(actor.UserID) && !actor.HasRole(RoleAdmin) {
		return nil, domain.NewForbiddenError("you are not allowed to view this payment")
	}
	return payment, nil
}

// ListMyPayments returns the caller's own payment history.
func (uc *UseCases) ListMyPayments(ctx context.Context, actor Actor) ([]domain.Payment, error) {
	return uc.payments.ListByCustomer(ctx, actor.UserID)
}

// ListMyPaymentMethods returns the caller's saved cards.
func (uc *UseCases) ListMyPaymentMethods(ctx context.Context, actor Actor) ([]domain.PaymentMethod, error) {
	return uc.paymentMethods.ListByCustomer(ctx, actor.UserID)
}

// AddPaymentMethod saves a new card for the caller (demo mode — see
// domain.NewPaymentMethod for what is and isn't stored).
func (uc *UseCases) AddPaymentMethod(ctx context.Context, actor Actor, in domain.PaymentMethodInput) (*domain.PaymentMethod, error) {
	method, err := domain.NewPaymentMethod(actor.UserID, in)
	if err != nil {
		return nil, err
	}
	if method.IsDefault {
		if err := uc.paymentMethods.ClearDefault(ctx, actor.UserID); err != nil {
			return nil, err
		}
	}
	if err := uc.paymentMethods.Create(ctx, method); err != nil {
		return nil, err
	}
	return method, nil
}

// DeletePaymentMethod removes one of the caller's own saved cards.
func (uc *UseCases) DeletePaymentMethod(ctx context.Context, actor Actor, id string) error {
	method, err := uc.paymentMethods.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if !method.IsOwnedBy(actor.UserID) {
		return domain.NewForbiddenError("this payment method belongs to another user")
	}
	return uc.paymentMethods.Delete(ctx, id)
}
