package application

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"goodfood/payment-service/internal/domain"
)

type fakeRepo struct{ byOrder map[string]*domain.Payment }

func newFakeRepo() *fakeRepo { return &fakeRepo{byOrder: map[string]*domain.Payment{}} }

func (f *fakeRepo) GetByOrderID(_ context.Context, orderID string) (*domain.Payment, error) {
	p, ok := f.byOrder[orderID]
	if !ok {
		return nil, domain.NewNotFoundError("payment not found for this order")
	}
	cp := *p
	return &cp, nil
}

func (f *fakeRepo) Create(_ context.Context, p *domain.Payment) error {
	cp := *p
	f.byOrder[p.OrderID] = &cp
	return nil
}

func (f *fakeRepo) Update(_ context.Context, p *domain.Payment) error {
	f.byOrder[p.OrderID] = p
	return nil
}

func (f *fakeRepo) ListByCustomer(_ context.Context, customerID string) ([]domain.Payment, error) {
	out := []domain.Payment{}
	for _, p := range f.byOrder {
		if p.CustomerID == customerID {
			out = append(out, *p)
		}
	}
	return out, nil
}

type fakePaymentMethods struct {
	items map[string]*domain.PaymentMethod
}

func newFakePaymentMethods() *fakePaymentMethods {
	return &fakePaymentMethods{items: map[string]*domain.PaymentMethod{}}
}

func (f *fakePaymentMethods) ListByCustomer(_ context.Context, customerID string) ([]domain.PaymentMethod, error) {
	out := []domain.PaymentMethod{}
	for _, m := range f.items {
		if m.CustomerID == customerID {
			out = append(out, *m)
		}
	}
	return out, nil
}

func (f *fakePaymentMethods) GetByID(_ context.Context, id string) (*domain.PaymentMethod, error) {
	m, ok := f.items[id]
	if !ok {
		return nil, domain.NewNotFoundError("payment method not found")
	}
	cp := *m
	return &cp, nil
}

func (f *fakePaymentMethods) Create(_ context.Context, m *domain.PaymentMethod) error {
	cp := *m
	f.items[m.ID] = &cp
	return nil
}

func (f *fakePaymentMethods) Delete(_ context.Context, id string) error {
	delete(f.items, id)
	return nil
}

func (f *fakePaymentMethods) ClearDefault(_ context.Context, customerID string) error {
	for _, m := range f.items {
		if m.CustomerID == customerID {
			m.IsDefault = false
		}
	}
	return nil
}

type fakeGateway struct {
	status      string
	createCalls int
}

func (f *fakeGateway) CreateIntent(_ context.Context, _ int64, _, orderID string) (*domain.PaymentIntent, error) {
	f.createCalls++
	return &domain.PaymentIntent{ID: "pi_" + orderID, ClientSecret: "secret"}, nil
}

func (f *fakeGateway) GetIntentStatus(_ context.Context, _ string) (string, error) {
	if f.status == "" {
		return "succeeded", nil
	}
	return f.status, nil
}

var (
	customer = Actor{UserID: "cust-1", RoleSlugs: []string{"user"}}
	other    = Actor{UserID: "cust-2", RoleSlugs: []string{"user"}}
)

func setup() (*UseCases, *fakeRepo, *fakeGateway) {
	repo := newFakeRepo()
	gw := &fakeGateway{}
	return NewUseCases(repo, gw, newFakePaymentMethods()), repo, gw
}

func TestCreateIntentIsIdempotent(t *testing.T) {
	uc, _, gw := setup()
	in := CreateIntentInput{OrderID: "order-1", AmountCents: 2598}

	first, secret, err := uc.CreateIntent(context.Background(), customer, in)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusPending, first.Status)
	assert.Equal(t, "secret", secret)
	assert.Equal(t, int64(2598), first.AmountCents)
	assert.Equal(t, "eur", first.Currency, "currency defaults to eur")

	second, _, err := uc.CreateIntent(context.Background(), customer, in)
	require.NoError(t, err)
	assert.Equal(t, first.ID, second.ID)
	assert.Equal(t, 1, gw.createCalls, "the provider is called only once")
}

func TestCreateIntentRejectsInvalidAmount(t *testing.T) {
	uc, _, _ := setup()
	_, _, err := uc.CreateIntent(context.Background(), customer,
		CreateIntentInput{OrderID: "order-1", AmountCents: 0})
	var derr *domain.Error
	require.ErrorAs(t, err, &derr)
	assert.Equal(t, domain.ErrCodeValidation, derr.Code)
}

func TestConfirmPaymentSuccess(t *testing.T) {
	uc, _, _ := setup()
	_, _, err := uc.CreateIntent(context.Background(), customer, CreateIntentInput{OrderID: "order-1", AmountCents: 1000})
	require.NoError(t, err)

	paid, err := uc.ConfirmPayment(context.Background(), customer, "order-1")
	require.NoError(t, err)
	assert.Equal(t, domain.StatusSucceeded, paid.Status)
	require.NotNil(t, paid.PaidAt)

	// Idempotent
	again, err := uc.ConfirmPayment(context.Background(), customer, "order-1")
	require.NoError(t, err)
	assert.Equal(t, domain.StatusSucceeded, again.Status)
}

