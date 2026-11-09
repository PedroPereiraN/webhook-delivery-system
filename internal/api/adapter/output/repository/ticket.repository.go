package repository

import (
	"database/sql"
	"time"

	"github.com/PedroPereiraN/webhook-delivery-system/internal/domain"
	"github.com/PedroPereiraN/webhook-delivery-system/internal/helpers"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

// Chave do advisory lock que serializa as reservas. Qualquer número serve,
// só precisa ser o mesmo em todas as transações de reserva.
const reserveLockKey = 7_301_001

func NewTicketRepository(db *sql.DB) TicketRepository {
	return &ticketRepository{
		db: db,
	}
}

type TicketRepository interface {
	CreateTable() error
	ReserveMany(capacity int, tickets []*domain.Ticket, event *domain.Outbox) ([]uuid.UUID, error)
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

	// O count de tickets ativos roda em toda reserva.
	indexQuery := `CREATE INDEX IF NOT EXISTS idx_tickets_active
		ON tickets (id)
		WHERE deleted_at IS NULL`

	_, err = repository.db.Exec(indexQuery)
	if err != nil {
		return err
	}

	return nil
}

// ReserveMany confere se há tickets disponíveis, insere todos e grava o evento
// no outbox numa única transação: ou tudo é criado, ou nada acontece.
func (repository *ticketRepository) ReserveMany(capacity int, tickets []*domain.Ticket, event *domain.Outbox) ([]uuid.UUID, error) {
	tx, err := repository.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Sem uma linha de stock para travar com UPDATE, duas reservas
	// concorrentes poderiam contar os mesmos tickets livres e passar da
	// capacidade. O advisory lock faz uma reserva esperar a outra commitar;
	// ele é liberado sozinho no fim da transação.
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock($1)`, reserveLockKey); err != nil {
		return nil, err
	}

	var reserved int

	countQuery := `SELECT COUNT(*) FROM tickets WHERE deleted_at IS NULL`

	if err := tx.QueryRow(countQuery).Scan(&reserved); err != nil {
		return nil, err
	}

	quantity := len(tickets)

	if reserved+quantity > capacity {
		return nil, domain.ErrInsufficientStock
	}

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

// FindTicketsByIDs devolve os tickets não deletados entre os IDs informados.
// IDs inexistentes ou já deletados simplesmente não aparecem no resultado.
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

// DeleteMany faz o soft delete dos tickets e grava os eventos no outbox na
// mesma transação. Se algum ticket não puder ser deletado, nada é deletado.
// Não precisa do advisory lock: devolver só aumenta o disponível.
func (repository *ticketRepository) DeleteMany(ids []uuid.UUID, events []*domain.Outbox) error {
	tx, err := repository.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// O "deleted_at IS NULL" impede que devolver o mesmo ticket duas vezes
	// gere dois eventos.
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

	// Outra requisição pode ter deletado algum ticket entre a validação no
	// service e aqui; nesse caso desfaz tudo.
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

// CountTickets devolve quantos tickets estão reservados (ativos) e quantos
// foram devolvidos.
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
