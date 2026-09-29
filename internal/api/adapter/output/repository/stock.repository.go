package repository

import (
	"database/sql"
	"time"

	"github.com/PedroPereiraN/webhook-delivery-system/internal/config"
	"github.com/PedroPereiraN/webhook-delivery-system/internal/domain"
	"github.com/google/uuid"
)

func NewStockRepository(db *sql.DB) StockRepository {
	return &stockRepository{
		db: db,
	}
}

type StockRepository interface {
	CreateTable() error
	CreateDefaultStock() error
	FindStockById(uuid.UUID) (*domain.Stock, error)
	UpdateStockQuantity(uuid.UUID, int32) (uuid.UUID, error)
}

type stockRepository struct {
	db *sql.DB
}

func (repository *stockRepository) CreateTable() error {
	query := `CREATE TABLE IF NOT EXISTS stocks (
    id uuid UNIQUE PRIMARY KEY,
    name varchar(80) NOT NULL,
		total_quantity int NOT NULL,
		reserved_quantity int NOT NULL,
		free_quantity int NOT NULL,
    created_at timestamp DEFAULT NOW()
  )`

	_, err := repository.db.Exec(query)
	if err != nil {
		return err
	}

	return nil
}

func (repository *stockRepository) CreateDefaultStock() error {
	query := `
		INSERT INTO stocks (
			id,
			name,
			total_quantity,
			reserved_quantity,
			free_quantity,
			created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6
		) ON CONFLICT (id) DO NOTHING
	`

	_, err := repository.db.Exec(
		query,
		config.DefaultStockID,
		"default_stock",
		0,
		0,
		0,
		time.Now(),
	)
	if err != nil {
		return err
	}

	return nil
}

func (repository *stockRepository) FindStockById(id uuid.UUID) (*domain.Stock, error) {
	query := `
		SELECT
			s.id,
			s.name,
			s.total_quantity,
			s.reserved_quantity,
			s.free_quantity,
			s.created_at
		FROM stocks s
		WHERE s.id = $1
	`

	var stock domain.Stock

	err := repository.db.QueryRow(query, id).Scan(
		&stock.ID,
		&stock.Name,
		&stock.TotalQuantity,
		&stock.ReservedQuantity,
		&stock.FreeQuantity,
		&stock.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	return &stock, nil
}

func (repository *stockRepository) UpdateStockQuantity(id uuid.UUID, quantity int32) (uuid.UUID, error) {
	// quantity é um delta: positivo adiciona, negativo remove.
	// O WHERE impede que o total fique abaixo do que já está reservado.
	query := `
		UPDATE stocks
		SET
			total_quantity = total_quantity + $2,
			free_quantity = free_quantity + $2
		WHERE id = $1 AND total_quantity + $2 >= reserved_quantity
		RETURNING id
	`

	var pk uuid.UUID

	err := repository.db.QueryRow(query, id, quantity).Scan(&pk)
	if err != nil {
		return uuid.Nil, err
	}

	return pk, nil
}
