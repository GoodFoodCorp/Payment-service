package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"goodfood/payment-service/internal/domain"
)

type PaymentRepository struct {
	pool *pgxpool.Pool
}

func NewPaymentRepository(pool *pgxpool.Pool) *PaymentRepository {
	return &PaymentRepository{pool: pool}
}

const cols = `id, order_id, customer_id, stripe_intent_id, status, amount_cents, currency, paid_at, created_at`

func (r *PaymentRepository) GetByOrderID(ctx context.Context, orderID string) (*domain.Payment, error) {
	var p domain.Payment
	err := r.pool.QueryRow(ctx, `SELECT `+cols+` FROM payments WHERE order_id = $1`, orderID).
		Scan(&p.ID, &p.OrderID, &p.CustomerID, &p.StripeIntentID, &p.Status,
			&p.AmountCents, &p.Currency, &p.PaidAt, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.NewNotFoundError("payment not found for this order")
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *PaymentRepository) Create(ctx context.Context, p *domain.Payment) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO payments (`+cols+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		p.ID, p.OrderID, p.CustomerID, p.StripeIntentID, p.Status,
		p.AmountCents, p.Currency, p.PaidAt, p.CreatedAt)
	return err
}

func (r *PaymentRepository) Update(ctx context.Context, p *domain.Payment) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE payments SET status = $1, paid_at = $2 WHERE id = $3`, p.Status, p.PaidAt, p.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.NewNotFoundError("payment not found")
	}
	return nil
}
