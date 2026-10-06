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
	MarkDelivered(uuid.UUID) error
	ScheduleRetry(uuid.UUID, time.Duration) error
	MarkFailed(uuid.UUID) error
}

type outboxRepository struct {
	db *sql.DB
}

// Sem foreign keys: o evento precisa sobreviver mesmo se o ticket/customer
// for removido, senão o ON DELETE CASCADE apagaria eventos ainda não entregues.
func (repository *outboxRepository) CreateTable() error {
	query := `CREATE TABLE IF NOT EXISTS outbox (
		id uuid PRIMARY KEY,
		customer_id uuid NOT NULL,
		stock_id uuid NOT NULL,
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

	// Índice parcial: só os eventos que o worker ainda precisa olhar.
	indexQuery := `CREATE INDEX IF NOT EXISTS idx_outbox_due
		ON outbox (next_retry_at)
		WHERE status IN ('pending', 'processing')`

	_, err = repository.db.Exec(indexQuery)
	if err != nil {
		return err
	}

	return nil
}

// insertOutbox grava o evento usando a transação de quem chamou, para que o
// evento só exista se a operação no ticket também for commitada.
func insertOutbox(tx *sql.Tx, event *domain.Outbox) error {
	query := `
		INSERT INTO outbox (
			id,
			customer_id,
			stock_id,
			event_type,
			payload,
			status,
			attempts,
			created_at,
			next_retry_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9
		)
	`

	_, err := tx.Exec(
		query,
		event.ID,
		event.CustomerID,
		event.StockID,
		event.EventType,
		// O lib/pq manda []byte como bytea, então o jsonb precisa ir como string.
		string(event.Payload),
		event.Status,
		event.Attempts,
		event.CreatedAt,
		event.NextRetryAt,
	)

	return err
}

// ClaimPending pega até `limit` eventos vencidos e marca como processing por
// `lease`. O SKIP LOCKED deixa várias réplicas do worker rodarem sem pegar o
// mesmo evento. Se o worker morrer no meio da entrega, o lease expira e o
// evento volta a ser elegível. Attempts é incrementado aqui para que um evento
// que sempre derruba o worker não fique em loop infinito.
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
		RETURNING id, customer_id, stock_id, event_type, payload, status, attempts, created_at, next_retry_at
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
			&event.CustomerID,
			&event.StockID,
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

func (repository *outboxRepository) MarkDelivered(id uuid.UUID) error {
	query := `UPDATE outbox SET status = 'delivered' WHERE id = $1`

	_, err := repository.db.Exec(query, id)

	return err
}

// ScheduleRetry devolve o evento para pending, elegível de novo após `delay`.
func (repository *outboxRepository) ScheduleRetry(id uuid.UUID, delay time.Duration) error {
	query := `
		UPDATE outbox
		SET
			status = 'pending',
			next_retry_at = NOW() + make_interval(secs => $2)
		WHERE id = $1
	`

	_, err := repository.db.Exec(query, id, delay.Seconds())

	return err
}

func (repository *outboxRepository) MarkFailed(id uuid.UUID) error {
	query := `UPDATE outbox SET status = 'failed' WHERE id = $1`

	_, err := repository.db.Exec(query, id)

	return err
}
