package application

import (
	"context"

	"goodfood/payment-service/internal/domain"
)

// HandleProviderEvent applies a verified provider notification. It is the
// source of truth in production: a customer who closes the tab right after
// paying still gets a confirmed order, since payment.succeeded is published
// from here. Events for unknown intents (another platform on the same account)
// and replays are ignored, so the provider's at-least-once delivery is safe.
func (uc *UseCases) HandleProviderEvent(ctx context.Context, ev domain.GatewayEvent) error {
	if ev.IntentID == "" {
		return nil
	}
	payment, err := uc.payments.GetByIntentID(ctx, ev.IntentID)
	if isNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	switch ev.Type {
	case domain.GatewayIntentSucceeded:
		return uc.applyProviderStatus(ctx, payment, "succeeded")
	case domain.GatewayIntentCanceled:
		return uc.applyProviderStatus(ctx, payment, "canceled")
	case domain.GatewayIntentFailed:
		if payment.Status != domain.StatusPending {
			return nil
		}
		payment.MarkFailed()
		return uc.transition(ctx, payment, domain.StatusPending, domain.EventPaymentFailed)
	case domain.GatewayChargeRefunded:
		// Also covers refunds made from the provider's dashboard.
		if payment.Status != domain.StatusSucceeded {
			return nil
		}
		payment.MarkRefunded()
		return uc.transition(ctx, payment, domain.StatusSucceeded, domain.EventPaymentRefunded)
	}
	return nil
}
