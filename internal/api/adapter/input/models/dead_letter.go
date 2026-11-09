package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type DeadLetter struct {
	ID        uuid.UUID       `json:"id"`
	Email     string          `json:"email"`
	EventType string          `json:"eventType"`
	Attempts  int             `json:"attempts"`
	LastError string          `json:"lastError"`
	FailedAt  time.Time       `json:"failedAt"`
	CreatedAt time.Time       `json:"createdAt"`
	Payload   json.RawMessage `json:"payload"`
}
