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
	return NewUseCases(repo, gw), repo, gw
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
