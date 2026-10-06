package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrCustomerNotFound = errors.New("customer not found")

type Customer struct {
	ID        uuid.UUID
	Name      string
	Email     string
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt time.Time
}
