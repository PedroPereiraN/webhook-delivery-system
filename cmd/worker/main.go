package main

import (
	"context"
	"database/sql"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/PedroPereiraN/webhook-delivery-system/internal/api/adapter/output/repository"
	"github.com/PedroPereiraN/webhook-delivery-system/internal/config"
	"github.com/PedroPereiraN/webhook-delivery-system/internal/worker"
	_ "github.com/lib/pq"
)

func main() {
	db, err := sql.Open("postgres", config.DatabaseURL())
	if err != nil {
		log.Fatal(err)
	}

	defer db.Close()

	if err = db.Ping(); err != nil {
		log.Fatal(err)
	}

	webhookURL := os.Getenv("WEBHOOK_URL")
	if webhookURL == "" {
		webhookURL = "http://email-mock:8081/webhook"
	}

	webhookSecret := os.Getenv("WEBHOOK_SECRET")
	if webhookSecret == "" {
		log.Fatal("WEBHOOK_SECRET is required to sign the webhooks")
	}

	// The table is created by the API. If the worker starts up before that,
	// ClaimPending fails, the error is logged, and it retries on the next poll.
	oRepository := repository.NewOutboxRepository(db)

	w := worker.New(oRepository, worker.Config{
		WebhookURL:     webhookURL,
		WebhookSecret:  webhookSecret,
		PollInterval:   2 * time.Second,
		BatchSize:      10,
		RequestTimeout: 10 * time.Second,
		Lease:          30 * time.Second,
		MaxAttempts:    8,
		BaseBackoff:    2 * time.Second,
		MaxBackoff:     5 * time.Minute,
	})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Printf("worker started, delivering to %s", webhookURL)

	w.Run(ctx)

	log.Println("worker stopped")
}
