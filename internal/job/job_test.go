package job

import (
	"encoding/hex"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// mockSession is a simple Recipient for tests.
type mockSession struct {
	mu            sync.Mutex
	authenticated bool
	notified      bool
	lastJobID     int
	lastNonce     string
}

func (m *mockSession) IsAuthenticated() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.authenticated
}

func (m *mockSession) NotifyJob(jobID int, nonce string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notified = true
	m.lastJobID = jobID
	m.lastNonce = nonce
}

// panicSession panics inside NotifyJob to simulate a dead connection.
type panicSession struct{}

func (p *panicSession) IsAuthenticated() bool { return true }
func (p *panicSession) NotifyJob(_ int, _ string) {
	panic("dead connection")
}

func TestNewNonce_IsRandom(t *testing.T) {
	n1, err1 := NewNonce()
	n2, err2 := NewNonce()
	assert.NoError(t, err1)
	assert.NoError(t, err2)
	assert.NotEqual(t, n1, n2)
}

func TestNewNonce_IsHex(t *testing.T) {
	n, err := NewNonce()
	assert.NoError(t, err)
	_, decodeErr := hex.DecodeString(n)
	assert.NoError(t, decodeErr, "nonce should be a valid hex string")
}

func TestJobID_Increments(t *testing.T) {
	m := NewManager()
	m.Broadcast()
	id1, _ := m.CurrentJob()
	m.Broadcast()
	id2, _ := m.CurrentJob()
	assert.Equal(t, 1, id1)
	assert.Equal(t, 2, id2)
}

func TestBroadcast_SendsToAllSessions(t *testing.T) {
	m := NewManager()
	sessions := make([]*mockSession, 3)
	for i := range sessions {
		s := &mockSession{authenticated: true}
		sessions[i] = s
		m.AddSession(fmt.Sprintf("s%d", i), s)
	}

	m.Broadcast()

	for i, s := range sessions {
		assert.True(t, s.notified, "session %d should have been notified", i)
		assert.Equal(t, 1, s.lastJobID)
		assert.NotEmpty(t, s.lastNonce)
	}
}

func TestBroadcast_SkipsUnauthenticated(t *testing.T) {
	m := NewManager()
	authed := &mockSession{authenticated: true}
	unauthed := &mockSession{authenticated: false}
	m.AddSession("authed", authed)
	m.AddSession("unauthed", unauthed)

	m.Broadcast()

	assert.True(t, authed.notified)
	assert.False(t, unauthed.notified)
}

func TestBroadcast_IgnoresDeadConnections(t *testing.T) {
	m := NewManager()
	m.AddSession("dead", &panicSession{})
	live := &mockSession{authenticated: true}
	m.AddSession("live", live)

	assert.NotPanics(t, func() { m.Broadcast() })
	assert.True(t, live.notified)
}
