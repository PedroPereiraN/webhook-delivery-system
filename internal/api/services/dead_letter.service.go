package service

import (
	"github.com/PedroPereiraN/webhook-delivery-system/internal/api/ports/output"
	"github.com/PedroPereiraN/webhook-delivery-system/internal/domain"
	"github.com/google/uuid"
)

const (
	defaultDeadLetterLimit = 100
	maxDeadLetterLimit     = 500
)

func NewDeadLetterService(repository port.DeadLetterRepository) DeadLetterService {
	return &deadLetterService{
		repository: repository,
	}
}

type DeadLetterService interface {
	List(limit int) ([]*domain.Outbox, error)
	Replay(id uuid.UUID) error
}

type deadLetterService struct {
	repository port.DeadLetterRepository
}

func (service *deadLetterService) List(limit int) ([]*domain.Outbox, error) {
	if limit <= 0 {
		limit = defaultDeadLetterLimit
	}

	return service.repository.ListFailed(min(limit, maxDeadLetterLimit))
}

// Replay sends a dead letter back to the workers. It does not check why the
// event failed: replaying a 4xx without fixing the cause just fails again, so
// look at last_error first.
func (service *deadLetterService) Replay(id uuid.UUID) error {
	found, err := service.repository.Replay(id)
	if err != nil {
		return err
	}

	if !found {
		return domain.ErrDeadLetterNotFound
	}

	return nil
}
