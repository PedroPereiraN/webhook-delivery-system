package port

import (
	"github.com/PedroPereiraN/webhook-delivery-system/internal/domain"
	"github.com/google/uuid"
)

type CustomerRepository interface {
	Create(*domain.Customer) (uuid.UUID, error)
	FindCustomerByEmail(string) (*domain.Customer, error)
	FindCustomerById(uuid.UUID) (*domain.Customer, error)
	Update(uuid.UUID, *domain.Customer) (uuid.UUID, error)
	Delete(uuid.UUID) (uuid.UUID, error)
}
