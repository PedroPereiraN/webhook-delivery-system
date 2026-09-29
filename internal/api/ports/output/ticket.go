package port

import (
	"github.com/PedroPereiraN/webhook-delivery-system/internal/domain"
	"github.com/google/uuid"
)

type TicketRepository interface {
	Create(*domain.Ticket) (uuid.UUID, error)
	ReserveMany(uuid.UUID, []*domain.Ticket) ([]uuid.UUID, error)
	FindTicketByID(uuid.UUID) (*domain.Ticket, error)
	Delete(uuid.UUID) (uuid.UUID, error)
}
