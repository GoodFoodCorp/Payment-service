package application

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"goodfood/payment-service/internal/domain"
)

var (
	customer = Actor{UserID: "cust-1", RoleSlugs: []string{"user"}}
	other    = Actor{UserID: "cust-2", RoleSlugs: []string{"user"}}
	ctx      = context.Background()
)

type fixture struct {
	uc        *UseCases
	repo      *fakeRepo
	gw        *fakeGateway
	methods   *fakePaymentMethods
	customers *fakeCustomers
}

func setup() fixture {
	f := fixture{repo: newFakeRepo(), gw: newFakeGateway(), methods: newFakePaymentMethods(),
		customers: &fakeCustomers{ids: map[string]string{}}}
	f.uc = NewUseCases(f.repo, f.gw, f.methods, f.customers)
	return f
}

func intentFor(orderID string, amount int64) OrderIntentInput {
	return OrderIntentInput{OrderID: orderID, CustomerID: customer.UserID, AmountCents: amount}
}

func requireCode(t *testing.T, err error, code domain.ErrorCode) {
	t.Helper()
	var derr *domain.Error
	require.ErrorAs(t, err, &derr)
	assert.Equal(t, code, derr.Code)
}

// ── Intents (order-service) ─────────────────────────────────

func TestCreateIntentForOrder_ChargesTheOrderAmount(t *testing.T) {
	f := setup()

	out, err := f.uc.CreateIntentForOrder(ctx, intentFor("order-1", 2598))

	require.NoError(t, err)
	assert.Equal(t, int64(2598), out.Payment.AmountCents)
	assert.Equal(t, int64(2598), f.gw.lastRequest.AmountCents)
	assert.Equal(t, "eur", out.Payment.Currency, "currency defaults to eur")
	assert.Equal(t, "secret_order-1", out.ClientSecret)
	assert.Equal(t, domain.StatusPending, out.Payment.Status)
}

func TestCreateIntentForOrder_ResumesPendingIntentWithItsSecret(t *testing.T) {
	f := setup()
	_, err := f.uc.CreateIntentForOrder(ctx, intentFor("order-1", 1000))
	require.NoError(t, err)

	again, err := f.uc.CreateIntentForOrder(ctx, intentFor("order-1", 1000))

	require.NoError(t, err)
	assert.Equal(t, 1, f.gw.createCalls, "the provider intent is reused")
	assert.NotEmpty(t, again.ClientSecret, "a retry still gets a usable client secret")
}

func TestCreateIntentForOrder_RealignsAmountOfPendingIntent(t *testing.T) {
	f := setup()
	_, _ = f.uc.CreateIntentForOrder(ctx, intentFor("order-1", 1000))

	out, err := f.uc.CreateIntentForOrder(ctx, intentFor("order-1", 1500))

	require.NoError(t, err)
	assert.Equal(t, int64(1500), f.gw.updatedAmount)
	assert.Equal(t, int64(1500), out.Payment.AmountCents)
	stored, _ := f.repo.GetByOrderID(ctx, "order-1")
	assert.Equal(t, int64(1500), stored.AmountCents)
}

func TestCreateIntentForOrder_Validation(t *testing.T) {
	f := setup()
	_, err := f.uc.CreateIntentForOrder(ctx, intentFor("order-1", 0))
	requireCode(t, err, domain.ErrCodeValidation)

	_, err = f.uc.CreateIntentForOrder(ctx, OrderIntentInput{OrderID: "order-1", AmountCents: 100})
	requireCode(t, err, domain.ErrCodeValidation)
}

func TestCreateIntentForOrder_RefusedOncePaid(t *testing.T) {
	f := setup()
	_, _ = f.uc.CreateIntentForOrder(ctx, intentFor("order-1", 1000))
	f.gw.status = "succeeded"
	_, err := f.uc.SyncPayment(ctx, "order-1")
	require.NoError(t, err)

	_, err = f.uc.CreateIntentForOrder(ctx, intentFor("order-1", 1000))
	requireCode(t, err, domain.ErrCodeConflict)
}

