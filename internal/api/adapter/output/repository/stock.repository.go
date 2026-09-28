package repository

import (
	"database/sql"
	"time"

	"github.com/PedroPereiraN/webhook-delivery-system/internal/config"
)

func NewStockRepository(db *sql.DB) StockRepository {
	return &stockRepository{
		db: db,
	}
}

type StockRepository interface {
	CreateTable() error
	CreateDefaultStock() error
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
    created_at timestamp DEFAULT NOW(),
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
