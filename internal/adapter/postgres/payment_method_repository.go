package postgres

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"goodfood/payment-service/internal/domain"
)

func (r *PaymentRepository) ListPaymentMethods(ctx context.Context, customerID string) ([]domain.PaymentMethod, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, customer_id, provider_payment_method_id, brand, last4, expiry_month, expiry_year, is_default, created_at FROM payment_methods WHERE customer_id = $1 ORDER BY is_default DESC, created_at DESC`, customerID)
	if err != nil { return nil, err }
	defer rows.Close()
	items := []domain.PaymentMethod{}
	for rows.Next() {
		var item domain.PaymentMethod
		if err := rows.Scan(&item.ID, &item.CustomerID, &item.ProviderPaymentMethodID, &item.Brand, &item.Last4, &item.ExpiryMonth, &item.ExpiryYear, &item.IsDefault, &item.CreatedAt); err != nil { return nil, err }
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PaymentRepository) CreatePaymentMethod(ctx context.Context, item *domain.PaymentMethod) error {
	if item.IsDefault { if _, err := r.pool.Exec(ctx, `UPDATE payment_methods SET is_default = false WHERE customer_id = $1`, item.CustomerID); err != nil { return err } }
	_, err := r.pool.Exec(ctx, `INSERT INTO payment_methods (id, customer_id, provider_payment_method_id, brand, last4, expiry_month, expiry_year, is_default, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, item.ID, item.CustomerID, item.ProviderPaymentMethodID, item.Brand, item.Last4, item.ExpiryMonth, item.ExpiryYear, item.IsDefault, item.CreatedAt)
	return err
}

func (r *PaymentRepository) DeletePaymentMethod(ctx context.Context, customerID, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM payment_methods WHERE id = $1 AND customer_id = $2`, id, customerID)
	if err != nil { return err }
	if tag.RowsAffected() == 0 { return domain.NewNotFoundError("payment method not found") }
	return nil
}
