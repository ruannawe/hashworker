package server

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net"
	"sync"
	"time"

	"github.com/luxor/hashworker/internal/auth"
	"github.com/luxor/hashworker/internal/job"
	"github.com/luxor/hashworker/internal/queue"
	"github.com/luxor/hashworker/internal/stats"
	"github.com/luxor/hashworker/internal/submission"
)

// Message is a generic inbound JSON-RPC-style message.
type Message struct {
	ID     *int            `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// Response is a generic outbound reply.
type Response struct {
	ID     *int   `json:"id"`
	Result any    `json:"result"`
	Error  string `json:"error,omitempty"`
}

// Session holds all per-connection state.
type Session struct {
	conn net.Conn
	auth *auth.Auth
	mu   sync.Mutex
	vs   *submission.ValidationState
}

// IsAuthenticated implements job.Recipient.
func (s *Session) IsAuthenticated() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.auth.IsAuthorized()
}

// NotifyJob implements job.Recipient.
func (s *Session) NotifyJob(jobID int, serverNonce string) {
	s.mu.Lock()
	s.vs.JobHistory[jobID] = serverNonce
	s.vs.CurrentJobID = jobID
	s.mu.Unlock()

	type jobParams struct {
		JobID       int    `json:"job_id"`
		ServerNonce string `json:"server_nonce"`
	}
	type jobMsg struct {
		ID     *int      `json:"id"`
		Method string    `json:"method"`
		Params jobParams `json:"params"`
	}
	msg := jobMsg{Method: "job", Params: jobParams{JobID: jobID, ServerNonce: serverNonce}}
	data, _ := json.Marshal(msg)
	data = append(data, '\n')
	s.conn.Write(data) //nolint:errcheck
}

func writeResponse(conn net.Conn, resp Response) {
	data, _ := json.Marshal(resp)
	data = append(data, '\n')
	conn.Write(data) //nolint:errcheck
}

// Server is the top-level TCP server.
type Server struct {
	ln      net.Listener
	Manager *job.Manager
	done    chan struct{}
	DB      *sql.DB
	Pub     *queue.Publisher
}

// NewServer creates a Server listening on addr.
func NewServer(addr string, db *sql.DB, pub *queue.Publisher) (*Server, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	s := &Server{
		ln:      ln,
		Manager: job.NewManager(),
		done:    make(chan struct{}),
		DB:      db,
		Pub:     pub,
	}
	go s.Manager.Start(s.done)
	return s, nil
}

// Addr returns the listening address.
func (s *Server) Addr() net.Addr { return s.ln.Addr() }

// Close shuts down the server.
func (s *Server) Close() {
	close(s.done)
	s.ln.Close()
}

// Serve accepts connections until Close is called.
func (s *Server) Serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
				log.Printf("accept: %v", err)
				continue
			}
		}
		go s.handleConnection(conn)
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	sess := &Session{
		conn: conn,
		auth: &auth.Auth{},
		vs:   submission.NewValidationState(),
	}

	sessionKey := conn.RemoteAddr().String()
	s.Manager.AddSession(sessionKey, sess)
	defer s.Manager.RemoveSession(sessionKey)

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		var msg Message
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
		}
		s.dispatch(sess, &msg)
	}
}

func (s *Server) dispatch(sess *Session, msg *Message) {
	switch msg.Method {
	case "authorize":
		s.handleAuthorize(sess, msg)
	case "submit":
		s.handleSubmit(sess, msg)
	default:
		writeResponse(sess.conn, Response{ID: msg.ID, Error: "unknown method"})
	}
}

type authorizeParams struct {
	Username string `json:"username"`
}

func (s *Server) handleAuthorize(sess *Session, msg *Message) {
	var p authorizeParams
	if err := json.Unmarshal(msg.Params, &p); err != nil {
		writeResponse(sess.conn, Response{ID: msg.ID, Error: "invalid params"})
		return
	}
	sess.mu.Lock()
	err := sess.auth.Authorize(p.Username)
	sess.mu.Unlock()
	if err != nil {
		writeResponse(sess.conn, Response{ID: msg.ID, Result: false, Error: err.Error()})
		return
	}
	writeResponse(sess.conn, Response{ID: msg.ID, Result: true})
}

type submitParams struct {
	JobID       int    `json:"job_id"`
	ClientNonce string `json:"client_nonce"`
	Result      string `json:"result"`
}

func (s *Server) handleSubmit(sess *Session, msg *Message) {
	sess.mu.Lock()
	if !sess.auth.IsAuthorized() {
		sess.mu.Unlock()
		writeResponse(sess.conn, Response{ID: msg.ID, Result: false, Error: "not authorized"})
		return
	}
	sess.mu.Unlock()

	var p submitParams
	if err := json.Unmarshal(msg.Params, &p); err != nil {
		writeResponse(sess.conn, Response{ID: msg.ID, Result: false, Error: "invalid params"})
		return
	}

	sess.mu.Lock()
	err := submission.ValidateSubmission(sess.vs, p.JobID, p.ClientNonce, p.Result, time.Now())
	username := sess.auth.Username
	sess.mu.Unlock()

	if err != nil {
		writeResponse(sess.conn, Response{ID: msg.ID, Result: false, Error: err.Error()})
		return
	}

	s.recordSubmission(username, p.JobID)
	writeResponse(sess.conn, Response{ID: msg.ID, Result: true})
}

func (s *Server) recordSubmission(username string, jobID int) {
	now := time.Now()
	if s.Pub != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.Pub.Publish(ctx, queue.Event{Username: username, Timestamp: now, JobID: jobID}); err != nil {
			log.Printf("queue publish: %v", err)
		}
		return
	}
	if s.DB != nil {
		if err := stats.UpsertSubmission(s.DB, username, now); err != nil {
			log.Printf("db upsert: %v", err)
		}
	}
}
