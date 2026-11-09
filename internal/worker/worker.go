// Worker do transactional outbox: lê os eventos pendentes da tabela outbox e
// entrega cada um como webhook. A entrega é at-least-once, então o destino
// precisa ser idempotente pelo event_id.
package worker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/PedroPereiraN/webhook-delivery-system/internal/domain"
	"github.com/google/uuid"
)

type OutboxRepository interface {
	ClaimPending(limit int, lease time.Duration) ([]*domain.Outbox, error)
	MarkDelivered(id uuid.UUID, attempt int) error
	ScheduleRetry(id uuid.UUID, attempt int, delay time.Duration) error
	MarkFailed(id uuid.UUID, attempt int) error
}

type Config struct {
	WebhookURL string
	// Intervalo entre buscas quando o último lote não veio cheio.
	PollInterval time.Duration
	BatchSize    int
	// Timeout de cada POST. Precisa ser bem menor que o Lease, senão o
	// evento volta a ser elegível enquanto ainda está sendo entregue.
	RequestTimeout time.Duration
	Lease          time.Duration
	MaxAttempts    int
	BaseBackoff    time.Duration
	MaxBackoff     time.Duration
}

type Worker struct {
	repository OutboxRepository
	client     *http.Client
	config     Config
}

func New(repository OutboxRepository, config Config) *Worker {
	return &Worker{
		repository: repository,
		client:     &http.Client{Timeout: config.RequestTimeout},
		config:     config,
	}
}

// Run busca e entrega eventos até o ctx ser cancelado. Ao parar, termina de
// entregar o lote atual em vez de abandonar eventos no meio.
func (worker *Worker) Run(ctx context.Context) {
	for {
		claimed, err := worker.processBatch()
		if err != nil {
			log.Printf("claim failed: %v", err)
		}

		// Lote cheio: provavelmente tem mais eventos esperando, busca de novo
		// sem dormir.
		if err == nil && claimed == worker.config.BatchSize {
			if ctx.Err() != nil {
				return
			}

			continue
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(worker.config.PollInterval):
		}
	}
}

func (worker *Worker) processBatch() (int, error) {
	events, err := worker.repository.ClaimPending(worker.config.BatchSize, worker.config.Lease)
	if err != nil {
		return 0, err
	}

	var wg sync.WaitGroup

	for _, event := range events {
		wg.Go(func() {
			worker.handle(event)
		})
	}

	wg.Wait()

	return len(events), nil
}

func (worker *Worker) handle(event *domain.Outbox) {
	// Attempts já foi incrementado no ClaimPending. Passar do limite aqui
	// significa que tentativas anteriores morreram no meio (crash do worker),
	// então desiste sem tentar de novo.
	if event.Attempts > worker.config.MaxAttempts {
		log.Printf("[failed] event=%s attempts=%d exceeded max without a result", event.ID, event.Attempts)
		worker.updateStatus(event, worker.repository.MarkFailed(event.ID, event.Attempts))
		return
	}

	err := worker.deliver(event)
	if err == nil {
		log.Printf("[delivered] event=%s type=%s attempt=%d", event.ID, event.EventType, event.Attempts)
		worker.updateStatus(event, worker.repository.MarkDelivered(event.ID, event.Attempts))
		return
	}

	if !isRetryable(err) || event.Attempts >= worker.config.MaxAttempts {
		log.Printf("[failed] event=%s attempt=%d: %v", event.ID, event.Attempts, err)
		worker.updateStatus(event, worker.repository.MarkFailed(event.ID, event.Attempts))
		return
	}

	delay := worker.backoff(event.Attempts)
	log.Printf("[retry] event=%s attempt=%d in %s: %v", event.ID, event.Attempts, delay, err)
	worker.updateStatus(event, worker.repository.ScheduleRetry(event.ID, event.Attempts, delay))
}

// updateStatus só loga: se o update falhar o evento continua em processing e
// volta a ser elegível quando o lease expirar.
func (worker *Worker) updateStatus(event *domain.Outbox, err error) {
	if err != nil {
		log.Printf("could not update event=%s: %v", event.ID, err)
	}
}

type deliveryError struct {
	statusCode int
	err        error
}

func (e *deliveryError) Error() string {
	if e.err != nil {
		return e.err.Error()
	}

	return fmt.Sprintf("webhook responded %d", e.statusCode)
}

// isRetryable: erro de rede, timeout e 5xx podem passar numa próxima
// tentativa. 4xx é problema no próprio evento, exceto 408 e 429.
func isRetryable(err error) bool {
	deliveryErr, ok := err.(*deliveryError)
	if !ok || deliveryErr.statusCode == 0 {
		return true
	}

	code := deliveryErr.statusCode

	return code >= 500 || code == http.StatusRequestTimeout || code == http.StatusTooManyRequests
}

func (worker *Worker) deliver(event *domain.Outbox) error {
	// Não usa o ctx do Run: no shutdown a entrega em andamento termina (ou
	// estoura o RequestTimeout) em vez de ser cortada no meio.
	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		worker.config.WebhookURL,
		bytes.NewReader(event.Payload),
	)
	if err != nil {
		return &deliveryError{err: err}
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Event-ID", event.ID.String())
	request.Header.Set("X-Event-Type", string(event.EventType))
	request.Header.Set("X-Delivery-Attempt", strconv.Itoa(event.Attempts))

	response, err := worker.client.Do(request)
	if err != nil {
		return &deliveryError{err: err}
	}
	defer response.Body.Close()

	// Lê o corpo até o fim para a conexão poder ser reaproveitada.
	io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &deliveryError{statusCode: response.StatusCode}
	}

	return nil
}

// backoff exponencial com jitter: BaseBackoff * 2^(attempt-1), limitado a
// MaxBackoff, e sorteia entre metade e o valor cheio. O jitter evita que
// vários eventos que falharam juntos tentem de novo todos no mesmo instante.
func (worker *Worker) backoff(attempt int) time.Duration {
	limit := worker.config.MaxBackoff

	if attempt-1 < 32 {
		if exponential := worker.config.BaseBackoff << (attempt - 1); exponential > 0 && exponential < limit {
			limit = exponential
		}
	}

	half := limit / 2

	return half + time.Duration(rand.Int64N(int64(limit-half)+1))
}