func TestCreateIntentForOrder_RefusesAnotherCustomersOrder(t *testing.T) {
	f := setup()
	_, _ = f.uc.CreateIntentForOrder(ctx, intentFor("order-1", 1000))

	_, err := f.uc.CreateIntentForOrder(ctx, OrderIntentInput{OrderID: "order-1", CustomerID: other.UserID, AmountCents: 1})
	requireCode(t, err, domain.ErrCodeForbidden)
}

func TestCreateIntentForOrder_WithSavedCard(t *testing.T) {
	f := setup()
	f.gw.cards["pm_1"] = "cus_" + customer.UserID
	method, err := f.uc.AddPaymentMethod(ctx, customer, domain.PaymentMethodInput{ProviderPaymentMethodID: "pm_1"})
	require.NoError(t, err)

	in := intentFor("order-1", 1000)
	in.PaymentMethodID = method.ID
	_, err = f.uc.CreateIntentForOrder(ctx, in)

	require.NoError(t, err)
	assert.Equal(t, "pm_1", f.gw.lastRequest.ProviderPaymentMethodID)
	assert.Equal(t, "cus_"+customer.UserID, f.gw.lastRequest.ProviderCustomerID)
}

func TestCreateIntentForOrder_RefusesSomeoneElsesCard(t *testing.T) {
	f := setup()
	f.gw.cards["pm_1"] = "cus_" + other.UserID
	method, err := f.uc.AddPaymentMethod(ctx, other, domain.PaymentMethodInput{ProviderPaymentMethodID: "pm_1"})
	require.NoError(t, err)

	in := intentFor("order-1", 1000)
	in.PaymentMethodID = method.ID
	_, err = f.uc.CreateIntentForOrder(ctx, in)
	requireCode(t, err, domain.ErrCodeForbidden)
}

// ── Sync and refunds ────────────────────────────────────────

func TestSyncPayment_PublishesSucceededOnce(t *testing.T) {
	f := setup()
	_, _ = f.uc.CreateIntentForOrder(ctx, intentFor("order-1", 1000))

	pending, err := f.uc.SyncPayment(ctx, "order-1")
	require.NoError(t, err)
	assert.Equal(t, domain.StatusPending, pending.Status)

	f.gw.status = "succeeded"
	paid, err := f.uc.SyncPayment(ctx, "order-1")
	require.NoError(t, err)
	assert.Equal(t, domain.StatusSucceeded, paid.Status)
	require.NotNil(t, paid.PaidAt)

	_, err = f.uc.SyncPayment(ctx, "order-1")
	require.NoError(t, err)
	assert.Equal(t, []string{domain.EventPaymentSucceeded}, f.repo.eventTypes())
}

func TestRefundOrder_RefundsPaidOrderOnce(t *testing.T) {
	f := setup()
	_, _ = f.uc.CreateIntentForOrder(ctx, intentFor("order-1", 1000))
	f.gw.status = "succeeded"

	refunded, err := f.uc.RefundOrder(ctx, "order-1")
	require.NoError(t, err)
	assert.Equal(t, domain.StatusRefunded, refunded.Status)

	_, err = f.uc.RefundOrder(ctx, "order-1")
	require.NoError(t, err)
	assert.Equal(t, []string{"pi_order-1"}, f.gw.refunds, "refunded exactly once")
	assert.Equal(t, []string{domain.EventPaymentSucceeded, domain.EventPaymentRefunded}, f.repo.eventTypes())
}

func TestRefundOrder_CancelsUnpaidIntent(t *testing.T) {
	f := setup()
	_, _ = f.uc.CreateIntentForOrder(ctx, intentFor("order-1", 1000))

	payment, err := f.uc.RefundOrder(ctx, "order-1")

	require.NoError(t, err)
	assert.Equal(t, domain.StatusCanceled, payment.Status)
	assert.Equal(t, []string{"pi_order-1"}, f.gw.canceled)
	assert.Empty(t, f.gw.refunds)
}

