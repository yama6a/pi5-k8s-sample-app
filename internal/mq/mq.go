// Package mq publishes to and consumes from RabbitMQ. It never declares exchanges or queues: the
// Messaging Topology Operator owns them, and a queue's properties cannot change once declared.
package mq

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"
)

// reconnectDelay is short, so a pod that starts before its queue or credentials exist recovers quickly.
const reconnectDelay = 5 * time.Second

// Env collects required environment variables, so startup fails once and names every missing one.
type Env struct{ missing []string }

// Require returns the value of key, recording it as missing when unset or empty.
func (e *Env) Require(key string) string {
	v := os.Getenv(key)
	if v == "" {
		e.missing = append(e.missing, key)
	}
	return v
}

// Err names every required variable that was unset or empty.
func (e *Env) Err() error {
	if len(e.missing) > 0 {
		return fmt.Errorf("missing required environment variables: %s", strings.Join(e.missing, ", "))
	}
	return nil
}

// URIFromEnv builds the AMQP URI from the RABBITMQ_* variables. Check e.Err before using it.
func URIFromEnv(e *Env) string {
	uri := url.URL{
		Scheme: "amqp",
		User:   url.UserPassword(e.Require("RABBITMQ_USERNAME"), e.Require("RABBITMQ_PASSWORD")), // escapes URL-special characters
		Host:   net.JoinHostPort(e.Require("RABBITMQ_HOST"), e.Require("RABBITMQ_PORT")),
		Path:   "/" + e.Require("RABBITMQ_VHOST"),
	}
	return uri.String()
}

// Publisher holds one connection and re-dials after a failure.
type Publisher struct {
	logger *zap.Logger
	uri    string

	mu   sync.Mutex
	conn *amqp.Connection
	ch   *amqp.Channel
}

// NewPublisher returns a Publisher that dials on the first Publish.
func NewPublisher(logger *zap.Logger, uri string) *Publisher {
	return &Publisher{logger: logger.With(zap.String("role", "publisher")), uri: uri}
}

// Publish sends body to an existing exchange, and drops the connection on failure so the next call re-dials.
func (p *Publisher) Publish(ctx context.Context, exchange, routingKey string, body []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.ch == nil {
		conn, ch, err := dial(p.uri)
		if err != nil {
			return err
		}
		p.conn, p.ch = conn, ch
		p.logger.Info("connected")
	}

	pubCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err := p.ch.PublishWithContext(pubCtx, exchange, routingKey, false, false, amqp.Publishing{
		ContentType: "application/json",
		Body:        body,
	})
	if err != nil {
		p.reset()
		return fmt.Errorf("publish to %q: %w", exchange, err)
	}
	p.logger.Info("published", zap.String("exchange", exchange), zap.String("routingKey", routingKey), zap.ByteString("message", body))
	return nil
}

// Close releases the connection.
func (p *Publisher) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reset()
}

// reset expects the caller to hold p.mu.
func (p *Publisher) reset() {
	if p.conn != nil {
		_ = p.conn.Close() // also closes p.ch
	}
	p.conn, p.ch = nil, nil
}

// Consume passes each message on an existing queue to handle until ctx is cancelled, and reconnects after any failure.
func Consume(ctx context.Context, logger *zap.Logger, uri, queue, consumerTag string, handle func(ctx context.Context, routingKey string, body []byte) error) {
	log := logger.With(zap.String("role", "consumer"), zap.String("queue", queue))
	for {
		if err := consumeLoop(ctx, log, uri, queue, consumerTag, handle); err != nil {
			log.Warn("consumer stopped, reconnecting", zap.Error(err), zap.Duration("retryIn", reconnectDelay))
		}
		if !sleep(ctx, reconnectDelay) {
			return
		}
	}
}

func consumeLoop(ctx context.Context, log *zap.Logger, uri, queue, consumerTag string, handle func(ctx context.Context, routingKey string, body []byte) error) error {
	conn, ch, err := dial(uri)
	if err != nil {
		return err
	}
	defer conn.Close()

	// autoAck: a message whose handler fails is lost. The demo shows routing, not delivery guarantees.
	deliveries, err := ch.Consume(queue, consumerTag, true /*autoAck*/, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consume %q: %w", queue, err)
	}
	log.Info("consumer connected")

	closed := make(chan *amqp.Error, 1)
	conn.NotifyClose(closed)

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-closed:
			return fmt.Errorf("connection closed: %w", err)
		case d, ok := <-deliveries:
			if !ok {
				return fmt.Errorf("delivery channel closed")
			}
			if err := handle(ctx, d.RoutingKey, d.Body); err != nil {
				log.Error("handle delivery", zap.String("routingKey", d.RoutingKey), zap.Error(err))
			}
		}
	}
}

func dial(uri string) (*amqp.Connection, *amqp.Channel, error) {
	conn, err := amqp.Dial(uri)
	if err != nil {
		return nil, nil, fmt.Errorf("dial: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("open channel: %w", err)
	}
	return conn, ch, nil
}

// sleep reports false when ctx is cancelled before d passes.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
