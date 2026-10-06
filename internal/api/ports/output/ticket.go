package port

import (
	"github.com/PedroPereiraN/webhook-delivery-system/internal/domain"
	"github.com/google/uuid"
)

type TicketRepository interface {
	ReserveMany(uuid.UUID, []*domain.Ticket, *domain.Outbox) ([]uuid.UUID, error)
	FindTicketByID(uuid.UUID) (*domain.Ticket, error)
	FindTicketsByIDs([]uuid.UUID) ([]*domain.Ticket, error)
	DeleteMany([]uuid.UUID, []*domain.Outbox) error
}
