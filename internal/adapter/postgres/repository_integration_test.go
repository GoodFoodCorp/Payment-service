//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/uuid"

	"goodfood/payment-service/internal/adapter/outbox"
	"goodfood/payment-service/internal/domain"
)

// Run with: TEST_DATABASE_URL=postgres://... go test -tags integration ./internal/adapter/postgres/
func setupDB(t *testing.T) *PaymentRepository {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	if err := Migrate(url); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := Connect(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return NewPaymentRepository(pool)
}

func TestPaymentLifecycleWithOutbox(t *testing.T) {
	ctx := context.Background()
	repo := setupDB(t)
	orderID := uuid.NewString()
	p, _ := domain.NewPayment(orderID, uuid.NewString(), "pi_"+orderID, 1234, "eur")
	if err := repo.Create(ctx, p); err != nil {
		t.Fatal(err)
	}

	dup, _ := domain.NewPayment(orderID, p.CustomerID, "pi_dup_"+orderID, 1234, "eur")
	if err := repo.Create(ctx, dup); err == nil {
		t.Fatal("a second payment for the same order must be refused")
	}

	p.MarkSucceeded()
	applied, err := repo.Transition(ctx, p, domain.StatusPending, domain.NewPaymentEvent(domain.EventPaymentSucceeded, p))
	if err != nil || !applied {
		t.Fatalf("transition: %v %v", applied, err)
	}
	// A stale writer (still believing PENDING) loses and writes no event.
	applied, err = repo.Transition(ctx, p, domain.StatusPending, domain.NewPaymentEvent(domain.EventPaymentSucceeded, p))
	if err != nil || applied {
		t.Fatalf("stale transition must not apply: %v %v", applied, err)
	}

	byIntent, err := repo.GetByIntentID(ctx, "pi_"+orderID)
	if err != nil || byIntent.Status != domain.StatusSucceeded || byIntent.PaidAt == nil {
		t.Fatalf("got %+v %v", byIntent, err)
	}

	store := NewOutboxRepository(repo.pool)
	var relayed []outbox.Message
	collect := func(_ context.Context, m outbox.Message) error {
		relayed = append(relayed, m)
		return nil
	}
	for {
		n, err := store.Relay(ctx, 100, collect)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
	}
	found := 0
	for _, m := range relayed {
		var env struct {
			Type string                  `json:"type"`
			Data domain.PaymentEventData `json:"data"`
		}
		if err := json.Unmarshal(m.Body, &env); err != nil {
			t.Fatal(err)
		}
		if env.Data.OrderID == orderID {
			found++
			if env.Type != domain.EventPaymentSucceeded || env.Data.AmountCents != 1234 {
				t.Fatalf("unexpected event %+v", env)
			}
		}
	}
	if found != 1 {
		t.Fatalf("expected exactly one event for the order, got %d", found)
	}

	n, err := store.Relay(ctx, 100, collect)
	if err != nil || n != 0 {
		t.Fatalf("published events must not be relayed again: %d %v", n, err)
	}
}

func TestListByCustomerIsPaginated(t *testing.T) {
	ctx := context.Background()
	repo := setupDB(t)
	customerID := uuid.NewString()
	for i := 0; i < 3; i++ {
		orderID := uuid.NewString()
		p, _ := domain.NewPayment(orderID, customerID, "pi_"+orderID, 100, "eur")
		if err := repo.Create(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	page, err := repo.ListByCustomer(ctx, customerID, domain.Page{Limit: 2, Offset: 0})
	if err != nil || len(page) != 2 {
		t.Fatalf("got %d %v", len(page), err)
	}
	rest, _ := repo.ListByCustomer(ctx, customerID, domain.Page{Limit: 2, Offset: 2})
	if len(rest) != 1 {
		t.Fatalf("got %d", len(rest))
	}
}
