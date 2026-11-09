// Transactional outbox worker: reads pending events from the outbox table and
// delivers each one as a webhook. Delivery is at-least-once, so the destination
// must be idempotent based on the event_id.
package worker

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PedroPereiraN/webhook-delivery-system/internal/domain"
	"github.com/google/uuid"
)

type OutboxRepository interface {
	ClaimPending(limit int, lease time.Duration) ([]*domain.Outbox, error)
	MarkDelivered(id uuid.UUID, attempt int) error
	ScheduleRetry(id uuid.UUID, attempt int, delay time.Duration) error
	MarkFailed(id uuid.UUID, attempt int, reason string) error
}

type Config struct {
	WebhookURL string
	// Shared with the receiver, which uses it to check that the request came
	// from us and that the body was not changed on the way.
	WebhookSecret string
	// Interval between searches when the last batch was not full.
	PollInterval time.Duration
	BatchSize    int
	// Timeout for each POST. It must be significantly shorter than the lease duration
	// otherwise, the event could become eligible again while it is still being delivered.
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

func (worker *Worker) Run(ctx context.Context) {
	for {
		claimed, err := worker.processBatch()
		if err != nil {
			log.Printf("claim failed: %v", err)
		}

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
	// 'Attempts' has already been incremented in ClaimPending. Exceeding the limit here
	// means that previous attempts failed midway (e.g., due to a worker crash),
	// so give up without trying again.
	if event.Attempts > worker.config.MaxAttempts {
		reason := fmt.Sprintf("exceeded %d attempts without a result", worker.config.MaxAttempts)
		log.Printf("[failed] event=%s attempts=%d: %s", event.ID, event.Attempts, reason)
		worker.updateStatus(event, worker.repository.MarkFailed(event.ID, event.Attempts, reason))
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
		worker.updateStatus(event, worker.repository.MarkFailed(event.ID, event.Attempts, err.Error()))
		return
	}

	delay := worker.backoff(event.Attempts)
	log.Printf("[retry] event=%s attempt=%d in %s: %v", event.ID, event.Attempts, delay, err)
	worker.updateStatus(event, worker.repository.ScheduleRetry(event.ID, event.Attempts, delay))
}

// updateStatus only logs: if the update fails, the event remains in 'processing'
// and becomes eligible again once the lease expires.
func (worker *Worker) updateStatus(event *domain.Outbox, err error) {
	if err != nil {
		log.Printf("could not update event=%s: %v", event.ID, err)
	}
}

type deliveryError struct {
	statusCode int
	// Start of the response body, so the dead letter says why the receiver
	// refused it (e.g. "signature mismatch") and not only the status code.
	body string
	err  error
}

func (e *deliveryError) Error() string {
	if e.err != nil {
		return e.err.Error()
	}

	if e.body != "" {
		return fmt.Sprintf("webhook responded %d: %s", e.statusCode, e.body)
	}

	return fmt.Sprintf("webhook responded %d", e.statusCode)
}

// isRetryable: network errors, timeouts, and 5xx status codes may succeed on a
// subsequent attempt. 4xx errors indicate an issue with the event itself,
// with the exception of 408 and 429.
func isRetryable(err error) bool {
	deliveryErr, ok := err.(*deliveryError)
	if !ok || deliveryErr.statusCode == 0 {
		return true
	}

	code := deliveryErr.statusCode

	return code >= 500 || code == http.StatusRequestTimeout || code == http.StatusTooManyRequests
}

func (worker *Worker) deliver(event *domain.Outbox) error {
	// Do not use the ctx from Run: during shutdown, an ongoing delivery is allowed to finish
	// (or hit the RequestTimeout) rather than being cut off mid-process.
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

	// Signed on every attempt, not stored in the outbox: the timestamp is part
	// of the signature and has to be fresh, or the receiver rejects it as a
	// replay.
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	request.Header.Set("X-Webhook-Timestamp", timestamp)
	request.Header.Set("X-Webhook-Signature", "sha256="+sign(worker.config.WebhookSecret, timestamp, event.Payload))

	response, err := worker.client.Do(request)
	if err != nil {
		return &deliveryError{err: err}
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(response.Body, 200))
		io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))

		return &deliveryError{
			statusCode: response.StatusCode,
			body:       strings.TrimSpace(string(snippet)),
		}
	}

	// Reads the body to the end so that the connection can be reused.
	io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))

	return nil
}

// sign returns the hex HMAC-SHA256 of "<timestamp>.<body>". It signs the exact
// bytes that go on the wire, so the receiver must verify the raw body before
// parsing it; re-encoding the JSON could change field order or spacing.
func sign(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)

	return hex.EncodeToString(mac.Sum(nil))
}

// Exponential backoff with jitter: BaseBackoff * 2^(attempt-1), capped at
// MaxBackoff, with a randomized value chosen between half and the full amount.
// Jitter prevents multiple events that failed simultaneously from all retrying
// at the exact same instant.
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
