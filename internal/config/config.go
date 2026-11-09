package config

import "os"

const defaultDatabaseURL = "postgres://postgres:postgres@postgres:5432/db?sslmode=disable"

// DatabaseURL é compartilhada pela api e pelo worker.
func DatabaseURL() string {
	if url := os.Getenv("DATABASE_URL"); url != "" {
		return url
	}

	return defaultDatabaseURL
}
