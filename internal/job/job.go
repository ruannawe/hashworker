package job

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Recipient is implemented by every active session.
type Recipient interface {
	IsAuthenticated() bool
	NotifyJob(jobID int, serverNonce string)
}

// jobNotification is the wire format sent to clients.
type jobNotification struct {
	ID     *int            `json:"id"`
	Method string          `json:"method"`
	Params jobNotifParams  `json:"params"`
}

type jobNotifParams struct {
	JobID       int    `json:"job_id"`
	ServerNonce string `json:"server_nonce"`
}

// Manager owns the session registry and drives the periodic job broadcast.
type Manager struct {
	mu       sync.RWMutex
	sessions map[string]Recipient

	jobID atomic.Int64 // monotonically increasing

	currentMu   sync.RWMutex
	currentID   int
	currentNonce string
}

// NewManager creates a ready-to-use Manager.
func NewManager() *Manager {
	return &Manager{
		sessions: make(map[string]Recipient),
	}
}

// AddSession registers a session under the given key.
func (m *Manager) AddSession(id string, r Recipient) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[id] = r
}

// RemoveSession deregisters a session.
func (m *Manager) RemoveSession(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
}

// CurrentJob returns the latest job ID and server nonce.
func (m *Manager) CurrentJob() (jobID int, nonce string) {
	m.currentMu.RLock()
	defer m.currentMu.RUnlock()
	return m.currentID, m.currentNonce
}

// NewNonce generates a random 16-byte hex nonce.
func NewNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("rand: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// Broadcast generates a new job and sends it to all authenticated sessions.
func (m *Manager) Broadcast() {
	nonce, err := NewNonce()
	if err != nil {
		return
	}
	id := int(m.jobID.Add(1))

	m.currentMu.Lock()
	m.currentID = id
	m.currentNonce = nonce
	m.currentMu.Unlock()

	msg := jobNotification{
		ID:     nil,
		Method: "job",
		Params: jobNotifParams{JobID: id, ServerNonce: nonce},
	}
	data, _ := json.Marshal(msg)
	data = append(data, '\n')

	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, r := range m.sessions {
		if !r.IsAuthenticated() {
			continue
		}
		func() {
			defer func() { recover() }() // ignore panics from dead connections
			r.NotifyJob(id, nonce)
		}()
		_ = data // data is used by the server-level session, not here
	}
}

// Start runs the 30-second broadcast loop until done is closed.
func (m *Manager) Start(done <-chan struct{}) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.Broadcast()
		case <-done:
			return
		}
	}
}
