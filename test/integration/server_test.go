//go:build integration

package integration

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/luxor/hashworker/internal/server"
	"github.com/luxor/hashworker/internal/submission"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newSrv(t *testing.T) *server.Server {
	t.Helper()
	srv, err := server.NewServer(":0", nil, nil)
	require.NoError(t, err)
	go srv.Serve()
	t.Cleanup(func() { srv.Close() })
	return srv
}

type tcpClient struct {
	conn    net.Conn
	scanner *bufio.Scanner
	mu      sync.Mutex
	msgID   int
}

func dial(t *testing.T, addr string) *tcpClient {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	return &tcpClient{conn: conn, scanner: bufio.NewScanner(conn), msgID: 1}
}

func (c *tcpClient) send(v any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, _ := json.Marshal(v)
	data = append(data, '\n')
	c.conn.Write(data) //nolint:errcheck
}

func (c *tcpClient) recv(t *testing.T) map[string]any {
	t.Helper()
	c.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	defer c.conn.SetReadDeadline(time.Time{})
	require.True(t, c.scanner.Scan(), "expected a message")
	var m map[string]any
	require.NoError(t, json.Unmarshal(c.scanner.Bytes(), &m))
	return m
}

func (c *tcpClient) nextID() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.msgID
	c.msgID++
	return id
}

func (c *tcpClient) authorize(t *testing.T, username string) map[string]any {
	id := c.nextID()
	c.send(map[string]any{"id": id, "method": "authorize", "params": map[string]string{"username": username}})
	return c.recv(t)
}

func (c *tcpClient) submit(t *testing.T, jobID int, clientNonce, result string) map[string]any {
	id := c.nextID()
	c.send(map[string]any{
		"id":     id,
		"method": "submit",
		"params": map[string]any{"job_id": jobID, "client_nonce": clientNonce, "result": result},
	})
	return c.recv(t)
}

func (c *tcpClient) recvJob(t *testing.T) map[string]any {
	t.Helper()
	c.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	defer c.conn.SetReadDeadline(time.Time{})
	for c.scanner.Scan() {
		var m map[string]any
		require.NoError(t, json.Unmarshal(c.scanner.Bytes(), &m))
		if m["method"] == "job" {
			return m
		}
	}
	t.Fatal("did not receive job message")
	return nil
}

func jobFields(t *testing.T, msg map[string]any) (int, string) {
	t.Helper()
	params := msg["params"].(map[string]any)
	jobID := int(params["job_id"].(float64))
	nonce := params["server_nonce"].(string)
	return jobID, nonce
}

func TestFullFlow_AuthAndReceiveJob(t *testing.T) {
	srv := newSrv(t)
	c := dial(t, srv.Addr().String())

	resp := c.authorize(t, "alice")
	assert.Equal(t, true, resp["result"])

	srv.Manager.Broadcast()
	msg := c.recvJob(t)
	_, nonce := jobFields(t, msg)
	assert.NotEmpty(t, nonce)
}

func TestFullFlow_SubmitValid(t *testing.T) {
	srv := newSrv(t)
	c := dial(t, srv.Addr().String())
	c.authorize(t, "alice")

	srv.Manager.Broadcast()
	job := c.recvJob(t)
	jobID, serverNonce := jobFields(t, job)
	result := submission.CalcResult(serverNonce, "clientnonce")

	resp := c.submit(t, jobID, "clientnonce", result)
	assert.Equal(t, true, resp["result"])
	assert.Empty(t, resp["error"])
}

func TestFullFlow_SubmitInvalidHash(t *testing.T) {
	srv := newSrv(t)
	c := dial(t, srv.Addr().String())
	c.authorize(t, "alice")

	srv.Manager.Broadcast()
	job := c.recvJob(t)
	jobID, _ := jobFields(t, job)

	resp := c.submit(t, jobID, "clientnonce", "badhash")
	assert.Equal(t, false, resp["result"])
	assert.Equal(t, "Invalid result", resp["error"])
}

