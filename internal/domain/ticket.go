package domain

import (
	"time"

	"github.com/google/uuid"
)

type Ticket struct {
	ID         uuid.UUID
	StockID    uuid.UUID
	ReservedBy uuid.UUID
	ReservedAt time.Time
	CreatedAt  time.Time
	DeletedAt  time.Time
}
