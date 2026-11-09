package model

import (
	"time"

	"github.com/google/uuid"
)

type CreateTicket struct {
	Email    string `json:"email" binding:"required,email"`
	Quantity int32  `json:"quantity" binding:"required,gt=0"`
}

type DeleteTickets struct {
	IDs []uuid.UUID `json:"ids" binding:"required,min=1,max=100"`
}

type Ticket struct {
	ID         uuid.UUID `json:"id"`
	Email      string    `json:"email"`
	ReservedAt time.Time `json:"ReservedAt"`
	CreatedAt  time.Time `json:"CreatedAt"`
	DeletedAt  time.Time `json:"DeletedAt"`
}

type Stock struct {
	Reserved int `json:"reserved"`
	Returned int `json:"returned"`
}
