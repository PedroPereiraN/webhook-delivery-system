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
	ReserveMany(uuid.UUID, []*domain.Ticket, *domain.Outbox) ([]uuid.UUID, error)
	FindTicketByID(uuid.UUID) (*domain.Ticket, error)
	FindTicketsByIDs([]uuid.UUID) ([]*domain.Ticket, error)
	DeleteMany([]uuid.UUID, []*domain.Outbox) error
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

// ReserveMany reserva os tickets no stock, insere todos e grava o evento no
// outbox numa única transação: ou tudo é criado e o stock é atualizado, ou nada acontece.
func (repository *ticketRepository) ReserveMany(stockID uuid.UUID, tickets []*domain.Ticket, event *domain.Outbox) ([]uuid.UUID, error) {
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

// FindTicketsByIDs devolve os tickets não deletados entre os IDs informados.
// IDs inexistentes ou já deletados simplesmente não aparecem no resultado.
func (repository *ticketRepository) FindTicketsByIDs(ids []uuid.UUID) ([]*domain.Ticket, error) {
	query := `
		SELECT id, stock_id, reserved_by, reserved_at, createdAt
		FROM tickets
		WHERE id = ANY($1) AND deletedAt IS NULL
	`

	rows, err := repository.db.Query(query, pq.Array(helpers.UuidsToStrings(ids)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tickets := make([]*domain.Ticket, 0, len(ids))

	for rows.Next() {
		var ticket domain.Ticket
		var reservedAt sql.NullString
		var createdAt sql.NullString

		err := rows.Scan(&ticket.ID, &ticket.StockID, &ticket.ReservedBy, &reservedAt, &createdAt)
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

// DeleteMany faz o soft delete dos tickets, devolve as unidades reservadas para
// o stock e grava os eventos no outbox na mesma transação. Se algum ticket não
// puder ser deletado, nada é deletado.
func (repository *ticketRepository) DeleteMany(ids []uuid.UUID, events []*domain.Outbox) error {
	tx, err := repository.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// O "deletedAt IS NULL" impede que deletar o mesmo ticket duas vezes
	// libere a unidade duas vezes.
	ticketQuery := `
		UPDATE tickets
		SET deletedAt = $2
		WHERE id = ANY($1) AND deletedAt IS NULL
		RETURNING stock_id
	`

	rows, err := tx.Query(ticketQuery, pq.Array(helpers.UuidsToStrings(ids)), time.Now())
	if err != nil {
		return err
	}

	releasedByStock := map[uuid.UUID]int{}
	deleted := 0

	for rows.Next() {
		var stockID uuid.UUID

		if err := rows.Scan(&stockID); err != nil {
			rows.Close()
			return err
		}

		releasedByStock[stockID]++
		deleted++
	}

	rows.Close()

	if err := rows.Err(); err != nil {
		return err
	}

	// Outra requisição pode ter deletado algum ticket entre a validação no
	// service e aqui; nesse caso desfaz tudo.
	if deleted != len(ids) {
		return domain.ErrTicketNotFound
	}

	stockQuery := `
		UPDATE stocks
		SET
			reserved_quantity = reserved_quantity - $2,
			free_quantity = free_quantity + $2
		WHERE id = $1
	`

	for stockID, quantity := range releasedByStock {
		if _, err := tx.Exec(stockQuery, stockID, quantity); err != nil {
			return err
		}
	}

	for _, event := range events {
		if err := insertOutbox(tx, event); err != nil {
			return err
		}
	}

	return tx.Commit()
}
