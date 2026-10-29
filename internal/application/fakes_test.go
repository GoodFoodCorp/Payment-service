package application

import (
	"context"
	"sort"
	"sync"

	"goodfood/payment-service/internal/domain"
)

// ── In-memory payment repository ────────────────────────────

type fakeRepo struct {
	mu      sync.Mutex
	byOrder map[string]*domain.Payment
	events  []*domain.Event
}

func newFakeRepo() *fakeRepo { return &fakeRepo{byOrder: map[string]*domain.Payment{}} }

func (f *fakeRepo) GetByOrderID(_ context.Context, orderID string) (*domain.Payment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.byOrder[orderID]
	if !ok {
		return nil, domain.NewNotFoundError("payment not found for this order")
	}
	cp := *p
	return &cp, nil
}

func (f *fakeRepo) GetByIntentID(_ context.Context, intentID string) (*domain.Payment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.byOrder {
		if p.StripeIntentID == intentID {
			cp := *p
			return &cp, nil
		}
	}
	return nil, domain.NewNotFoundError("payment not found")
}

func (f *fakeRepo) ListByCustomer(_ context.Context, customerID string, page domain.Page) ([]domain.Payment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []domain.Payment{}
	for _, p := range f.byOrder {
		if p.CustomerID == customerID {
			out = append(out, *p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].OrderID < out[j].OrderID })
	if page.Offset >= len(out) {
		return []domain.Payment{}, nil
	}
	out = out[page.Offset:]
	if len(out) > page.Limit {
		out = out[:page.Limit]
	}
	return out, nil
}

func (f *fakeRepo) Create(_ context.Context, p *domain.Payment) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.byOrder[p.OrderID]; exists {
		return domain.NewConflictError("a payment already exists for this order")
	}
	cp := *p
	f.byOrder[p.OrderID] = &cp
	return nil
}

func (f *fakeRepo) UpdateAmount(_ context.Context, p *domain.Payment) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byOrder[p.OrderID].AmountCents = p.AmountCents
	return nil
}

func (f *fakeRepo) Transition(_ context.Context, p *domain.Payment, from domain.PaymentStatus, event *domain.Event) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	stored := f.byOrder[p.OrderID]
	if stored == nil || stored.Status != from {
		return false, nil
	}
	cp := *p
	f.byOrder[p.OrderID] = &cp
	if event != nil {
		f.events = append(f.events, event)
	}
	return true, nil
}

func (f *fakeRepo) eventTypes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.events))
	for _, e := range f.events {
		out = append(out, e.Type)
	}
	return out
}

// ── In-memory saved cards and provider customers ────────────

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
	if m.IsDefault {
		for _, other := range f.items {
			if other.CustomerID == m.CustomerID {
				other.IsDefault = false
			}
		}
	}
	cp := *m
	f.items[m.ID] = &cp
	return nil
}

func (f *fakePaymentMethods) Delete(_ context.Context, id string) error {
	delete(f.items, id)
	return nil
}

type fakeCustomers struct{ ids map[string]string }

func (f *fakeCustomers) GetProviderCustomerID(_ context.Context, customerID string) (string, error) {
	return f.ids[customerID], nil
}

func (f *fakeCustomers) SaveProviderCustomerID(_ context.Context, customerID, providerID string) error {
	if _, ok := f.ids[customerID]; !ok {
		f.ids[customerID] = providerID
	}
	return nil
}

// ── Scriptable gateway ──────────────────────────────────────

type fakeGateway struct {
	status         string // reported by GetIntent; "" means requires_payment_method
	createCalls    int
	lastRequest    domain.IntentRequest
	updatedAmount  int64
	refunds        []string
	canceled       []string
	detached       []string
	customersMade  int
	cards          map[string]string // payment method id -> provider customer it is attached to
	failOperations bool
}

func newFakeGateway() *fakeGateway { return &fakeGateway{cards: map[string]string{}} }

func (g *fakeGateway) err() error {
	if g.failOperations {
		return errProvider
	}
	return nil
}

func (g *fakeGateway) CreateIntent(_ context.Context, req domain.IntentRequest, _ string) (*domain.PaymentIntent, error) {
	g.createCalls++
	g.lastRequest = req
	return &domain.PaymentIntent{ID: "pi_" + req.OrderID, ClientSecret: "secret_" + req.OrderID, Status: "requires_payment_method", AmountCents: req.AmountCents}, g.err()
}

func (g *fakeGateway) GetIntent(_ context.Context, id string) (*domain.PaymentIntent, error) {
	status := g.status
	if status == "" {
		status = "requires_payment_method"
	}
	return &domain.PaymentIntent{ID: id, ClientSecret: "secret_again", Status: status}, g.err()
}

func (g *fakeGateway) UpdateIntentAmount(_ context.Context, id string, amount int64) (*domain.PaymentIntent, error) {
	g.updatedAmount = amount
	return &domain.PaymentIntent{ID: id, ClientSecret: "secret_updated", Status: "requires_payment_method", AmountCents: amount}, g.err()
}

func (g *fakeGateway) CancelIntent(_ context.Context, id string) error {
	g.canceled = append(g.canceled, id)
	return g.err()
}

func (g *fakeGateway) Refund(_ context.Context, id, _ string) error {
	g.refunds = append(g.refunds, id)
	return g.err()
}

func (g *fakeGateway) CreateCustomer(_ context.Context, customerID string) (string, error) {
	g.customersMade++
	return "cus_" + customerID, g.err()
}

func (g *fakeGateway) CreateSetupIntent(_ context.Context, providerCustomerID string) (string, error) {
	return "seti_secret_" + providerCustomerID, g.err()
}

func (g *fakeGateway) GetCustomerCard(_ context.Context, providerCustomerID, pmID string) (*domain.CardDetails, error) {
	if g.cards[pmID] != providerCustomerID {
		return nil, errProvider
	}
	return &domain.CardDetails{ProviderID: pmID, HolderName: "Marie Dupont", Brand: "Visa", Last4: "4242", ExpMonth: 12, ExpYear: 2034}, nil
}

func (g *fakeGateway) DetachCard(_ context.Context, pmID string) error {
	g.detached = append(g.detached, pmID)
	return g.err()
}

type providerError struct{}

func (providerError) Error() string { return "provider unavailable" }

var errProvider = providerError{}
