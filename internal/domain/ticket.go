package domain

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrTicketNotFound = errors.New("one or more tickets were not found")

type Ticket struct {
	ID         uuid.UUID
	Email      string
	ReservedAt time.Time
	CreatedAt  time.Time
	DeletedAt  time.Time
}

// Stock is not stored in a table: it is computed from the tickets.
// Reserved are the active tickets and Returned the returned ones (soft delete).
type Stock struct {
	Reserved int
	Returned int
}

// TicketEventPayload is the body the worker sends to the webhook.
type TicketEventPayload struct {
	EventID    uuid.UUID   `json:"event_id"`
	EventType  EventType   `json:"event_type"`
	Email      string      `json:"email"`
	Quantity   int         `json:"quantity"`
	TicketIDs  []uuid.UUID `json:"ticket_ids"`
	OccurredAt time.Time   `json:"occurred_at"`
}

func NewTicketEvent(
	eventType EventType,
	email string,
	ticketIDs []uuid.UUID,
	now time.Time,
) (*Outbox, error) {
	eventID := uuid.New()

	payload, err := json.Marshal(TicketEventPayload{
		EventID:    eventID,
		EventType:  eventType,
		Email:      email,
		Quantity:   len(ticketIDs),
		TicketIDs:  ticketIDs,
		OccurredAt: now,
	})
	if err != nil {
		return nil, err
	}

	return &Outbox{
		ID:          eventID,
		Email:       email,
		Payload:     payload,
		EventType:   eventType,
		Status:      EventPending,
		Attempts:    0,
		CreatedAt:   now,
		NextRetryAt: now,
	}, nil
}