func TestRefundOrder_ProviderFailureKeepsPaymentPaid(t *testing.T) {
	f := setup()
	_, _ = f.uc.CreateIntentForOrder(ctx, intentFor("order-1", 1000))
	f.gw.status = "succeeded"
	_, _ = f.uc.SyncPayment(ctx, "order-1")
	f.gw.failOperations = true

	_, err := f.uc.RefundOrder(ctx, "order-1")

	requireCode(t, err, domain.ErrCodeGateway)
	stored, _ := f.repo.GetByOrderID(ctx, "order-1")
	assert.Equal(t, domain.StatusSucceeded, stored.Status, "order-service retries the refund later")
}

func TestRefundOrder_UnknownOrder(t *testing.T) {
	_, err := setup().uc.RefundOrder(ctx, "never-paid")
	requireCode(t, err, domain.ErrCodeNotFound)
}

// ── Webhooks ────────────────────────────────────────────────

func TestHandleProviderEvent(t *testing.T) {
	f := setup()
	_, _ = f.uc.CreateIntentForOrder(ctx, intentFor("order-1", 1000))

	require.NoError(t, f.uc.HandleProviderEvent(ctx, domain.GatewayEvent{Type: domain.GatewayIntentFailed, IntentID: "pi_order-1"}))
	failed, _ := f.repo.GetByOrderID(ctx, "order-1")
	assert.Equal(t, domain.StatusFailed, failed.Status)

	// A declined card can be retried: the intent is resumed, not refused.
	_, err := f.uc.CreateIntentForOrder(ctx, intentFor("order-1", 1000))
	require.NoError(t, err)

	succeeded := domain.GatewayEvent{Type: domain.GatewayIntentSucceeded, IntentID: "pi_order-1"}
	require.NoError(t, f.uc.HandleProviderEvent(ctx, succeeded))
	require.NoError(t, f.uc.HandleProviderEvent(ctx, succeeded), "replays are harmless")

	require.NoError(t, f.uc.HandleProviderEvent(ctx, domain.GatewayEvent{Type: domain.GatewayChargeRefunded, IntentID: "pi_order-1"}))
	refunded, _ := f.repo.GetByOrderID(ctx, "order-1")
	assert.Equal(t, domain.StatusRefunded, refunded.Status)

	assert.Equal(t, []string{domain.EventPaymentFailed, domain.EventPaymentSucceeded, domain.EventPaymentRefunded}, f.repo.eventTypes())
}

func TestHandleProviderEvent_IgnoresUnknownIntent(t *testing.T) {
	err := setup().uc.HandleProviderEvent(ctx, domain.GatewayEvent{Type: domain.GatewayIntentSucceeded, IntentID: "pi_other_platform"})
	assert.NoError(t, err)
}

// ── Customer reads ──────────────────────────────────────────

func TestOnlyOwnerOrAdminCanView(t *testing.T) {
	f := setup()
	_, _ = f.uc.CreateIntentForOrder(ctx, intentFor("order-1", 1000))

	_, err := f.uc.GetPayment(ctx, other, "order-1")
	requireCode(t, err, domain.ErrCodeForbidden)

	_, err = f.uc.GetPayment(ctx, customer, "order-1")
	require.NoError(t, err)
	_, err = f.uc.GetPayment(ctx, Actor{UserID: "adm", RoleSlugs: []string{RoleAdmin}}, "order-1")
	require.NoError(t, err)
}

func TestListMyPayments_IsPaginated(t *testing.T) {
	f := setup()
	for _, id := range []string{"order-1", "order-2", "order-3"} {
		_, _ = f.uc.CreateIntentForOrder(ctx, intentFor(id, 1000))
	}

	page, err := f.uc.ListMyPayments(ctx, customer, NormalizePage(2, 1))

	require.NoError(t, err)
	require.Len(t, page, 2)
	assert.Equal(t, "order-2", page[0].OrderID)
}

