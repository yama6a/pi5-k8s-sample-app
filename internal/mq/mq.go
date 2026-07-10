// Package mq is the RabbitMQ (AMQP 0-9-1) transport shared by all three binaries (manager, signup,
// auditor). It only publishes and consumes — it never declares exchanges or queues. In the cluster
// the Messaging Topology Operator owns all topology (see raspi-cluster docs/11_messaging.md), and a
// queue's properties are immutable once declared, so declaring here would at best duplicate and at
// worst conflict. The Publisher and Consume both reconnect on failure, which also covers cold-start
// ordering (topology / generated credentials may not exist yet). Message shapes live in the
// internal/messages package; this package is bytes-in/bytes-out.
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

// reconnectDelay is how long the consumer loop waits before re-dialing after any failure. Kept
// short so a pod that starts before its topology/credentials are ready self-heals quickly.
const reconnectDelay = 5 * time.Second

// Env collects required environment-variable reads so a misconfigured process fails fast at
// startup reporting EVERY missing variable at once, rather than defaulting silently or failing one
// variable at a time. Read every var through Require, then check Err once.
type Env struct{ missing []string }

// Require returns the value of key, recording it as missing when unset or empty.
func (e *Env) Require(key string) string {
	v := os.Getenv(key)
	if v == "" {
		e.missing = append(e.missing, key)
	}
	return v
}

// Err returns a non-nil error naming every variable that was required but unset/empty.
func (e *Env) Err() error {
	if len(e.missing) > 0 {
		return fmt.Errorf("missing required environment variables: %s", strings.Join(e.missing, ", "))
	}
	return nil
}

// URIFromEnv assembles the AMQP URI from the required RABBITMQ_* connection vars (recorded on e;
// the caller checks e.Err before using the URI). Mirrors store.DSNFromEnv: the password is escaped
// via url.URL so a rotated password with URL-special characters can't malform the URI.
// Username/password come from the operator-generated <user>-user-credentials Secret.
func URIFromEnv(e *Env) string {
	uri := url.URL{
		Scheme: "amqp",
		User:   url.UserPassword(e.Require("RABBITMQ_USERNAME"), e.Require("RABBITMQ_PASSWORD")),
		Host:   net.JoinHostPort(e.Require("RABBITMQ_HOST"), e.Require("RABBITMQ_PORT")),
		Path:   "/" + e.Require("RABBITMQ_VHOST"),
	}
	return uri.String()
}

// Publisher publishes pre-marshalled messages to the broker, holding one connection/channel and
// re-dialing lazily. It is safe for sequential use from a single goroutine (the manager publishes
// from inside its command handler; signup publishes from a ticker) — a mutex guards the shared
// connection so a future concurrent caller is safe too.
type Publisher struct {
	logger *zap.Logger
	uri    string

	mu   sync.Mutex
	conn *amqp.Connection
	ch   *amqp.Channel
}

// NewPublisher returns a Publisher; it dials lazily on the first Publish.
func NewPublisher(logger *zap.Logger, uri string) *Publisher {
	return &Publisher{logger: logger.With(zap.String("role", "publisher")), uri: uri}
}

// Publish sends body to exchange with routingKey. On any failure it drops the connection so the
// next call re-dials, and returns the error for the caller to log. The exchange must already exist
// (declared by the topology operator).
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
		p.reset() // force a fresh dial next time
		return fmt.Errorf("publish to %q: %w", exchange, err)
	}
	p.logger.Info("published", zap.String("exchange", exchange), zap.String("routingKey", routingKey), zap.ByteString("message", body))
	return nil
}

// Close releases the connection. Safe to call once at shutdown.
func (p *Publisher) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reset()
}

// reset tears down the current connection; the caller holds p.mu.
func (p *Publisher) reset() {
	if p.conn != nil {
		_ = p.conn.Close() // also closes p.ch
	}
	p.conn, p.ch = nil, nil
}

// Consume delivers messages from queue to handle until ctx is cancelled, reconnecting on any
// failure (and never returning otherwise). autoAck is used: the teaching focus here is the
// exchange/routing topology, not delivery guarantees — manual ack would be the enhancement for
// at-least-once processing. handle errors are logged; the message is not redelivered (autoAck).
// The queue must already exist (declared by the topology operator).
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

	// The consumer tag ties log lines back to this workload; empty would auto-generate an opaque one.
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

// dial opens a connection and a channel, closing the connection if the channel fails.
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

// sleep waits for d or until ctx is cancelled. It returns false if ctx was cancelled (the caller
// should stop), true otherwise.
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
