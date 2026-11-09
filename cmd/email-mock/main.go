// Mock de um serviço externo de email. Recebe os webhooks do worker e finge
// que envia um email para o email do ticket. Ele falha e atrasa de propósito para
// exercitar o retry, o backoff e o timeout do worker.
//
// De propósito não importa nada de internal/: simula um serviço de terceiro,
// que só conhece o contrato JSON do webhook.
package main

import (
	"encoding/json"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"strconv"
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
	// Quantas vezes o mesmo event_id chegou, incluindo retries após falhas
	// simuladas e entregas duplicadas.
	Deliveries int `json:"deliveries"`
}

type config struct {
	port        string
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
		failureRate: envFloat("FAILURE_RATE", 0.3),
		slowRate:    envFloat("SLOW_RATE", 0.1),
		slowDelay:   envDuration("SLOW_DELAY", 15*time.Second),
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
	var event ticketEvent

	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		// 4xx: payload inválido não melhora com retry.
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	if event.EventID == "" || event.Email == "" {
		http.Error(w, "event_id and email are required", http.StatusUnprocessableEntity)
		return
	}

	// Lento antes de decidir a resposta: se o worker der timeout, ele não
	// sabe se o email foi enviado ou não, e vai reenviar.
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

	// Idempotência: um event_id repetido responde 200 sem "enviar" outro
	// email, assim o worker para de tentar.
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

// handleListEmails mostra os emails "enviados", útil para conferir o que
// chegou sem ficar lendo os logs.
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