func TestNormalizePage(t *testing.T) {
	assert.Equal(t, domain.Page{Limit: DefaultPageSize}, NormalizePage(0, -3))
	assert.Equal(t, domain.Page{Limit: MaxPageSize, Offset: 10}, NormalizePage(10_000, 10))
}

// ── Saved cards ─────────────────────────────────────────────

func TestCreateCardSetup_CreatesProviderCustomerOnce(t *testing.T) {
	f := setup()
	secret, err := f.uc.CreateCardSetup(ctx, customer)
	require.NoError(t, err)
	assert.Equal(t, "seti_secret_cus_cust-1", secret)

	_, err = f.uc.CreateCardSetup(ctx, customer)
	require.NoError(t, err)
	assert.Equal(t, 1, f.gw.customersMade)
}

func TestCreateCardSetup_ReplacesACustomerTheProviderDoesNotKnow(t *testing.T) {
	f := setup()
	// Mapped in demo mode (or with another provider account), with a card.
	f.customers.ids[customer.UserID] = "legacy_" + customer.UserID
	require.NoError(t, f.methods.Create(ctx, &domain.PaymentMethod{ID: "old-card", CustomerID: customer.UserID}))

	secret, err := f.uc.CreateCardSetup(ctx, customer)

	require.NoError(t, err)
	assert.Equal(t, "seti_secret_cus_cust-1", secret)
	assert.Equal(t, "cus_"+customer.UserID, f.customers.ids[customer.UserID])
	cards, _ := f.uc.ListMyPaymentMethods(ctx, customer)
	assert.Empty(t, cards, "cards of the unknown customer cannot be charged any more")
}

func TestAddPaymentMethod_OnlyCardsOfTheCallersAccount(t *testing.T) {
	f := setup()
	f.gw.cards["pm_mine"] = "cus_" + customer.UserID
	f.gw.cards["pm_theirs"] = "cus_" + other.UserID

	method, err := f.uc.AddPaymentMethod(ctx, customer, domain.PaymentMethodInput{ProviderPaymentMethodID: "pm_mine", IsDefault: true})
	require.NoError(t, err)
	assert.Equal(t, "Visa", method.Brand)
	assert.Equal(t, "4242", method.Last4)

	_, err = f.uc.AddPaymentMethod(ctx, customer, domain.PaymentMethodInput{ProviderPaymentMethodID: "pm_theirs"})
	requireCode(t, err, domain.ErrCodeForbidden)

	_, err = f.uc.AddPaymentMethod(ctx, customer, domain.PaymentMethodInput{})
	requireCode(t, err, domain.ErrCodeValidation)
}

func TestOnlyOneDefaultPaymentMethod(t *testing.T) {
	f := setup()
	f.gw.cards["pm_1"] = "cus_" + customer.UserID
	f.gw.cards["pm_2"] = "cus_" + customer.UserID
	_, err := f.uc.AddPaymentMethod(ctx, customer, domain.PaymentMethodInput{ProviderPaymentMethodID: "pm_1", IsDefault: true})
	require.NoError(t, err)
	_, err = f.uc.AddPaymentMethod(ctx, customer, domain.PaymentMethodInput{ProviderPaymentMethodID: "pm_2", IsDefault: true})
	require.NoError(t, err)

	list, _ := f.uc.ListMyPaymentMethods(ctx, customer)
	defaults := 0
	for _, m := range list {
		if m.IsDefault {
			defaults++
		}
	}
	assert.Equal(t, 1, defaults)
}

func TestDeletePaymentMethod_OwnershipAndProviderDetach(t *testing.T) {
	f := setup()
	f.gw.cards["pm_1"] = "cus_" + customer.UserID
	method, err := f.uc.AddPaymentMethod(ctx, customer, domain.PaymentMethodInput{ProviderPaymentMethodID: "pm_1"})
	require.NoError(t, err)

	requireCode(t, f.uc.DeletePaymentMethod(ctx, other, method.ID), domain.ErrCodeForbidden)

	require.NoError(t, f.uc.DeletePaymentMethod(ctx, customer, method.ID))
	assert.Equal(t, []string{"pm_1"}, f.gw.detached)
}
