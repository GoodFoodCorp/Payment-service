package application

import (
	"goodfood/payment-service/internal/domain"
)

// Actor is the authenticated caller of a public endpoint.
type Actor struct {
	UserID    string
	RoleSlugs []string
}

func (a Actor) HasRole(slug string) bool {
	for _, r := range a.RoleSlugs {
		if r == slug {
			return true
		}
	}
	return false
}

const RoleAdmin = "admin"

// Pagination bounds of the public lists.
const (
	DefaultPageSize = 50
	MaxPageSize     = 200
)

// NormalizePage clamps a requested page to the allowed bounds.
func NormalizePage(limit, offset int) domain.Page {
	if limit <= 0 {
		limit = DefaultPageSize
	}
	if limit > MaxPageSize {
		limit = MaxPageSize
	}
	if offset < 0 {
		offset = 0
	}
	return domain.Page{Limit: limit, Offset: offset}
}

// UseCases wires the ports together. The use cases are split by concern:
// intents.go (order-service, trusted), webhooks.go (provider), methods.go
// (saved cards) and queries.go (customer reads).
type UseCases struct {
	payments       domain.PaymentRepository
	gateway        domain.PaymentGateway
	paymentMethods domain.PaymentMethodRepository
	customers      domain.CustomerRepository
}

func NewUseCases(payments domain.PaymentRepository, gateway domain.PaymentGateway,
	paymentMethods domain.PaymentMethodRepository, customers domain.CustomerRepository) *UseCases {
	return &UseCases{payments: payments, gateway: gateway, paymentMethods: paymentMethods, customers: customers}
}
