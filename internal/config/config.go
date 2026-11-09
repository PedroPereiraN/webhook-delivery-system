package config

import (
	"os"
	"strconv"
)

const defaultDatabaseURL = "postgres://postgres:postgres@postgres:5432/db?sslmode=disable"

const defaultTicketCapacity = 100

// DatabaseURL é compartilhada pela api e pelo worker.
func DatabaseURL() string {
	if url := os.Getenv("DATABASE_URL"); url != "" {
		return url
	}

	return defaultDatabaseURL
}

// TicketCapacity é o total de tickets que podem estar reservados ao mesmo
// tempo. O disponível é calculado como capacidade - tickets ativos.
func TicketCapacity() int {
	capacity, err := strconv.Atoi(os.Getenv("TICKET_CAPACITY"))
	if err != nil || capacity <= 0 {
		return defaultTicketCapacity
	}

	return capacity
}