func TestConfirmPaymentNotCompleted(t *testing.T) {
	uc, _, gw := setup()
	_, _, _ = uc.CreateIntent(context.Background(), customer, CreateIntentInput{OrderID: "order-1", AmountCents: 1000})
	gw.status = "requires_payment_method"

	_, err := uc.ConfirmPayment(context.Background(), customer, "order-1")
	var derr *domain.Error
	require.ErrorAs(t, err, &derr)
	assert.Equal(t, domain.ErrCodeGateway, derr.Code)
}

func TestOnlyOwnerCanConfirmOrView(t *testing.T) {
	uc, _, _ := setup()
	_, _, _ = uc.CreateIntent(context.Background(), customer, CreateIntentInput{OrderID: "order-1", AmountCents: 1000})

	_, err := uc.ConfirmPayment(context.Background(), other, "order-1")
	var derr *domain.Error
	require.ErrorAs(t, err, &derr)
	assert.Equal(t, domain.ErrCodeForbidden, derr.Code)

	_, err = uc.GetPayment(context.Background(), other, "order-1")
	require.ErrorAs(t, err, &derr)
	assert.Equal(t, domain.ErrCodeForbidden, derr.Code)

	// Head office may inspect any payment.
	admin := Actor{UserID: "adm", RoleSlugs: []string{RoleAdmin}}
	_, err = uc.GetPayment(context.Background(), admin, "order-1")
	require.NoError(t, err)
}

func TestCreateIntentRefusedOnAlreadyPaidOrder(t *testing.T) {
	uc, _, _ := setup()
	in := CreateIntentInput{OrderID: "order-1", AmountCents: 1000}
	_, _, _ = uc.CreateIntent(context.Background(), customer, in)
	_, err := uc.ConfirmPayment(context.Background(), customer, "order-1")
	require.NoError(t, err)

	_, _, err = uc.CreateIntent(context.Background(), customer, in)
	var derr *domain.Error
	require.ErrorAs(t, err, &derr)
	assert.Equal(t, domain.ErrCodeConflict, derr.Code)
}

func TestAddPaymentMethodDerivesBrandAndLast4(t *testing.T) {
	uc, _, _ := setup()
	method, err := uc.AddPaymentMethod(context.Background(), customer, domain.PaymentMethodInput{
		CardholderName: "Marie Dupont", CardNumber: "4242 4242 4242 4242", ExpMonth: 12, ExpYear: 2030, IsDefault: true,
	})
	require.NoError(t, err)
	assert.Equal(t, "Visa", method.Brand)
	assert.Equal(t, "4242", method.Last4)

	list, err := uc.ListMyPaymentMethods(context.Background(), customer)
	require.NoError(t, err)
	assert.Len(t, list, 1)
}

func TestOnlyOneDefaultPaymentMethod(t *testing.T) {
	uc, _, _ := setup()
	_, err := uc.AddPaymentMethod(context.Background(), customer, domain.PaymentMethodInput{
		CardholderName: "Marie Dupont", CardNumber: "4242424242424242", ExpMonth: 12, ExpYear: 2030, IsDefault: true,
	})
	require.NoError(t, err)
	_, err = uc.AddPaymentMethod(context.Background(), customer, domain.PaymentMethodInput{
		CardholderName: "Marie Dupont", CardNumber: "5555555555554444", ExpMonth: 6, ExpYear: 2031, IsDefault: true,
	})
	require.NoError(t, err)

	list, _ := uc.ListMyPaymentMethods(context.Background(), customer)
	defaults := 0
	for _, m := range list {
		if m.IsDefault {
			defaults++
		}
	}
	assert.Equal(t, 1, defaults, "adding a new default clears the previous one")
}

func TestDeletePaymentMethodOwnershipCheck(t *testing.T) {
	uc, _, _ := setup()
	method, err := uc.AddPaymentMethod(context.Background(), customer, domain.PaymentMethodInput{
		CardholderName: "Marie Dupont", CardNumber: "4242424242424242", ExpMonth: 12, ExpYear: 2030,
	})
	require.NoError(t, err)

	err = uc.DeletePaymentMethod(context.Background(), other, method.ID)
	var derr *domain.Error
	require.ErrorAs(t, err, &derr)
	assert.Equal(t, domain.ErrCodeForbidden, derr.Code)

	require.NoError(t, uc.DeletePaymentMethod(context.Background(), customer, method.ID))
}

func TestAddPaymentMethodRejectsExpiredCard(t *testing.T) {
	uc, _, _ := setup()
	_, err := uc.AddPaymentMethod(context.Background(), customer, domain.PaymentMethodInput{
		CardholderName: "Marie Dupont", CardNumber: "4242424242424242", ExpMonth: 1, ExpYear: 2000,
	})
	var derr *domain.Error
	require.ErrorAs(t, err, &derr)
	assert.Equal(t, domain.ErrCodeValidation, derr.Code)
}
