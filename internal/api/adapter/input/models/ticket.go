package model

import (
	"time"

	"github.com/google/uuid"
)

type CreateTicket struct {
	ReservedBy uuid.UUID `json:"reservedBy" binding:"required"`
	Quantity   int32     `json:"quantity" binding:"required,gt=0"`
}

type DeleteTickets struct {
	IDs []uuid.UUID `json:"ids" binding:"required,min=1,max=100"`
}

type Ticket struct {
	ID         uuid.UUID `json:"id"`
	ReservedBy uuid.UUID `json:"reservedBy"`
	ReservedAt time.Time `json:"ReservedAt"`
	CreatedAt  time.Time `json:"CreatedAt"`
	DeletedAt  time.Time `json:"DeletedAt"`
}
