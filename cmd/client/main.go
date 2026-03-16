package main

import (
	"bufio"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/luxor/hashworker/internal/submission"
)

//go:embed index.html
var indexHTML []byte

// ── shared types ─────────────────────────────────────────────────────────────

type Message struct {
	ID     *int            `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type Response struct {
	ID     *int   `json:"id"`
	Result any    `json:"result"`
	Error  string `json:"error,omitempty"`
}

type jobParams struct {
	JobID       int    `json:"job_id"`
	ServerNonce string `json:"server_nonce"`
}

func randomNonce() string {
	b := make([]byte, 8)
	rand.Read(b) //nolint:errcheck
	return hex.EncodeToString(b)
}

func writeMsg(conn net.Conn, v any) {
	data, _ := json.Marshal(v)
	data = append(data, '\n')
	conn.Write(data) //nolint:errcheck
}

// ── CLI mode ──────────────────────────────────────────────────────────────────

func runCLI(serverAddr, username string) {
	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer conn.Close()
	log.Printf("connected to %s as %s", serverAddr, username)

	writeMsg(conn, map[string]any{
		"id":     1,
		"method": "authorize",
		"params": map[string]string{"username": username},
	})

	scanner := bufio.NewScanner(conn)

	if scanner.Scan() {
		var resp Response
		if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
			log.Fatalf("auth response parse: %v", err)
		}
		if resp.Error != "" {
			log.Fatalf("auth failed: %s", resp.Error)
		}
		log.Printf("authenticated as %s", username)
	}

	var (
		currentJobID int
		currentNonce string
		lastSubmit   time.Time
		msgID        = 2
		submitTicker = time.NewTicker(time.Second)
	)
	defer submitTicker.Stop()

	msgCh := make(chan []byte, 32)
	go func() {
		for scanner.Scan() {
			line := make([]byte, len(scanner.Bytes()))
			copy(line, scanner.Bytes())
			msgCh <- line
		}
		close(msgCh)
	}()

	for {
		select {
		case line, ok := <-msgCh:
			if !ok {
				log.Println("connection closed")
				return
			}
			var msg Message
			if err := json.Unmarshal(line, &msg); err != nil {
				continue
			}
			if msg.Method == "job" {
				var p jobParams
				if err := json.Unmarshal(msg.Params, &p); err == nil {
					currentJobID = p.JobID
					currentNonce = p.ServerNonce
					log.Printf("received job %d nonce=%s", currentJobID, currentNonce)
				}
			} else {
				var resp Response
				if err := json.Unmarshal(line, &resp); err == nil {
					if resp.Error != "" {
						log.Printf("submit error: %s", resp.Error)
					} else {
						log.Printf("submit accepted")
					}
				}
			}

		case <-submitTicker.C:
			if currentJobID == 0 || currentNonce == "" {
				continue
			}
			if !lastSubmit.IsZero() && time.Since(lastSubmit) < time.Second {
				continue
			}
			clientNonce := randomNonce()
			result := submission.CalcResult(currentNonce, clientNonce)
			writeMsg(conn, map[string]any{
				"id":     msgID,
				"method": "submit",
				"params": map[string]any{
					"job_id":       currentJobID,
					"client_nonce": clientNonce,
					"result":       result,
				},
			})
			lastSubmit = now()
			log.Printf("submitted job %d nonce=%s", currentJobID, clientNonce)
			msgID++
		}
	}
}

func now() time.Time { return time.Now() }

// ── Web mode ──────────────────────────────────────────────────────────────────

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// bridge connects a WebSocket client (browser) to a raw TCP server.
// Each browser session gets its own TCP connection, maintaining full isolation.
func bridge(ws *websocket.Conn, serverAddr string) {
	tcp, err := net.Dial("tcp", serverAddr)
	if err != nil {
		log.Printf("bridge: tcp connect: %v", err)
		ws.Close()
		return
	}
	defer tcp.Close()
	defer ws.Close()

	var wsMu sync.Mutex
	done := make(chan struct{})

	// TCP → WebSocket
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(tcp)
		for scanner.Scan() {
			line := make([]byte, len(scanner.Bytes()))
			copy(line, scanner.Bytes())
			wsMu.Lock()
			err := ws.WriteMessage(websocket.TextMessage, line)
			wsMu.Unlock()
			if err != nil {
				return
			}
		}
	}()

	// WebSocket → TCP
	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			break
		}
		msg = append(msg, '\n')
		if _, err := tcp.Write(msg); err != nil {
			break
		}
	}
	<-done
}

func runWeb(serverAddr, webAddr string) {
	mux := http.NewServeMux()

	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		ws, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("ws upgrade: %v", err)
			return
		}
		go bridge(ws, serverAddr)
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML) //nolint:errcheck
	})

	fmt.Printf("web UI → http://localhost%s\n", webAddr)
	if err := http.ListenAndServe(webAddr, mux); err != nil {
		log.Fatalf("http: %v", err)
	}
}

// ── entrypoint ────────────────────────────────────────────────────────────────

func main() {
	webAddr    := flag.String("web-addr", envOrDefault("WEB_ADDR", ":8081"), "web UI listen address")
	serverAddr := flag.String("server", envOrDefault("SERVER_ADDR", "localhost:8080"), "TCP server address")
	username   := flag.String("username", envOrDefault("USERNAME", "worker"), "worker username (CLI mode)")
	flag.Parse()

	// Always start the web UI in the background.
	go runWeb(*serverAddr, *webAddr)

	// Run the CLI worker in the foreground.
	runCLI(*serverAddr, *username)
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
