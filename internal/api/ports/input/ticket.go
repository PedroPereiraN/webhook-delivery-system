package port

import (
	"github.com/PedroPereiraN/webhook-delivery-system/internal/api/adapter/input/models"
	"github.com/PedroPereiraN/webhook-delivery-system/internal/domain"
	"github.com/google/uuid"
)

type TicketService interface {
	Create(*model.CreateTicket) ([]uuid.UUID, error)
	List(uuid.UUID) (*domain.Ticket, error)
	DeleteMany([]uuid.UUID) ([]uuid.UUID, error)
	Stock() (*domain.Stock, error)
}
