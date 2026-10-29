package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CustomerRepository maps users to their provider customer.
type CustomerRepository struct {
	pool *pgxpool.Pool
}

func NewCustomerRepository(pool *pgxpool.Pool) *CustomerRepository {
	return &CustomerRepository{pool: pool}
}

// GetProviderCustomerID returns "" when the user has no provider customer yet.
func (r *CustomerRepository) GetProviderCustomerID(ctx context.Context, customerID string) (string, error) {
	var id string
	err := r.pool.QueryRow(ctx,
		`SELECT provider_customer_id FROM payment_customers WHERE customer_id = $1`, customerID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return id, err
}

// SaveProviderCustomerID keeps the first mapping if two requests race.
func (r *CustomerRepository) SaveProviderCustomerID(ctx context.Context, customerID, providerCustomerID string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO payment_customers (customer_id, provider_customer_id) VALUES ($1, $2)
		 ON CONFLICT (customer_id) DO NOTHING`, customerID, providerCustomerID)
	return err
}
