package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"github.com/luxor/hashworker/internal/submission"
)

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

func main() {
	addr := envOrDefault("SERVER_ADDR", "localhost:8080")
	username := envOrDefault("USERNAME", "worker")
	minInterval := time.Minute
	maxInterval := time.Second

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer conn.Close()
	log.Printf("connected to %s as %s", addr, username)

	// Authenticate.
	id1 := 1
	writeMsg(conn, map[string]any{
		"id":     id1,
		"method": "authorize",
		"params": map[string]string{"username": username},
	})

	scanner := bufio.NewScanner(conn)

	// Read auth response.
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
		currentJobID    int
		currentNonce    string
		lastSubmit      time.Time
		msgID           = 2
		submitTicker    = time.NewTicker(maxInterval)
	)
	defer submitTicker.Stop()

	msgCh := make(chan []byte, 32)

	// Reader goroutine: forward server messages to channel.
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
				// Submit response.
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
			// Respect minimum interval (1/minute) — always submit at tick rate
			// but honour the 1/s maximum implicitly via the ticker.
			now := time.Now()
			if !lastSubmit.IsZero() && now.Sub(lastSubmit) < minInterval {
				// Submit at most once per second, at least once per minute.
				// The ticker fires every second; skip if we already submitted
				// this second.
				if now.Sub(lastSubmit) < maxInterval {
					continue
				}
			}

			clientNonce := randomNonce()
			result := submission.CalcResult(currentNonce, clientNonce)
			id := msgID
			msgID++
			writeMsg(conn, map[string]any{
				"id":     id,
				"method": "submit",
				"params": map[string]any{
					"job_id":       currentJobID,
					"client_nonce": clientNonce,
					"result":       result,
				},
			})
			lastSubmit = now
			log.Printf("submitted job %d nonce=%s", currentJobID, clientNonce)

		}
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func init() {
	_ = fmt.Sprintf // avoid unused import
	_ = os.Stderr
}
