package repository

import (
	"database/sql"
	"time"

	"github.com/PedroPereiraN/webhook-delivery-system/internal/domain"
	"github.com/google/uuid"
)

func NewOutboxRepository(db *sql.DB) OutboxRepository {
	return &outboxRepository{
		db: db,
	}
}

type OutboxRepository interface {
	CreateTable() error
	ClaimPending(limit int, lease time.Duration) ([]*domain.Outbox, error)
	MarkDelivered(id uuid.UUID, attempt int) error
	ScheduleRetry(id uuid.UUID, attempt int, delay time.Duration) error
	MarkFailed(id uuid.UUID, attempt int) error
}

type outboxRepository struct {
	db *sql.DB
}

// No foreign keys: the event must persist even if the ticket is removed.
// The email is stored in a dedicated column—separate from the payload—to
// allow querying events for a specific recipient without having to parse the JSONB.
func (repository *outboxRepository) CreateTable() error {
	query := `CREATE TABLE IF NOT EXISTS outbox (
		id uuid PRIMARY KEY,
		email text NOT NULL,
		event_type text NOT NULL,
		payload jsonb NOT NULL,
		status text NOT NULL DEFAULT 'pending',
		attempts int NOT NULL DEFAULT 0,
		created_at timestamptz NOT NULL DEFAULT NOW(),
		next_retry_at timestamptz NOT NULL DEFAULT NOW()
	)`

	_, err := repository.db.Exec(query)
	if err != nil {
		return err
	}

	// Partial index: only the events that the worker still needs to look at.
	indexQuery := `CREATE INDEX IF NOT EXISTS idx_outbox_due
		ON outbox (next_retry_at)
		WHERE status IN ('pending', 'processing')`

	_, err = repository.db.Exec(indexQuery)
	if err != nil {
		return err
	}

	return nil
}

// insertOutbox records the event using the caller's transaction, ensuring that the
// event only exists if the operation on the ticket is also committed.
func insertOutbox(tx *sql.Tx, event *domain.Outbox) error {
	query := `
		INSERT INTO outbox (
			id,
			email,
			event_type,
			payload,
			status,
			attempts,
			created_at,
			next_retry_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8
		)
	`

	_, err := tx.Exec(
		query,
		event.ID,
		event.Email,
		event.EventType,
		// lib/pq sends []byte as bytea, so jsonb must be sent as a string.
		string(event.Payload),
		event.Status,
		event.Attempts,
		event.CreatedAt,
		event.NextRetryAt,
	)

	return err
}

// ClaimPending retrieves up to `limit` overdue events and marks them as 'processing' for
// a specified `lease` duration. The `SKIP LOCKED` clause allows multiple worker replicas
// to run concurrently without claiming the same event. If a worker fails during
// delivery, the lease expires, and the event becomes eligible for processing again.
// The `attempts` counter is incremented here to ensure that an event which consistently
// causes a worker to crash does not result in an infinite loop.
func (repository *outboxRepository) ClaimPending(limit int, lease time.Duration) ([]*domain.Outbox, error) {
	query := `
		UPDATE outbox
		SET
			status = 'processing',
			attempts = attempts + 1,
			next_retry_at = NOW() + make_interval(secs => $2)
		WHERE id IN (
			SELECT id FROM outbox
			WHERE status IN ('pending', 'processing') AND next_retry_at <= NOW()
			ORDER BY created_at
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, email, event_type, payload, status, attempts, created_at, next_retry_at
	`

	rows, err := repository.db.Query(query, limit, lease.Seconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := make([]*domain.Outbox, 0, limit)

	for rows.Next() {
		var event domain.Outbox
		var payload []byte

		err := rows.Scan(
			&event.ID,
			&event.Email,
			&event.EventType,
			&payload,
			&event.Status,
			&event.Attempts,
			&event.CreatedAt,
			&event.NextRetryAt,
		)
		if err != nil {
			return nil, err
		}

		event.Payload = payload
		events = append(events, &event)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return events, nil
}

// MarkDelivered, ScheduleRetry, and MarkFailed only update the event if it
// is still in the 'processing' state with the same `attempt` value that the
// worker retrieved during ClaimPending. If the lease has expired and another
// worker has picked up the event, the `attempt` value will have changed;
// consequently, the update from the delayed worker will have no effect,
// preventing it from overwriting the result of the more recent attempt.
func (repository *outboxRepository) MarkDelivered(id uuid.UUID, attempt int) error {
	query := `
		UPDATE outbox
		SET status = 'delivered'
		WHERE id = $1 AND status = 'processing' AND attempts = $2
	`

	_, err := repository.db.Exec(query, id, attempt)

	return err
}

// ScheduleRetry returns the event to pending, making it eligible again after `delay`.
func (repository *outboxRepository) ScheduleRetry(id uuid.UUID, attempt int, delay time.Duration) error {
	query := `
		UPDATE outbox
		SET
			status = 'pending',
			next_retry_at = NOW() + make_interval(secs => $3)
		WHERE id = $1 AND status = 'processing' AND attempts = $2
	`

	_, err := repository.db.Exec(query, id, attempt, delay.Seconds())

	return err
}

func (repository *outboxRepository) MarkFailed(id uuid.UUID, attempt int) error {
	query := `
		UPDATE outbox
		SET status = 'failed'
		WHERE id = $1 AND status = 'processing' AND attempts = $2
	`

	_, err := repository.db.Exec(query, id, attempt)

	return err
}
