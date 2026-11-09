package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ticketEvent struct {
	EventID    string    `json:"event_id"`
	EventType  string    `json:"event_type"`
	Email      string    `json:"email"`
	Quantity   int       `json:"quantity"`
	TicketIDs  []string  `json:"ticket_ids"`
	OccurredAt time.Time `json:"occurred_at"`
}

type sentEmail struct {
	To         string      `json:"to"`
	Subject    string      `json:"subject"`
	Event      ticketEvent `json:"event"`
	ReceivedAt time.Time   `json:"received_at"`
	// How many times the same event_id arrived, including retries following simulated
	// failures and duplicate deliveries.
	Deliveries int `json:"deliveries"`
}

type config struct {
	port        string
	secret      string
	tolerance   time.Duration
	failureRate float64
	slowRate    float64
	slowDelay   time.Duration
}

type server struct {
	config config

	mu     sync.Mutex
	emails map[string]*sentEmail
	order  []string
}

func main() {
	cfg := config{
		port:        envString("PORT", "8081"),
		secret:      os.Getenv("WEBHOOK_SECRET"),
		tolerance:   envDuration("SIGNATURE_TOLERANCE", 5*time.Minute),
		failureRate: envFloat("FAILURE_RATE", 0.3),
		slowRate:    envFloat("SLOW_RATE", 0.1),
		slowDelay:   envDuration("SLOW_DELAY", 15*time.Second),
	}

	if cfg.secret == "" {
		log.Fatal("WEBHOOK_SECRET is required to verify the webhooks")
	}

	s := &server{
		config: cfg,
		emails: map[string]*sentEmail{},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhook", s.handleWebhook)
	mux.HandleFunc("GET /emails", s.handleListEmails)

	log.Printf(
		"email-mock listening on :%s (failure_rate=%.2f slow_rate=%.2f slow_delay=%s)",
		cfg.port, cfg.failureRate, cfg.slowRate, cfg.slowDelay,
	)

	log.Fatal(http.ListenAndServe(":"+cfg.port, mux))
}

func (s *server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	// The signature covers the raw bytes, so read them before parsing.
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "could not read body", http.StatusBadRequest)
		return
	}

	if err := s.verifySignature(r.Header, body); err != nil {
		log.Printf("[rejected] %v", err)
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	var event ticketEvent

	if err := json.Unmarshal(body, &event); err != nil {
		// 4xx: invalid payload; retrying will not help.
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	if event.EventID == "" || event.Email == "" {
		http.Error(w, "event_id and email are required", http.StatusUnprocessableEntity)
		return
	}

	// Take your time before deciding on a response: if the worker times out, it
	// won't know whether the email was sent or not, and will end up resending it.
	if rand.Float64() < s.config.slowRate {
		log.Printf("[slow] event=%s sleeping %s", event.EventID, s.config.slowDelay)

		select {
		case <-time.After(s.config.slowDelay):
		case <-r.Context().Done():
			log.Printf("[slow] event=%s client gave up", event.EventID)
		}
	}

	if rand.Float64() < s.config.failureRate {
		log.Printf("[fail] event=%s returning 503", event.EventID)
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
		return
	}

	s.mu.Lock()
	email, duplicate := s.emails[event.EventID]
	if duplicate {
		email.Deliveries++
	} else {
		email = &sentEmail{
			To:         event.Email,
			Subject:    subjectFor(event),
			Event:      event,
			ReceivedAt: time.Now(),
			Deliveries: 1,
		}
		s.emails[event.EventID] = email
		s.order = append(s.order, event.EventID)
	}
	deliveries := email.Deliveries
	s.mu.Unlock()

	// Idempotency: a repeated event_id results in a 200 response without "sending" another
	// email, so the worker stops trying.
	if duplicate {
		log.Printf("[duplicate] event=%s deliveries=%d, email not sent again", event.EventID, deliveries)
	} else {
		log.Printf(
			"[sent] event=%s to=%s subject=%q tickets=%d",
			event.EventID, email.To, email.Subject, event.Quantity,
		)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"event_id":  event.EventID,
		"duplicate": duplicate,
	})
}

// verifySignature checks that the request was signed with our secret and is
// recent. The timestamp is inside the signed content, so an attacker cannot
// replay an old captured request with a new timestamp.
func (s *server) verifySignature(header http.Header, body []byte) error {
	timestamp := header.Get("X-Webhook-Timestamp")
	signature, ok := strings.CutPrefix(header.Get("X-Webhook-Signature"), "sha256=")

	if timestamp == "" || !ok {
		return errors.New("missing signature headers")
	}

	unix, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return errors.New("invalid timestamp")
	}

	if age := time.Since(time.Unix(unix, 0)); age > s.config.tolerance || age < -s.config.tolerance {
		return fmt.Errorf("timestamp outside tolerance (%s)", age.Round(time.Second))
	}

	got, err := hex.DecodeString(signature)
	if err != nil {
		return errors.New("invalid signature encoding")
	}

	mac := hmac.New(sha256.New, []byte(s.config.secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)

	// hmac.Equal takes the same time no matter where the bytes differ, so
	// response timing does not reveal how much of a forged signature is right.
	if !hmac.Equal(got, mac.Sum(nil)) {
		return errors.New("signature mismatch")
	}

	return nil
}

// handleListEmails displays "sent" emails, which is useful for checking what
// was sent without having to read through the logs.
func (s *server) handleListEmails(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	result := make([]sentEmail, 0, len(s.order))
	for _, id := range s.order {
		result = append(result, *s.emails[id])
	}
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func subjectFor(event ticketEvent) string {
	switch event.EventType {
	case "ticket.created":
		return "Your " + strconv.Itoa(event.Quantity) + " ticket(s) are reserved"
	case "ticket.deleted":
		return "Your " + strconv.Itoa(event.Quantity) + " ticket(s) were cancelled"
	default:
		return "Ticket update"
	}
}

func envString(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}

func envFloat(key string, fallback float64) float64 {
	value, err := strconv.ParseFloat(os.Getenv(key), 64)
	if err != nil {
		return fallback
	}

	return value
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value, err := time.ParseDuration(os.Getenv(key))
	if err != nil {
		return fallback
	}

	return value
}
