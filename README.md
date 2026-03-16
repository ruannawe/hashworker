# hashworker

TCP message-processing system written in Go (Luxor Backend Challenge).

## Stack

- **Go** — server and client
- **PostgreSQL 17** — per-minute submission aggregation
- **RabbitMQ 4.0** — event queue (bonus)
- **Protocol** — newline-delimited JSON over TCP

## Quick start

### 1. Start dependencies (PostgreSQL + RabbitMQ)

```bash
docker compose up -d postgres rabbitmq
```

### 2. Run the server

```bash
# In-memory only (no DB/queue):
go run ./cmd/server

# With PostgreSQL + RabbitMQ (reads from .env automatically):
go run ./cmd/server
```

| Env var        | Default     | Description              |
|----------------|-------------|--------------------------|
| `LISTEN_ADDR`  | `:8080`     | TCP listen address       |
| `DATABASE_URL` | —           | PostgreSQL connection URL |
| `AMQP_URL`     | —           | RabbitMQ connection URL  |

### 3. Run the web UI

```bash
go run ./cmd/client
```

Serves the browser interface at http://localhost:8081. All connections to the server are initiated from the browser — no background worker runs automatically.

| Flag/Env var              | Default          | Description           |
|---------------------------|------------------|-----------------------|
| `-server` / `SERVER_ADDR` | `localhost:8080` | TCP server address    |
| `-web-addr` / `WEB_ADDR`  | `:8081`          | Web UI listen address |

### 4. Run a CLI worker (optional)

```bash
go run ./cmd/worker <username>
```

Connects to the server as `<username>` and auto-submits SHA256 results every ~1s. Runs independently of the web UI.

```bash
# Example — multiple workers in separate terminals:
go run ./cmd/worker worker-1
go run ./cmd/worker worker-2
go run ./cmd/worker worker-3
```

| Flag/Env var              | Default          | Description        |
|---------------------------|------------------|--------------------|
| `-server` / `SERVER_ADDR` | `localhost:8080` | TCP server address |

### 5. Full stack via Docker Compose

```bash
docker compose up --build
```

- Server TCP → `localhost:8080`
- Web UI     → `http://localhost:8081`

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
cmd/
  server/       TCP server entrypoint (LISTEN_ADDR, DATABASE_URL, AMQP_URL)
  client/       Web bridge — serves the browser UI (go:embed index.html)
    index.html  Browser test interface (integration test button included)
  worker/       CLI mining worker — connects and auto-submits (run directly)
internal/
  auth/         Per-session authentication
  submission/   SHA256 validation, rate-limit, dedup
  job/          30s broadcaster, session registry
  stats/        Per-minute PostgreSQL upsert
  queue/        RabbitMQ producer/consumer (bonus)
  server/       TCP server, wires all packages together
db/migrations/  SQL schema
test/integration/ Integration tests (build tag: integration)
```

## Protocol (JSON over TCP)

All messages are newline-delimited JSON.

```
Client → Server   {"id":1,"method":"authorize","params":{"username":"worker"}}
Server → Client   {"id":1,"result":true}
Server → Client   {"id":null,"method":"job","params":{"job_id":1,"server_nonce":"abc..."}}
Client → Server   {"id":2,"method":"submit","params":{"job_id":1,"client_nonce":"xyz...","result":"sha256..."}}
Server → Client   {"id":2,"result":true}
```
