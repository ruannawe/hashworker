package queue

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/luxor/hashworker/internal/stats"
)

const queueName = "submissions"

// Event is the payload published for each valid submission.
type Event struct {
	Username  string    `json:"username"`
	Timestamp time.Time `json:"timestamp"`
	JobID     int       `json:"job_id"`
}

// Publisher wraps a RabbitMQ channel for publishing submission events.
type Publisher struct {
	ch *amqp.Channel
	q  amqp.Queue
}

// NewPublisher creates a Publisher connected to the given AMQP URL.
func NewPublisher(url string) (*Publisher, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("amqp dial: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("amqp channel: %w", err)
	}
	q, err := ch.QueueDeclare(queueName, true, false, false, false, nil)
	if err != nil {
		return nil, fmt.Errorf("queue declare: %w", err)
	}
	return &Publisher{ch: ch, q: q}, nil
}

// Publish sends a submission event to the queue.
func (p *Publisher) Publish(ctx context.Context, e Event) error {
	body, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return p.ch.PublishWithContext(ctx, "", p.q.Name, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         body,
	})
}

// StartConsumer reads events from the queue and upserts them into the database.
// It blocks until ctx is cancelled.
func StartConsumer(ctx context.Context, url string, db *sql.DB) error {
	conn, err := amqp.Dial(url)
	if err != nil {
		return fmt.Errorf("amqp dial: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("amqp channel: %w", err)
	}
	q, err := ch.QueueDeclare(queueName, true, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("queue declare: %w", err)
	}
	msgs, err := ch.Consume(q.Name, "", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consume: %w", err)
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-msgs:
				if !ok {
					return
				}
				var e Event
				if err := json.Unmarshal(msg.Body, &e); err != nil {
					log.Printf("queue: bad message: %v", err)
					msg.Nack(false, false)
					continue
				}
				if err := stats.UpsertSubmission(db, e.Username, e.Timestamp); err != nil {
					log.Printf("queue: upsert: %v", err)
					msg.Nack(false, true) // requeue
					continue
				}
				msg.Ack(false)
			}
		}
	}()
	return nil
}
