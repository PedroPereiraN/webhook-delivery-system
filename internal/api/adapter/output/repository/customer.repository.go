package repository

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/PedroPereiraN/webhook-delivery-system/internal/domain"
	"github.com/PedroPereiraN/webhook-delivery-system/internal/helpers"
	"github.com/google/uuid"
)

func NewCustomerRepository(db *sql.DB) CustomerRepository {
	return &customerRepository{
		db: db,
	}
}

type CustomerRepository interface {
	CreateTable() error
	Create(*domain.Customer) (uuid.UUID, error)
	FindCustomerByEmail(string) (*domain.Customer, error)
	FindCustomerById(uuid.UUID) (*domain.Customer, error)
	Update(uuid.UUID, *domain.Customer) (uuid.UUID, error)
	Delete(id uuid.UUID) (uuid.UUID, error)
}

type customerRepository struct {
	db *sql.DB
}

func (repository *customerRepository) CreateTable() error {
	query := `CREATE TABLE IF NOT EXISTS customers (
    id uuid UNIQUE PRIMARY KEY,
    name varchar(80) NOT NULL,
    email varchar(60) NOT NULL,
    createdAt timestamp DEFAULT NOW(),
    updatedAt timestamp,
    deletedAt timestamp
  )`

	_, err := repository.db.Exec(query)
	if err != nil {
		return err
	}

	return nil
}

func (repository *customerRepository) Create(dto *domain.Customer) (uuid.UUID, error) {
	query := `
		INSERT INTO customers (
			id,
			name,
			email,
			createdAt,
			updatedAt,
			deletedAt
		) VALUES (
			$1, $2, $3, $4, $5, $6
		) RETURNING id
	`

	var pk uuid.UUID

	err := repository.db.QueryRow(
		query,
		dto.ID,
		dto.Name,
		dto.Email,
		dto.CreatedAt,
		nil,
		nil,
	).Scan(&pk)
	if err != nil {
		return uuid.Nil, err
	}

	return pk, nil
}

func (repository *customerRepository) findCustomerBy(fieldCondition string, value any) (*domain.Customer, error) {
	const baseCustomerQuery = `
			SELECT
			    c.id,
			    c.name,
			    c.email,
			    c.createdAt,
			    c.updatedAt,
			    c.deletedAt
			FROM customers c
			WHERE %s AND c.deletedAt IS NULL
			`

	query := fmt.Sprintf(baseCustomerQuery, fieldCondition)

	var (
		customerID           uuid.UUID
		customerName         string
		customerEmail        string
		customerSqlCreatedAt sql.NullString
		customerSqlUpdatedAt sql.NullString
		customerSqlDeletedAt sql.NullString
	)

	err := repository.db.QueryRow(query, value).Scan(
		&customerID,
		&customerName,
		&customerEmail,
		&customerSqlCreatedAt,
		&customerSqlUpdatedAt,
		&customerSqlDeletedAt,
	)
	if err != nil {
		return nil, err
	}

	// Conversões de datas
	customerCreatedAt, err := helpers.ParseSqlDate(customerSqlCreatedAt)
	if err != nil {
		return nil, err
	}
	customerUpdatedAt, _ := helpers.ParseSqlDate(customerSqlUpdatedAt)
	customerDeletedAt, _ := helpers.ParseSqlDate(customerSqlDeletedAt)

	cDomain := &domain.Customer{
		ID:        customerID,
		Name:      customerName,
		Email:     customerEmail,
		CreatedAt: customerCreatedAt,
		UpdatedAt: customerUpdatedAt,
		DeletedAt: customerDeletedAt,
	}

	return cDomain, nil
}

func (repository *customerRepository) FindCustomerByEmail(email string) (*domain.Customer, error) {
	return repository.findCustomerBy("c.email = $1", email)
}

func (repository *customerRepository) FindCustomerById(id uuid.UUID) (*domain.Customer, error) {
	return repository.findCustomerBy("c.id = $1", id)
}

func (repository *customerRepository) Update(id uuid.UUID, dto *domain.Customer) (uuid.UUID, error) {
	setClauses := []string{}
	args := []any{id}
	argIndex := 2

	if dto.Name != "" {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argIndex))
		args = append(args, dto.Name)
		argIndex++
	}

	if dto.Email != "" {
		setClauses = append(setClauses, fmt.Sprintf("email = $%d", argIndex))
		args = append(args, dto.Email)
		argIndex++
	}

	// sempre atualiza o updatedAt
	setClauses = append(setClauses, fmt.Sprintf("updatedAt = $%d", argIndex))
	args = append(args, time.Now())

	// monta query
	query := fmt.Sprintf(`UPDATE customers SET %s WHERE id = $1 RETURNING id`, strings.Join(setClauses, ", "))

	var pk uuid.UUID

	err := repository.db.QueryRow(query, args...).Scan(&pk)
	if err != nil {
		return uuid.Nil, err
	}

	return pk, nil
}

func (repository *customerRepository) Delete(id uuid.UUID) (uuid.UUID, error) {
	var pk uuid.UUID

	query := `UPDATE customers SET deletedAt = $2 WHERE id = $1 RETURNING id`

	err := repository.db.QueryRow(query, id, time.Now()).Scan(&pk)
	if err != nil {
		return uuid.Nil, err
	}

	return pk, nil
}
