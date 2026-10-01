package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"goodfood/payment-service/internal/domain"
)

type PaymentMethodRepository struct {
	pool *pgxpool.Pool
}

func NewPaymentMethodRepository(pool *pgxpool.Pool) *PaymentMethodRepository {
	return &PaymentMethodRepository{pool: pool}
}

const paymentMethodCols = `id, customer_id, cardholder_name, brand, last4, exp_month, exp_year, is_default, created_at`

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
		var m domain.PaymentMethod
		if err := rows.Scan(&m.ID, &m.CustomerID, &m.CardholderName, &m.Brand, &m.Last4,
			&m.ExpMonth, &m.ExpYear, &m.IsDefault, &m.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, m)
	}
	return list, rows.Err()
}

func (r *PaymentMethodRepository) GetByID(ctx context.Context, id string) (*domain.PaymentMethod, error) {
	var m domain.PaymentMethod
	err := r.pool.QueryRow(ctx, `SELECT `+paymentMethodCols+` FROM payment_methods WHERE id = $1`, id).
		Scan(&m.ID, &m.CustomerID, &m.CardholderName, &m.Brand, &m.Last4,
			&m.ExpMonth, &m.ExpYear, &m.IsDefault, &m.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.NewNotFoundError("payment method not found")
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *PaymentMethodRepository) Create(ctx context.Context, m *domain.PaymentMethod) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO payment_methods (`+paymentMethodCols+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		m.ID, m.CustomerID, m.CardholderName, m.Brand, m.Last4, m.ExpMonth, m.ExpYear, m.IsDefault, m.CreatedAt)
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

func (r *PaymentMethodRepository) ClearDefault(ctx context.Context, customerID string) error {
	_, err := r.pool.Exec(ctx, `UPDATE payment_methods SET is_default = FALSE WHERE customer_id = $1`, customerID)
	return err
}
