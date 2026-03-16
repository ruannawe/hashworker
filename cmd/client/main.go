package main

import (
	"bufio"
	_ "embed"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/gorilla/websocket"
)

//go:embed index.html
var indexHTML []byte

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

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	webAddr    := flag.String("web-addr", envOrDefault("WEB_ADDR", ":8081"), "web UI listen address")
	serverAddr := flag.String("server", envOrDefault("SERVER_ADDR", "localhost:8080"), "TCP server address")
	flag.Parse()

	go runWeb(*serverAddr, *webAddr)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
}
