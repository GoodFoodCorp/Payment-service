package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"goodfood/payment-service/internal/domain"
)

type PaymentMethodRepository struct {
	pool *pgxpool.Pool
}

func NewPaymentMethodRepository(pool *pgxpool.Pool) *PaymentMethodRepository {
	return &PaymentMethodRepository{pool: pool}
}

// Cards saved before the provider flow have no provider id (demo rows).
const paymentMethodCols = `id, customer_id, COALESCE(provider_payment_method_id, ''), cardholder_name, brand, last4, exp_month, exp_year, is_default, created_at`

func scanMethod(row pgx.Row) (*domain.PaymentMethod, error) {
	var m domain.PaymentMethod
	err := row.Scan(&m.ID, &m.CustomerID, &m.ProviderPaymentMethodID, &m.CardholderName, &m.Brand, &m.Last4,
		&m.ExpMonth, &m.ExpYear, &m.IsDefault, &m.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.NewNotFoundError("payment method not found")
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *PaymentMethodRepository) ListByCustomer(ctx context.Context, customerID string) ([]domain.PaymentMethod, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+paymentMethodCols+` FROM payment_methods
		 WHERE customer_id = $1 ORDER BY is_default DESC, created_at DESC`, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []domain.PaymentMethod{}
	for rows.Next() {
		m, err := scanMethod(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *m)
	}
	return list, rows.Err()
}

func (r *PaymentMethodRepository) GetByID(ctx context.Context, id string) (*domain.PaymentMethod, error) {
	return scanMethod(r.pool.QueryRow(ctx, `SELECT `+paymentMethodCols+` FROM payment_methods WHERE id = $1`, id))
}

// Create stores the card; a new default replaces the previous one atomically.
func (r *PaymentMethodRepository) Create(ctx context.Context, m *domain.PaymentMethod) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if m.IsDefault {
		if _, err := tx.Exec(ctx,
			`UPDATE payment_methods SET is_default = FALSE WHERE customer_id = $1 AND is_default`, m.CustomerID); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO payment_methods (id, customer_id, provider_payment_method_id, cardholder_name, brand, last4, exp_month, exp_year, is_default, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		m.ID, m.CustomerID, m.ProviderPaymentMethodID, m.CardholderName, m.Brand, m.Last4,
		m.ExpMonth, m.ExpYear, m.IsDefault, m.CreatedAt)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return domain.NewConflictError("this card is already saved")
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *PaymentMethodRepository) DeleteAllOf(ctx context.Context, customerID string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM payment_methods WHERE customer_id = $1`, customerID)
	return err
}

func (r *PaymentMethodRepository) Delete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM payment_methods WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.NewNotFoundError("payment method not found")
	}
	return nil
}
