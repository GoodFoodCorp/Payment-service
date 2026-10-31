package application

import (
	"context"

	"goodfood/payment-service/internal/domain"
)

// GetPayment returns the payment of an order, to the customer who made it
// only (head office sees no customer's order).
func (uc *UseCases) GetPayment(ctx context.Context, actor Actor, orderID string) (*domain.Payment, error) {
	payment, err := uc.payments.GetByOrderID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if !payment.IsOwnedBy(actor.UserID) {
		return nil, domain.NewForbiddenError("you are not allowed to view this payment")
	}
	return payment, nil
}

// ListMyPayments returns a page of the caller's own payment history.
func (uc *UseCases) ListMyPayments(ctx context.Context, actor Actor, page domain.Page) ([]domain.Payment, error) {
	return uc.payments.ListByCustomer(ctx, actor.UserID, page)
}
