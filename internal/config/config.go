package config

import "os"

const defaultDatabaseURL = "postgres://postgres:postgres@postgres:5432/db?sslmode=disable"

// DatabaseURL is shared by the api and the worker.
func DatabaseURL() string {
	if url := os.Getenv("DATABASE_URL"); url != "" {
		return url
	}

	return defaultDatabaseURL
}
