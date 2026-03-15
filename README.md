# hashworker

TCP message-processing system written in Go (Luxor Backend Challenge).

## Stack

- **Go** — server and client
- **PostgreSQL 17** — per-minute submission aggregation
- **RabbitMQ 4.0** — event queue (bonus)
- **Protocol** — newline-delimited JSON over TCP

## Quick start

### 1. Start services

```bash
docker compose up -d
```

### 2. Run the server

```bash
# Without DB/queue (in-memory only):
go run ./cmd/server

# With PostgreSQL:
DATABASE_URL="postgres://luxor:luxor@localhost/luxor?sslmode=disable" go run ./cmd/server

# With PostgreSQL + RabbitMQ:
DATABASE_URL="postgres://luxor:luxor@localhost/luxor?sslmode=disable" \
AMQP_URL="amqp://guest:guest@localhost:5672/" \
go run ./cmd/server
```

Default listen address: `:8080`. Override with `LISTEN_ADDR`.

### 3. Run the client

```bash
go run ./cmd/client
```

Override with `SERVER_ADDR` and `USERNAME` env vars. Multiple clients can run simultaneously.

## Tests

```bash
# Unit tests (no external dependencies)
go test ./internal/...

# Integration tests (requires Docker)
go test ./test/integration/... -tags=integration -timeout 60s

# Everything
go test ./... -tags=integration -timeout 120s
```

## Project structure

```
internal/
  auth/         per-session authentication
  submission/   SHA256 validation, rate-limit, dedup
  job/          30s ticker, session registry, broadcast
  stats/        per-minute PostgreSQL upsert
  queue/        RabbitMQ producer/consumer (bonus)
  server/       TCP server wiring all packages together
cmd/
  server/       server entrypoint
  client/       client entrypoint
db/migrations/  SQL schema
test/integration/ integration tests (build tag: integration)
```
