package application

import (
	"context"

	"goodfood/payment-service/internal/domain"
)

// CreateCardSetup starts registering a card: the browser confirms the returned
// SetupIntent client secret with the provider's own form, so the card number
// goes straight to the provider.
func (uc *UseCases) CreateCardSetup(ctx context.Context, actor Actor) (string, error) {
	providerCustomer, err := uc.ensureProviderCustomer(ctx, actor.UserID)
	if err != nil {
		return "", err
	}
	secret, err := uc.gateway.CreateSetupIntent(ctx, providerCustomer)
	if err != nil {
		return "", domain.NewGatewayError("payment provider error: " + err.Error())
	}
	return secret, nil
}

// AddPaymentMethod records a card the customer registered with the provider.
// The provider is asked for the card, scoped to the caller's provider
// customer: a payment method id set up by someone else is refused.
func (uc *UseCases) AddPaymentMethod(ctx context.Context, actor Actor, in domain.PaymentMethodInput) (*domain.PaymentMethod, error) {
	if in.ProviderPaymentMethodID == "" {
		return nil, domain.NewValidationError("payment method id is required")
	}
	providerCustomer, err := uc.ensureProviderCustomer(ctx, actor.UserID)
	if err != nil {
		return nil, err
	}
	card, err := uc.gateway.GetCustomerCard(ctx, providerCustomer, in.ProviderPaymentMethodID)
	if err != nil {
		return nil, domain.NewForbiddenError("this card is not registered for your account")
	}
	method, err := domain.NewPaymentMethod(actor.UserID, card, in.IsDefault)
	if err != nil {
		return nil, err
	}
	if err := uc.paymentMethods.Create(ctx, method); err != nil {
		return nil, err
	}
	return method, nil
}

// ListMyPaymentMethods returns the caller's saved cards.
func (uc *UseCases) ListMyPaymentMethods(ctx context.Context, actor Actor) ([]domain.PaymentMethod, error) {
	return uc.paymentMethods.ListByCustomer(ctx, actor.UserID)
}

// DeletePaymentMethod removes one of the caller's cards, at the provider too.
func (uc *UseCases) DeletePaymentMethod(ctx context.Context, actor Actor, id string) error {
	method, err := uc.paymentMethods.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if !method.IsOwnedBy(actor.UserID) {
		return domain.NewForbiddenError("this payment method belongs to another user")
	}
	if method.ProviderPaymentMethodID != "" {
		if err := uc.gateway.DetachCard(ctx, method.ProviderPaymentMethodID); err != nil {
			return domain.NewGatewayError("payment provider error: " + err.Error())
		}
	}
	return uc.paymentMethods.Delete(ctx, id)
}

// ensureProviderCustomer returns the provider customer of a user, creating it
// on first use.
func (uc *UseCases) ensureProviderCustomer(ctx context.Context, customerID string) (string, error) {
	existing, err := uc.customers.GetProviderCustomerID(ctx, customerID)
	if err != nil {
		return "", err
	}
	if existing != "" {
		known, err := uc.gateway.HasCustomer(ctx, existing)
		if err != nil {
			return "", domain.NewGatewayError("payment provider error: " + err.Error())
		}
		if known {
			return existing, nil
		}
		return uc.replaceProviderCustomer(ctx, customerID, existing)
	}
	created, err := uc.gateway.CreateCustomer(ctx, customerID)
	if err != nil {
		return "", domain.NewGatewayError("payment provider error: " + err.Error())
	}
	if err := uc.customers.SaveProviderCustomerID(ctx, customerID, created); err != nil {
		return "", err
	}
	// SaveProviderCustomerID keeps the first mapping on a concurrent insert:
	// read it back so every caller uses the same provider customer.
	return uc.customers.GetProviderCustomerID(ctx, customerID)
}

// replaceProviderCustomer gives the customer a provider customer again when
// the stored one is unknown to the provider (created in demo mode, or with
// another provider account). The cards saved with it cannot be charged any
// more: they are removed, and the customer registers a card again.
func (uc *UseCases) replaceProviderCustomer(ctx context.Context, customerID, stale string) (string, error) {
	created, err := uc.gateway.CreateCustomer(ctx, customerID)
	if err != nil {
		return "", domain.NewGatewayError("payment provider error: " + err.Error())
	}
	if err := uc.customers.ReplaceProviderCustomerID(ctx, customerID, stale, created); err != nil {
		return "", err
	}
	if err := uc.paymentMethods.DeleteAllOf(ctx, customerID); err != nil {
		return "", err
	}
	return uc.customers.GetProviderCustomerID(ctx, customerID)
}
