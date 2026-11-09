package port

import (
	"github.com/PedroPereiraN/webhook-delivery-system/internal/domain"
	"github.com/google/uuid"
)

type DeadLetterRepository interface {
	ListFailed(limit int) ([]*domain.Outbox, error)
	Replay(id uuid.UUID) (bool, error)
}
