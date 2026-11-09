package repository

import (
	"database/sql"
	"time"

	"github.com/PedroPereiraN/webhook-delivery-system/internal/domain"
	"github.com/PedroPereiraN/webhook-delivery-system/internal/helpers"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

func NewTicketRepository(db *sql.DB) TicketRepository {
	return &ticketRepository{
		db: db,
	}
}

type TicketRepository interface {
	CreateTable() error
	ReserveMany(tickets []*domain.Ticket, event *domain.Outbox) ([]uuid.UUID, error)
	FindTicketByID(uuid.UUID) (*domain.Ticket, error)
	FindTicketsByIDs([]uuid.UUID) ([]*domain.Ticket, error)
	DeleteMany([]uuid.UUID, []*domain.Outbox) error
	CountTickets() (reserved int, returned int, err error)
}

type ticketRepository struct {
	db *sql.DB
}

func (repository *ticketRepository) CreateTable() error {
	query := `CREATE TABLE IF NOT EXISTS tickets (
		id uuid PRIMARY KEY,
		email text NOT NULL,
		reserved_at timestamptz NOT NULL,
		created_at timestamptz NOT NULL DEFAULT NOW(),
		deleted_at timestamptz
	)`

	_, err := repository.db.Exec(query)
	if err != nil {
		return err
	}

	return nil
}

func (repository *ticketRepository) ReserveMany(tickets []*domain.Ticket, event *domain.Outbox) ([]uuid.UUID, error) {
	tx, err := repository.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	quantity := len(tickets)

	ticketQuery := `
		INSERT INTO tickets (
			id,
			email,
			reserved_at,
			created_at
		) VALUES (
			$1, $2, $3, $4
		)
	`

	stmt, err := tx.Prepare(ticketQuery)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	ids := make([]uuid.UUID, 0, quantity)

	for _, ticket := range tickets {
		_, err := stmt.Exec(
			ticket.ID,
			ticket.Email,
			ticket.ReservedAt,
			ticket.CreatedAt,
		)
		if err != nil {
			return nil, err
		}

		ids = append(ids, ticket.ID)
	}

	if err := insertOutbox(tx, event); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return ids, nil
}

func (repository *ticketRepository) FindTicketByID(id uuid.UUID) (*domain.Ticket, error) {
	tDomain := domain.Ticket{}
	var reservedAt sql.NullTime
	var createdAt sql.NullTime
	var deletedAt sql.NullTime

	query := `SELECT id, email, reserved_at, created_at, deleted_at FROM tickets WHERE id = $1 AND deleted_at IS NULL`

	err := repository.db.QueryRow(query, id).Scan(&tDomain.ID, &tDomain.Email, &reservedAt, &createdAt, &deletedAt)
	if err != nil {
		return &domain.Ticket{}, err
	}

	tDomain.ReservedAt, err = helpers.ParseSqlDate(reservedAt)
	if err != nil {
		return &domain.Ticket{}, err
	}

	tDomain.CreatedAt, err = helpers.ParseSqlDate(createdAt)
	if err != nil {
		return &domain.Ticket{}, err
	}

	tDomain.DeletedAt, err = helpers.ParseSqlDate(deletedAt)
	if err != nil {
		return &domain.Ticket{}, err
	}

	return &tDomain, nil
}

func (repository *ticketRepository) FindTicketsByIDs(ids []uuid.UUID) ([]*domain.Ticket, error) {
	query := `
		SELECT id, email, reserved_at, created_at
		FROM tickets
		WHERE id = ANY($1) AND deleted_at IS NULL
	`

	rows, err := repository.db.Query(query, pq.Array(helpers.UuidsToStrings(ids)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tickets := make([]*domain.Ticket, 0, len(ids))

	for rows.Next() {
		var ticket domain.Ticket
		var reservedAt sql.NullTime
		var createdAt sql.NullTime

		err := rows.Scan(&ticket.ID, &ticket.Email, &reservedAt, &createdAt)
		if err != nil {
			return nil, err
		}

		ticket.ReservedAt, err = helpers.ParseSqlDate(reservedAt)
		if err != nil {
			return nil, err
		}

		ticket.CreatedAt, err = helpers.ParseSqlDate(createdAt)
		if err != nil {
			return nil, err
		}

		tickets = append(tickets, &ticket)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return tickets, nil
}

func (repository *ticketRepository) DeleteMany(ids []uuid.UUID, events []*domain.Outbox) error {
	tx, err := repository.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	ticketQuery := `
		UPDATE tickets
		SET deleted_at = $2
		WHERE id = ANY($1) AND deleted_at IS NULL
	`

	result, err := tx.Exec(ticketQuery, pq.Array(helpers.UuidsToStrings(ids)), time.Now())
	if err != nil {
		return err
	}

	deleted, err := result.RowsAffected()
	if err != nil {
		return err
	}

	// Another request might have deleted a ticket between the validation step and this point;
	// in that case, everything is rolled back.
	if int(deleted) != len(ids) {
		return domain.ErrTicketNotFound
	}

	for _, event := range events {
		if err := insertOutbox(tx, event); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (repository *ticketRepository) CountTickets() (int, int, error) {
	query := `
		SELECT
			COUNT(*) FILTER (WHERE deleted_at IS NULL),
			COUNT(*) FILTER (WHERE deleted_at IS NOT NULL)
		FROM tickets
	`

	var reserved, returned int

	if err := repository.db.QueryRow(query).Scan(&reserved, &returned); err != nil {
		return 0, 0, err
	}

	return reserved, returned, nil
}
