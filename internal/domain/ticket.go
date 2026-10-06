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
	StockID    uuid.UUID
	ReservedBy uuid.UUID
	ReservedAt time.Time
	CreatedAt  time.Time
	DeletedAt  time.Time
}

// ticketEventPayload é o corpo que o worker vai enviar para o webhook.
type TicketEventPayload struct {
	EventID       uuid.UUID   `json:"event_id"`
	EventType     EventType   `json:"event_type"`
	CustomerID    uuid.UUID   `json:"customer_id"`
	CustomerEmail string      `json:"customer_email"`
	StockID       uuid.UUID   `json:"stock_id"`
	Quantity      int         `json:"quantity"`
	TicketIDs     []uuid.UUID `json:"ticket_ids"`
	OccurredAt    time.Time   `json:"occurred_at"`
}

func NewTicketEvent(
	eventType EventType,
	customer *Customer,
	stockID uuid.UUID,
	ticketIDs []uuid.UUID,
	now time.Time,
) (*Outbox, error) {
	eventID := uuid.New()

	payload, err := json.Marshal(TicketEventPayload{
		EventID:       eventID,
		EventType:     eventType,
		CustomerID:    customer.ID,
		CustomerEmail: customer.Email,
		StockID:       stockID,
		Quantity:      len(ticketIDs),
		TicketIDs:     ticketIDs,
		OccurredAt:    now,
	})
	if err != nil {
		return nil, err
	}

	return &Outbox{
		ID:          eventID,
		CustomerID:  customer.ID,
		StockID:     stockID,
		Payload:     payload,
		EventType:   eventType,
		Status:      EventPending,
		Attempts:    0,
		CreatedAt:   now,
		NextRetryAt: now,
	}, nil
}
