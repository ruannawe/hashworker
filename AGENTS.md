# Project Context — Luxor Backend Challenge

> Paste this entire file at the start of any AI conversation before asking for help.
> This ensures all devs get consistent answers aligned with the project's decisions.

---

## What We're Building

A **TCP message processing system** written in **Go**.
No front-end. All backend.

---

## Stack

| Layer     | Technology              |
|-----------|-------------------------|
| Language  | Go (required)           |
| Database  | PostgreSQL 17 (Docker)  |
| Queue     | RabbitMQ 4.0 (Docker)   |
| Protocol  | TCP with JSON           |

---

## Folder Structure

```
/
├── cmd/
│   ├── server/main.go        # TCP server entrypoint
│   └── client/main.go        # TCP client entrypoint
├── internal/
│   ├── auth/                 # Per-session authentication logic
│   ├── job/                  # Job generation, 30s tick and broadcast
│   ├── submission/           # Validation: SHA256, rate limit, dedup
│   ├── stats/                # Per-minute aggregation and DB writes
│   └── queue/                # RabbitMQ integration (bonus)
├── db/
│   └── migrations/           # SQL migration files
├── docker-compose.yaml
├── README.md
└── go.mod
```

---

## Architectural Decisions (already made — do not change)

### Concurrency
- **One goroutine per TCP connection** (idiomatic Go pattern)
- The server calls `go handleConnection(conn)` for each incoming client
- No worker pool, no artificial connection limits

### Rate Limiting
- **In-memory per session** using `time.Time` of the last submission
- Stored in the session struct, protected by a mutex
- Does not persist across restarts (acceptable for this challenge)

### Job Broadcast
- The server maintains an **active session registry** (map protected by `sync.RWMutex`)
- Every 30s, a separate goroutine iterates the registry and writes the new job to each `net.Conn`

### Tests
- **Integration tests** covering the full flow (real server + real client)
- **Unit tests** for critical functions: SHA256, rate limit, submission validation
- Use Go's standard `testing` package + `testify` for assertions

---

## Message Protocol (JSON over TCP)

Every message has an `id` (number) or `id: null` if no response is expected.
Messages are newline-delimited (`\n`).
Use `json.NewDecoder` with `bufio` for line-by-line reading.

### Suggested Go Structs

```go
// Generic incoming message
type Message struct {
    ID     *int            `json:"id"`
    Method string          `json:"method"`
    Params json.RawMessage `json:"params"`
}

// Generic outgoing response
type Response struct {
    ID     *int   `json:"id"`
    Result any    `json:"result"`
    Error  string `json:"error,omitempty"`
}
```

### 1. Authentication (Client → Server)

```json
{ "id": 1, "method": "authorize", "params": { "username": "admin" } }
```

Response:
```json
{ "id": 1, "result": true }
```

### 2. Job Distribution (Server → Client)

Sent every 30 seconds to all authenticated clients:

```json
{ "id": null, "method": "job", "params": { "job_id": 1, "server_nonce": "abc123" } }
```

- `server_nonce`: random string generated each tick (e.g. `hex(rand bytes)`)
- `job_id`: integer incremented each tick

### 3. Result Submission (Client → Server)

```json
{
  "id": 2,
  "method": "submit",
  "params": {
    "job_id": 1,
    "client_nonce": "xyz456",
    "result": "<sha256>"
  }
}
```

Success response:
```json
{ "id": 2, "result": true }
```

Error response:
```json
{ "id": 2, "result": false, "error": "mensagem de erro" }
```

---

## Critical Business Rules

### Server

- Maintain persistent and concurrent TCP connections
- Track `username` per session (authentication required before accepting submissions)
- Update `server_nonce` every **30 seconds** and increment `job_id` atomically
- Broadcast the new job to **all authenticated clients** immediately
- Maintain a `job_id → server_nonce` history per session (for detailed error messages)
- Validate submissions (see error table below)

### Client

- Single TCP connection per instance
- Authenticate upon connecting before any other operation
- On job receipt: generate a random `client_nonce` and compute `SHA256(server_nonce + client_nonce)`
- Submit between **1/minute (minimum)** and **1/second (maximum)**
- Multiple client instances must be able to run simultaneously (for concurrency testing)

---

## SHA256 Calculation

