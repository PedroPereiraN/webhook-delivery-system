package domain

import (
	"time"

	"github.com/google/uuid"
)

type Ticket struct {
	ID          uuid.UUID
	ReserverdBy uuid.UUID
	BuyedBy     uuid.UUID
	price       float32
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   time.Time
}
