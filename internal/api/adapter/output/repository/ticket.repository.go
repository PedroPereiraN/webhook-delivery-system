package repository

import (
	"database/sql"
	"time"

	"github.com/PedroPereiraN/webhook-delivery-system/internal/domain"
	"github.com/PedroPereiraN/webhook-delivery-system/internal/helpers"
	"github.com/google/uuid"
)

func NewTicketRepository(db *sql.DB) TicketRepository {
	return &ticketRepository{
		db: db,
	}
}

type TicketRepository interface {
	CreateTable() error
	Create(*domain.Ticket) (uuid.UUID, error)
	ReserveMany(uuid.UUID, []*domain.Ticket) ([]uuid.UUID, error)
	FindTicketByID(uuid.UUID) (*domain.Ticket, error)
	Delete(id uuid.UUID) (uuid.UUID, error)
}

type ticketRepository struct {
	db *sql.DB
}

func (repository *ticketRepository) CreateTable() error {
	query := `CREATE TABLE IF NOT EXISTS tickets (
    id uuid UNIQUE PRIMARY KEY,
		stock_id uuid NOT NULL,
		CONSTRAINT foreign_key_stock
			FOREIGN KEY (stock_id)
      REFERENCES stocks (id)
      ON DELETE CASCADE,
		reserved_by uuid NOT NULL,
		CONSTRAINT foreign_key_customer
			FOREIGN KEY (reserved_by)
      REFERENCES customers (id)
      ON DELETE CASCADE,
		reserved_at timestamp,
    createdAt timestamp DEFAULT NOW(),
    deletedAt timestamp
  )`

	_, err := repository.db.Exec(query)
	if err != nil {
		return err
	}

	return nil
}

func (repository *ticketRepository) Create(dto *domain.Ticket) (uuid.UUID, error) {
	query := `
		INSERT INTO tickets (
			id,
			stock_id,
			reserved_by,
			reserved_at,
			createdAt,
			deletedAt
		) VALUES (
			$1, $2, $3, $4, $5, $6
		) RETURNING id
	`

	var pk uuid.UUID

	err := repository.db.QueryRow(
		query,
		dto.ID,
		dto.StockID,
		dto.ReservedBy,
		dto.ReservedAt,
		dto.CreatedAt,
		nil,
	).Scan(&pk)
	if err != nil {
		return uuid.Nil, err
	}

	return pk, nil
}

// ReserveMany reserva os tickets no stock e insere todos numa única transação:
// ou tudo é criado e o stock é atualizado, ou nada acontece.
func (repository *ticketRepository) ReserveMany(stockID uuid.UUID, tickets []*domain.Ticket) ([]uuid.UUID, error) {
	tx, err := repository.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	quantity := len(tickets)

	// O WHERE garante no banco que só reserva se houver quantidade livre,
	// mesmo com duas requisições concorrentes.
	stockQuery := `
		UPDATE stocks
		SET
			free_quantity = free_quantity - $2,
			reserved_quantity = reserved_quantity + $2
		WHERE id = $1 AND free_quantity >= $2
	`

	result, err := tx.Exec(stockQuery, stockID, quantity)
	if err != nil {
		return nil, err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}

	if affected == 0 {
		return nil, domain.ErrInsufficientStock
	}

	ticketQuery := `
		INSERT INTO tickets (
			id,
			stock_id,
			reserved_by,
			reserved_at,
			createdAt,
			deletedAt
		) VALUES (
			$1, $2, $3, $4, $5, $6
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
			stockID,
			ticket.ReservedBy,
			ticket.ReservedAt,
			ticket.CreatedAt,
			nil,
		)
		if err != nil {
			return nil, err
		}

		ids = append(ids, ticket.ID)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return ids, nil
}

func (repository *ticketRepository) FindTicketByID(id uuid.UUID) (*domain.Ticket, error) {
	tDomain := domain.Ticket{}
	var createdAt sql.NullString
	var reservedAt sql.NullString
	var deletedAt sql.NullString

	query := `SELECT id, stock_id, reserved_by, reserved_at, createdAt, deletedAt FROM tickets WHERE id = $1 AND deletedAt IS NULL`

	err := repository.db.QueryRow(query, id).Scan(&tDomain.ID, &tDomain.StockID, &tDomain.ReservedBy, &reservedAt, &createdAt, &deletedAt)
	if err != nil {
		return &domain.Ticket{}, err
	}

	parsedReservedAt, err := helpers.ParseSqlDate(reservedAt)
	if err != nil {
		return &domain.Ticket{}, err
	}

	tDomain.ReservedAt = parsedReservedAt

	parsedCreatedAt, err := helpers.ParseSqlDate(createdAt)
	if err != nil {
		return &domain.Ticket{}, err
	}

	tDomain.CreatedAt = parsedCreatedAt

	parsedDeletedAt, err := helpers.ParseSqlDate(deletedAt)
	if err != nil {
		return &domain.Ticket{}, err
	}

	tDomain.DeletedAt = parsedDeletedAt

	return &tDomain, nil
}

func (repository *ticketRepository) Delete(id uuid.UUID) (uuid.UUID, error) {
	var pk uuid.UUID

	query := `UPDATE tickets SET deletedAt = $2 WHERE id = $1 RETURNING id`

	err := repository.db.QueryRow(query, id, time.Now()).Scan(&pk)
	if err != nil {
		return uuid.Nil, err
	}

	return pk, nil
}
