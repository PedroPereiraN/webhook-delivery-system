package model

import (
	"time"

	"github.com/google/uuid"
)

type CreateTicket struct {
	ReservedBy uuid.UUID `json:"reservedBy" binding:"required"`
}

type Ticket struct {
	ID         uuid.UUID `json:"id"`
	ReservedBy uuid.UUID `json:"reservedBy"`
	ReservedAt time.Time `json:"ReservedAt"`
	CreatedAt  time.Time `json:"CreatedAt"`
	DeletedAt  time.Time `json:"DeletedAt"`
}
