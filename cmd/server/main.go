package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/lib/pq"

	"github.com/luxor/hashworker/internal/queue"
	"github.com/luxor/hashworker/internal/server"
	"github.com/luxor/hashworker/internal/stats"
)

func main() {
	addr := envOrDefault("LISTEN_ADDR", ":8080")
	dbURL := os.Getenv("DATABASE_URL")
	amqpURL := os.Getenv("AMQP_URL")

	var db *sql.DB
	if dbURL != "" {
		var err error
		db, err = sql.Open("postgres", dbURL)
		if err != nil {
			log.Fatalf("db open: %v", err)
		}
		if err := stats.Migrate(db); err != nil {
			log.Fatalf("migrate: %v", err)
		}
	}

	var pub *queue.Publisher
	if amqpURL != "" {
		var err error
		pub, err = queue.NewPublisher(amqpURL)
		if err != nil {
			log.Printf("rabbitmq publisher (skipping): %v", err)
		} else if db != nil {
			ctx := context.Background()
			if err := queue.StartConsumer(ctx, amqpURL, db); err != nil {
				log.Printf("rabbitmq consumer (skipping): %v", err)
			}
		}
	}

	srv, err := server.NewServer(addr, db, pub)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	fmt.Printf("listening on %s\n", srv.Addr())
	srv.Serve()
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