func TestFullFlow_SubmitUnknownJob(t *testing.T) {
	srv := newSrv(t)
	c := dial(t, srv.Addr().String())
	c.authorize(t, "alice")

	resp := c.submit(t, 99999, "nonce", "hash")
	assert.Equal(t, false, resp["result"])
	assert.Equal(t, "Task does not exist", resp["error"])
}

func TestFullFlow_RateLimit(t *testing.T) {
	srv := newSrv(t)
	c := dial(t, srv.Addr().String())
	c.authorize(t, "alice")

	srv.Manager.Broadcast()
	job := c.recvJob(t)
	jobID, serverNonce := jobFields(t, job)

	r1 := submission.CalcResult(serverNonce, "n1")
	resp1 := c.submit(t, jobID, "n1", r1)
	assert.Equal(t, true, resp1["result"])

	r2 := submission.CalcResult(serverNonce, "n2")
	resp2 := c.submit(t, jobID, "n2", r2)
	assert.Equal(t, false, resp2["result"])
	assert.Equal(t, "Submission too frequent", resp2["error"])
}

func TestFullFlow_DuplicateNonce(t *testing.T) {
	srv := newSrv(t)
	c := dial(t, srv.Addr().String())
	c.authorize(t, "alice")

	srv.Manager.Broadcast()
	job := c.recvJob(t)
	jobID, serverNonce := jobFields(t, job)

	r := submission.CalcResult(serverNonce, "n1")
	resp1 := c.submit(t, jobID, "n1", r)
	assert.Equal(t, true, resp1["result"])

	time.Sleep(1100 * time.Millisecond)
	resp2 := c.submit(t, jobID, "n1", r)
	assert.Equal(t, false, resp2["result"])
	assert.Equal(t, "Duplicate submission", resp2["error"])
}

func TestFullFlow_MultipleClients(t *testing.T) {
	srv := newSrv(t)
	addr := srv.Addr().String()

	const n = 5
	clients := make([]*tcpClient, n)
	for i := range clients {
		clients[i] = dial(t, addr)
		resp := clients[i].authorize(t, fmt.Sprintf("user%d", i))
		assert.Equal(t, true, resp["result"])
	}

	srv.Manager.Broadcast()

	jobIDs := make([]int, n)
	var wg sync.WaitGroup
	for i, cl := range clients {
		wg.Add(1)
		go func(idx int, c *tcpClient) {
			defer wg.Done()
			msg := c.recvJob(t)
			jobIDs[idx], _ = jobFields(t, msg)
		}(i, cl)
	}
	wg.Wait()

	for i := 1; i < n; i++ {
		assert.Equal(t, jobIDs[0], jobIDs[i], "all clients should receive the same job_id")
	}
}

func TestFullFlow_ClientDisconnect(t *testing.T) {
	srv := newSrv(t)
	addr := srv.Addr().String()

	c1 := dial(t, addr)
	c1.authorize(t, "alice")
	c1.conn.Close()

	time.Sleep(100 * time.Millisecond)
	c2 := dial(t, addr)
	resp := c2.authorize(t, "bob")
	assert.Equal(t, true, resp["result"])
}

func TestFullFlow_SubmitBeforeAuth(t *testing.T) {
	srv := newSrv(t)
	c := dial(t, srv.Addr().String())

	resp := c.submit(t, 1, "nonce", "hash")
	assert.Equal(t, false, resp["result"])
	assert.Equal(t, "not authorized", resp["error"])
}

func TestFullFlow_NewJobAfterTick(t *testing.T) {
	srv := newSrv(t)
	c := dial(t, srv.Addr().String())
	c.authorize(t, "alice")

	srv.Manager.Broadcast()
	job1 := c.recvJob(t)
	id1, _ := jobFields(t, job1)

	srv.Manager.Broadcast()
	job2 := c.recvJob(t)
	id2, _ := jobFields(t, job2)

	assert.Equal(t, id1+1, id2)
}
