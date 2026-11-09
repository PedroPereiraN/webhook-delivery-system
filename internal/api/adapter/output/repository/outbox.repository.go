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
	MarkFailed(id uuid.UUID, attempt int, reason string) error
	ListFailed(limit int) ([]*domain.Outbox, error)
	Replay(id uuid.UUID) (bool, error)
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

	// Added after the table existed, so ADD COLUMN IF NOT EXISTS updates
	// databases created before the dead letter columns.
	alterQuery := `ALTER TABLE outbox
		ADD COLUMN IF NOT EXISTS last_error text,
		ADD COLUMN IF NOT EXISTS failed_at timestamptz`

	_, err = repository.db.Exec(alterQuery)
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

// MarkFailed moves the event to the dead letters, keeping why it failed so
// someone can decide whether a replay makes sense.
func (repository *outboxRepository) MarkFailed(id uuid.UUID, attempt int, reason string) error {
	query := `
		UPDATE outbox
		SET
			status = 'failed',
			last_error = $3,
			failed_at = NOW()
		WHERE id = $1 AND status = 'processing' AND attempts = $2
	`

	_, err := repository.db.Exec(query, id, attempt, reason)

	return err
}

// ListFailed returns the dead letters, most recent failure first.
func (repository *outboxRepository) ListFailed(limit int) ([]*domain.Outbox, error) {
	query := `
		SELECT id, email, event_type, payload, status, attempts, created_at, next_retry_at,
			COALESCE(last_error, ''), failed_at
		FROM outbox
		WHERE status = 'failed'
		ORDER BY failed_at DESC NULLS LAST
		LIMIT $1
	`

	rows, err := repository.db.Query(query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := make([]*domain.Outbox, 0)

	for rows.Next() {
		var event domain.Outbox
		var payload []byte
		var failedAt sql.NullTime

		err := rows.Scan(
			&event.ID,
			&event.Email,
			&event.EventType,
			&payload,
			&event.Status,
			&event.Attempts,
			&event.CreatedAt,
			&event.NextRetryAt,
			&event.LastError,
			&failedAt,
		)
		if err != nil {
			return nil, err
		}

		event.Payload = payload
		event.FailedAt = failedAt.Time
		events = append(events, &event)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return events, nil
}

// Replay puts a failed event back in the queue as if it were new: attempts
// start over so it gets the full retry budget again. It keeps the same id, so
// a receiver that already processed it (e.g. the last attempt timed out after
// the email was sent) deduplicates it. Returns false if the event does not
// exist or is not failed.
func (repository *outboxRepository) Replay(id uuid.UUID) (bool, error) {
	query := `
		UPDATE outbox
		SET
			status = 'pending',
			attempts = 0,
			next_retry_at = NOW(),
			last_error = NULL,
			failed_at = NULL
		WHERE id = $1 AND status = 'failed'
	`

	result, err := repository.db.Exec(query, id)
	if err != nil {
		return false, err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}

	return affected == 1, nil
}