```
SHA256(server_nonce + client_nonce)
Exemplo: SHA256("123" + "456") = SHA256("123456")
```

**Order matters**: SHA256("123456") ≠ SHA256("654321")

Go implementation:
```go
import (
    "crypto/sha256"
    "fmt"
)

func calcResult(serverNonce, clientNonce string) string {
    h := sha256.Sum256([]byte(serverNonce + clientNonce))
    return fmt.Sprintf("%x", h)
}
```

---

## Error Table (server → client)

| Situation                       | `error` in response         |
|---------------------------------|-----------------------------|
| job_id does not exist           | `"Task does not exist"`     |
| job_id expired (old nonce)      | `"Task expired"`            |
| Wrong SHA256                    | `"Invalid result"`          |
| > 1 submission/second           | `"Submission too frequent"` |
| client_nonce already used       | `"Duplicate submission"`    |

---

## Suggested Session Struct

```go
type Session struct {
    conn      net.Conn
    username  string
    mu        sync.Mutex

    // Rate limiting
    lastSubmit time.Time

    // Anti-replay
    usedNonces map[string]struct{}

    // job_id → server_nonce history (for validation and detailed errors)
    jobHistory map[int]string
}
```

---

## Database Schema (PostgreSQL)

```sql
CREATE TABLE submissions (
    username          VARCHAR(255),
    timestamp         TIMESTAMP,
    submission_count  INT
);
```

- Data aggregated **per minute** (upsert: increment `submission_count` if a record already exists for that `username + minute`)
- Truncate timestamp to the minute: `DATE_TRUNC('minute', NOW())`

---

## RabbitMQ integration (bonus)

- Each valid submission publishes an event to the `submissions` queue
- Event payload:
```json
{ "username": "admin", "timestamp": "2024-01-01T00:00:00Z", "job_id": 1 }
```
- A separate consumer (goroutine in the same process) consumes the queue and upserts into the database
- Must tolerate server restarts without losing events (RabbitMQ persists messages)
- Without RabbitMQ: the server writes directly to the database synchronously (acceptable fallback)

---

## docker-compose.yaml

```yaml
services:
  rabbitmq:
    image: rabbitmq:4.0-management
    ports:
      - 5672:5672
      - 15672:15672   # UI: guest/guest
    volumes:
      - rabbitmq_data:/var/lib/rabbitmq

  postgres:
    image: postgres:17
    ports:
      - 5432:5432
    environment:
      POSTGRES_USER: luxor
      POSTGRES_PASSWORD: luxor
      POSTGRES_DB: luxor
    volumes:
      - postgres_data:/var/lib/postgresql/data

volumes:
  rabbitmq_data:
  postgres_data:
```

---

## Expected Go Dependencies

```
github.com/lib/pq                  # PostgreSQL driver
github.com/rabbitmq/amqp091-go     # RabbitMQ (bonus)
github.com/stretchr/testify        # test assertions
github.com/ory/dockertest/v3       # spins up a real PostgreSQL in integration tests
```

---

## Test Specification

### Folder Structure

Tests live **alongside the code** they test (Go convention):

```
/
├── internal/
│   ├── submission/
│   │   ├── submission.go
│   │   └── submission_test.go     # unit tests
│   ├── job/
│   │   ├── job.go
│   │   └── job_test.go            # unit tests
│   ├── auth/
│   │   ├── auth.go
│   │   └── auth_test.go           # unit tests
│   └── stats/
│       ├── stats.go
│       └── stats_test.go          # unit tests (with real DB via dockertest)
└── test/
    └── integration/
        └── server_test.go         # integration tests (real server + real client)
```

### How to Run

```bash
# Unit tests only (fast, no external dependencies)
go test ./internal/...

# Integration tests only (requires Docker)
go test ./test/integration/... -tags=integration

# Everything
go test ./...

# Verbose output
go test ./... -v

# With explicit timeout (integration tests can be slow)
go test ./test/integration/... -tags=integration -timeout 60s
```

> **Note:** integration tests use the build tag `//go:build integration` at the top of the file,
> so they won't run accidentally with `go test ./...` without the `-tags=integration` flag.

---

### Unit Tests — `internal/submission/submission_test.go`

Tests pure validation logic, no network or database.

