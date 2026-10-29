package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"goodfood/payment-service/internal/domain"
)

// uniqueViolation is PostgreSQL's SQLSTATE for a unique constraint violation.
const uniqueViolation = "23505"

type PaymentRepository struct {
	pool *pgxpool.Pool
}

func NewPaymentRepository(pool *pgxpool.Pool) *PaymentRepository {
	return &PaymentRepository{pool: pool}
}

const cols = `id, order_id, customer_id, stripe_intent_id, status, amount_cents, currency, paid_at, refunded_at, created_at`

func scanPayment(row pgx.Row) (*domain.Payment, error) {
	var p domain.Payment
	err := row.Scan(&p.ID, &p.OrderID, &p.CustomerID, &p.StripeIntentID, &p.Status,
		&p.AmountCents, &p.Currency, &p.PaidAt, &p.RefundedAt, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.NewNotFoundError("payment not found for this order")
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *PaymentRepository) GetByOrderID(ctx context.Context, orderID string) (*domain.Payment, error) {
	return scanPayment(r.pool.QueryRow(ctx, `SELECT `+cols+` FROM payments WHERE order_id = $1`, orderID))
}

func (r *PaymentRepository) GetByIntentID(ctx context.Context, intentID string) (*domain.Payment, error) {
	return scanPayment(r.pool.QueryRow(ctx, `SELECT `+cols+` FROM payments WHERE stripe_intent_id = $1`, intentID))
}

func (r *PaymentRepository) ListByCustomer(ctx context.Context, customerID string, page domain.Page) ([]domain.Payment, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+cols+` FROM payments WHERE customer_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		customerID, page.Limit, page.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []domain.Payment{}
	for rows.Next() {
		p, err := scanPayment(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *p)
	}
	return list, rows.Err()
}

// Create inserts a payment; a second payment for the same order is a CONFLICT.
func (r *PaymentRepository) Create(ctx context.Context, p *domain.Payment) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO payments (`+cols+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		p.ID, p.OrderID, p.CustomerID, p.StripeIntentID, p.Status,
		p.AmountCents, p.Currency, p.PaidAt, p.RefundedAt, p.CreatedAt)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return domain.NewConflictError("a payment already exists for this order")
	}
	return err
}

func (r *PaymentRepository) UpdateAmount(ctx context.Context, p *domain.Payment) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE payments SET amount_cents = $1 WHERE id = $2 AND status IN ('PENDING', 'FAILED')`,
		p.AmountCents, p.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.NewConflictError("payment is no longer awaiting payment")
	}
	return nil
}

// Transition is a compare-and-set on the status, with the event written to the
// outbox in the same transaction: the event exists if and only if the change
// was committed.
func (r *PaymentRepository) Transition(ctx context.Context, p *domain.Payment, from domain.PaymentStatus, event *domain.Event) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx,
		`UPDATE payments SET status = $1, paid_at = $2, refunded_at = $3 WHERE id = $4 AND status = $5`,
		p.Status, p.PaidAt, p.RefundedAt, p.ID, from)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	if event != nil {
		if err := insertEvent(ctx, tx, event); err != nil {
			return false, err
		}
	}
	return true, tx.Commit(ctx)
}
