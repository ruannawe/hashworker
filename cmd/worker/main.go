package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"log"
	"net"
	"os"
	"time"

	"github.com/luxor/hashworker/internal/submission"
)

type message struct {
	ID     *int            `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type response struct {
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

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	serverAddr := flag.String("server", envOrDefault("SERVER_ADDR", "localhost:8080"), "TCP server address")
	flag.Parse()

	username := flag.Arg(0)
	if username == "" {
		log.Fatal("usage: worker [flags] <username>")
	}

	conn, err := net.Dial("tcp", *serverAddr)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer conn.Close()
	log.Printf("connected to %s as %s", *serverAddr, username)

	writeMsg(conn, map[string]any{
		"id":     1,
		"method": "authorize",
		"params": map[string]string{"username": username},
	})

	scanner := bufio.NewScanner(conn)

	if scanner.Scan() {
		var resp response
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
			var msg message
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
				var resp response
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
			lastSubmit = time.Now()
			log.Printf("submitted job %d nonce=%s", currentJobID, clientNonce)
			msgID++
		}
	}
}
