# Webhook Delivery System

A small ticket reservation API used to study reliable webhook delivery. Every
time someone reserves or returns tickets, an event is recorded and a pool of
workers delivers it as a signed webhook to an email service (a local mock).

```
client ──► api ──(one transaction)──► postgres: tickets + outbox
                                                    │
                                          worker x3 │ claim events
                                                    ▼
                                         POST /webhook (signed)
                                                    │
                                                    ▼
                                               email-mock
```

- **Transactional outbox:** the ticket change and its event are written in the
  same transaction, so an event exists if and only if the change was committed.
- **Workers** claim events with `FOR UPDATE SKIP LOCKED`, so several replicas
  never take the same event. A claim is a lease: if a worker dies, the event
  becomes available again when the lease expires.
- **Retries** use exponential backoff with jitter. Network errors, timeouts,
  5xx, 408 and 429 are retried; other 4xx and exhausted attempts mark the event
  as `failed`.
- **At-least-once delivery:** a webhook can arrive more than once (for example
  after a timeout), so the receiver deduplicates by `event_id`.
- **Dead letters:** events that give up keep the reason of the failure and can
  be replayed through the API once the cause is fixed.
- **HMAC signatures:** every request is signed so the receiver can check it came
  from us, was not modified, and is not a replayed old request.

## Running

Requirements: Docker with Compose. Go 1.27+ is only needed for the load test.

```bash
docker compose up -d --build
```

This starts:

| Service      | Port | Description                                   |
|--------------|------|-----------------------------------------------|
| `api`        | 8080 | Ticket API                                    |
| `worker`     | -    | 3 replicas delivering the outbox events       |
| `email-mock` | 8081 | Fake email service that receives the webhooks |
| `postgres`   | 5432 | Database (`postgres` / `postgres`, db `db`)   |
| `pgadmin`    | 5050 | Postgres web UI, at http://localhost:5050     |

pgAdmin logs in with `admin@email.com` / `admin@123` by default. To change it,
copy `.env.example` to `.env` and edit the values. `.env` is not committed.
To connect to the database from pgAdmin, register a server with host
`postgres`, port `5432`, user `postgres` and password `postgres`.

There is no hot reload: after changing the code, run
`docker compose up -d --build` again. To stop everything:

```bash
docker compose down
```

## Using the API

Reserve tickets for an email:

```bash
curl -X POST localhost:8080/tickets \
  -H 'Content-Type: application/json' \
  -d '{"email": "ana@example.com", "quantity": 2}'
```

```json
{"message": "Tickets reserved successfully", "tickets": ["<id1>", "<id2>"]}
```

Get a ticket:

```bash
curl 'localhost:8080/tickets?id=<id1>'
```

Return tickets (all or nothing; one webhook is sent per email involved):

```bash
curl -X DELETE localhost:8080/tickets \
  -H 'Content-Type: application/json' \
  -d '{"ids": ["<id1>", "<id2>"]}'
```

See how many tickets are reserved and returned:

```bash
curl localhost:8080/stock
```

See the emails the mock "sent":

```bash
curl localhost:8081/emails
```

`deliveries` counts how many times the same event reached the mock; anything
above 1 is a duplicate the mock ignored. The mock keeps emails in memory, so
this list resets when it restarts.

## Logs

Workers and email mock together, following new lines:

```bash
docker compose logs -f worker email-mock
```

Only the workers (all replicas; each line is prefixed with `worker-1`,
`worker-2`, ...):

```bash
docker compose logs -f worker
```

Only the email mock:

```bash
docker compose logs -f email-mock
```

A single worker replica:

```bash
docker logs -f webhook-delivery-system-worker-2
```

Useful flags: `--tail 50` (only the last lines), `--since 10m` (only recent
lines), `--no-log-prefix` (hide the service name).

What the log tags mean:

| Service      | Tag           | Meaning                                                         |
|--------------|---------------|-----------------------------------------------------------------|
| `worker`     | `[delivered]` | Webhook answered 2xx                                            |
| `worker`     | `[retry]`     | Temporary failure, scheduled again after a backoff              |
| `worker`     | `[failed]`    | Permanent failure or attempts exhausted                         |
| `email-mock` | `[sent]`      | First time this event arrived: email "sent"                     |
| `email-mock` | `[duplicate]` | Event already received, answered 200 without sending again      |
| `email-mock` | `[fail]`      | Simulated failure (503)                                         |
| `email-mock` | `[slow]`      | Simulated slow answer, usually longer than the worker's timeout |
| `email-mock` | `[rejected]`  | Missing or invalid signature (401)                              |

Only problems:

```bash
docker compose logs -f worker email-mock | grep -E '\[(retry|failed|rejected)\]'
```

## Inspecting the outbox

```bash
docker compose exec postgres psql -U postgres -d db
```

```sql
-- How many events are in each state
SELECT status, COUNT(*), MAX(attempts) FROM outbox GROUP BY status;

-- Events waiting longer than expected (should be empty)
SELECT id, status, attempts, next_retry_at FROM outbox
WHERE status IN ('pending', 'processing') AND next_retry_at < NOW() - INTERVAL '1 minute';
```

