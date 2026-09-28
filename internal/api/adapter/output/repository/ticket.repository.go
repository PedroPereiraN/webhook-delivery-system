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
		reserved_at timestamp
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

func (repository *ticketRepository) FindTicketByID(id uuid.UUID) (*domain.Ticket, error) {
	tDomain := domain.Ticket{}
	var createdAt sql.NullString
	var reservedAt sql.NullString
	var deletedAt sql.NullString

	query := `SELECT id, stock_id, reserved_by, reserved_at, createdAt, deletedAt FROM users WHERE id = $1 AND deletedAt IS NULL`

	err := repository.db.QueryRow(query, id).Scan(&tDomain.ID, &tDomain.StockID, &tDomain.ReservedBy, &tDomain.ReservedAt, &reservedAt, &createdAt, &deletedAt)
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
