package port

import (
	"github.com/PedroPereiraN/webhook-delivery-system/internal/api/adapter/input/models"
	"github.com/PedroPereiraN/webhook-delivery-system/internal/domain"
	"github.com/google/uuid"
)

type CustomerService interface {
	Create(*model.CreateCustomer) (uuid.UUID, error)
	List(uuid.UUID) (*domain.Customer, error)
	Update(
		uuid.UUID,
		*model.UpdateCustomer,
	) (uuid.UUID, error)
	Delete(uuid.UUID) (uuid.UUID, error)
}