| Test | Description |
|-------|-----------|
| `TestCalcResult_CorrectHash` | SHA256("123"+"456") == expected fixed value |
| `TestCalcResult_OrderMatters` | SHA256("123456") ≠ SHA256("654321") |
| `TestValidateSubmission_Valid` | valid submission returns `nil` |
| `TestValidateSubmission_InvalidJobID` | unknown job_id → `"Task does not exist"` |
| `TestValidateSubmission_ExpiredJobID` | job_id from old nonce → `"Task expired"` |
| `TestValidateSubmission_WrongResult` | wrong SHA256 → `"Invalid result"` |
| `TestValidateSubmission_RateLimit` | 2 submissions in <1s → `"Submission too frequent"` |
| `TestValidateSubmission_DuplicateNonce` | same client_nonce twice → `"Duplicate submission"` |
| `TestValidateSubmission_RateLimit_Resets` | after 1s, new submission is accepted |

---

### Unit Tests — `internal/job/job_test.go`

| Test | Description |
|-------|-----------|
| `TestNewNonce_IsRandom` | two consecutively generated nonces are different |
| `TestNewNonce_IsHex` | generated nonce is a valid hex string |
| `TestJobID_Increments` | job_id increments with each new job |
| `TestBroadcast_SendsToAllSessions` | broadcast writes the job to N mock connections |
| `TestBroadcast_SkipsUnauthenticated` | session without username does not receive the job |
| `TestBroadcast_IgnoresDeadConnections` | closed connection does not cause panic |

---

### Unit Tests — `internal/auth/auth_test.go`

| Test | Description |
|-------|-----------|
| `TestAuthorize_ValidUsername` | non-empty username → authenticated successfully |
| `TestAuthorize_EmptyUsername` | empty username → error |
| `TestAuthorize_AlreadyAuthorized` | second authorization on same session → error |

---

### Unit Tests — `internal/stats/stats_test.go`

Uses `dockertest` to spin up a real PostgreSQL container during the test.

| Test | Description |
|-------|-----------|
| `TestUpsertSubmission_FirstEntry` | first submission of the minute creates a record with count=1 |
| `TestUpsertSubmission_SameMinute` | second submission in the same minute increments count to 2 |
| `TestUpsertSubmission_NewMinute` | submission in a different minute creates a new record |
| `TestUpsertSubmission_MultipleUsers` | two users in the same minute create separate records |

---

### Integration Tests — `test/integration/server_test.go`

Starts a real TCP server on a random port and connects real clients.
Uses the build tag `//go:build integration`.

| Test | Description |
|-------|-----------|
| `TestFullFlow_AuthAndReceiveJob` | client connects, authenticates, receives job within 35s |
| `TestFullFlow_SubmitValid` | client submits correct result → `result: true` |
| `TestFullFlow_SubmitInvalidHash` | client submits wrong SHA256 → `"Invalid result"` |
| `TestFullFlow_SubmitUnknownJob` | client submits unknown job_id → `"Task does not exist"` |
| `TestFullFlow_RateLimit` | client submits 2x in <1s → `"Submission too frequent"` |
| `TestFullFlow_DuplicateNonce` | client reuses client_nonce → `"Duplicate submission"` |
| `TestFullFlow_MultipleClients` | 5 simultaneous clients all receive the same job_id |
| `TestFullFlow_ClientDisconnect` | client disconnects abruptly, server does not crash |
| `TestFullFlow_SubmitBeforeAuth` | client attempts submission without authenticating → connection rejected |
| `TestFullFlow_NewJobAfterTick` | after 30s, server sends job with incremented job_id |

---

## Non-functional Requirements

- Code must compile and run on **macOS or Linux**
- Multiple simultaneous clients (for concurrency testing)
- Submission includes: source code, tests, `README.md` with build and run instructions
- **No front-end**

---

## How to Use This File with AI

Before each coding session, paste this entire file + your specific question. Example:

```
[paste AGENTS.md here]

---

Question: Implement internal/job/job.go with the 30s ticker and broadcast to active sessions.
```

### Prompting Tips for Best Results

- Be specific about **which file** you want implemented
- Mention whether you want **tests included** or just the code
- If you get code that doesn't compile, paste the error and ask for a fix
- Ask for **one file/package at a time** — avoids large inconsistent responses
- If generated code diverges from the architectural decisions above, correct it explicitly
