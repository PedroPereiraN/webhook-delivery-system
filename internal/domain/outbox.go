package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Transactional outbox: Persist the event before attempting delivery.
// Write the event to an outbox table or queue within the same transaction that produced the event.
// A separate worker/process reads the outbox and delivers webhooks asynchronously.

type Outbox struct {
	ID          uuid.UUID
	CustomerID  uuid.UUID
	TicketID    uuid.UUID
	Payload     json.RawMessage
	Status      EventStatus
	Attempts    int
	CreatedAt   time.Time
	NextRetryAt time.Time
}

type EventStatus string

const (
	EventPending    EventStatus = "pending"
	EventProcessing EventStatus = "processing"
	EventDelivered  EventStatus = "delivered"
	EventFailed     EventStatus = "failed"
)
