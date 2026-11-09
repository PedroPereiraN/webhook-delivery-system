package port

import (
	"github.com/PedroPereiraN/webhook-delivery-system/internal/domain"
	"github.com/google/uuid"
)

type DeadLetterService interface {
	List(limit int) ([]*domain.Outbox, error)
	Replay(id uuid.UUID) error
}
