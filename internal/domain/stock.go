package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrInsufficientStock = errors.New("insufficient free quantity in stock")

type Stock struct {
	ID               uuid.UUID
	Name             string
	TotalQuantity    int32
	ReservedQuantity int32
	FreeQuantity     int32
	CreatedAt        time.Time
}