An event goes through `pending → processing → delivered`, going back to
`pending` on each retry, or ending in `failed`.

## Dead letters

Events that end in `failed` are the dead letter queue: no worker picks them up
again, and they keep the reason of the last failure.

List them, most recent first (`limit` defaults to 100, max 500):

```bash
curl 'localhost:8080/dead-letters?limit=20'
```

```json
[{
  "id": "7e9377e0-...",
  "email": "ana@example.com",
  "eventType": "ticket.created",
  "attempts": 1,
  "lastError": "webhook responded 401: signature mismatch",
  "failedAt": "2026-11-09T04:02:21Z",
  "createdAt": "2026-11-09T04:02:21Z",
  "payload": {"event_id": "7e9377e0-...", "...": "..."}
}]
```

After fixing the cause, send an event back to the workers:

```bash
curl -X POST localhost:8080/dead-letters/<id>/replay
```

The event returns to `pending` with its attempts reset, so it gets the full
retry budget again. It keeps the same `event_id`, so if the receiver had
already processed it, it deduplicates instead of sending a second email.
Replaying without fixing the cause fails again: check `lastError` first (a
`401: signature mismatch` means the secrets differ, a `400` means the payload
itself is rejected). Replaying an event that is not `failed` returns 404.

## Load test

`cmd/loadtest` simulates many users with fake emails reserving and returning
tickets at the same time, then waits for the workers and checks that every
event became exactly one email in the mock. Run it with the stack up:

```bash
go run ./cmd/loadtest -users 30 -iterations 10
```

| Flag           | Default | Description                                      |
|----------------|---------|--------------------------------------------------|
| `-users`       | 20      | Concurrent users, each with its own email        |
| `-iterations`  | 10      | Reservations per user                            |
| `-max-qty`     | 3       | Max tickets per reservation                      |
| `-return-rate` | 0.5     | Chance of returning part of a reservation        |
| `-wait`        | 10m     | Max time to wait for the workers                 |
| `-verify`      | true    | Set to `false` to only generate load             |

To watch a worker crash being recovered, stop one replica during the test:

```bash
docker stop webhook-delivery-system-worker-2
```

Its claimed events are picked up by the other workers once the lease expires.

## Configuration

Set in `docker-compose.yaml`, except the pgAdmin login, which comes from `.env`.

| Variable              | Service             | Default                          | Description                                      |
|-----------------------|---------------------|----------------------------------|--------------------------------------------------|
| `DATABASE_URL`        | api, worker         | `postgres://...@postgres:5432/db`| Postgres connection string                       |
| `WEBHOOK_URL`         | worker              | `http://email-mock:8081/webhook` | Where webhooks are delivered                     |
| `WEBHOOK_SECRET`      | worker, email-mock  | required                         | Shared HMAC secret; must match on both sides     |
| `FAILURE_RATE`        | email-mock          | `0.3`                            | Fraction of requests answered with 503           |
| `SLOW_RATE`           | email-mock          | `0.1`                            | Fraction of requests answered slowly             |
| `SLOW_DELAY`          | email-mock          | `15s`                            | How slow those answers are                       |
| `SIGNATURE_TOLERANCE` | email-mock          | `5m`                             | Max age of a signed request                      |
| `PGADMIN_DEFAULT_EMAIL`    | pgadmin        | `admin@email.com`                | pgAdmin login, read from `.env`                  |
| `PGADMIN_DEFAULT_PASSWORD` | pgadmin        | `admin@123`                      | pgAdmin password, read from `.env`               |

Worker tuning (batch size, lease, timeout, max attempts, backoff) is in
`cmd/worker/main.go`.

## Webhook contract

```http
POST /webhook
Content-Type: application/json
X-Event-ID: 3f6c...
X-Event-Type: ticket.created
X-Delivery-Attempt: 1
X-Webhook-Timestamp: 1794195792
X-Webhook-Signature: sha256=<hex>
```

```json
{
  "event_id": "3f6c...",
  "event_type": "ticket.created",
  "email": "ana@example.com",
  "quantity": 2,
  "ticket_ids": ["<id1>", "<id2>"],
  "occurred_at": "2026-11-09T01:15:41Z"
}
```

`event_type` is `ticket.created` or `ticket.deleted`.

To verify a request, the receiver:

1. Reads the raw body, before parsing the JSON.
2. Computes `HMAC-SHA256(secret, "<X-Webhook-Timestamp>.<raw body>")` in hex.
3. Compares it with `X-Webhook-Signature` using a constant-time comparison.
4. Rejects timestamps older than the tolerance.
5. Ignores an `event_id` it has already processed.

`verifySignature` in `cmd/email-mock/main.go` is a reference implementation.

## Project layout

```
cmd/
  api/         HTTP API entrypoint
  worker/      Outbox worker entrypoint and its tuning
  email-mock/  Fake email service (receiver)
  loadtest/    Load test and verification
internal/
  api/         Controllers, services, ports and repositories (ports & adapters)
  domain/      Ticket, outbox event and payload
  worker/      Claim, deliver, sign, retry
  config/      Shared configuration
build/package/ Dockerfiles
```
