// Command signup is the sample-user-signup binary. It simulates users signing up: every 10s it
// publishes a `create-user-command` (direct) with a fresh uuid + timestamp for the manager to
// persist. It also subscribes to the manager's outputs — `users.created` on the `user-events` topic
// (NOT users.deleted: its queue binds only that key) and every `user-audit-logger` fanout message —
// logging both on receipt. No HTTP, no database. See raspi-cluster docs/11_messaging.md.
package main

import (
	"context"
	"encoding/json"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/yama6a/cluster-sampleapp/internal/messages"
	"github.com/yama6a/cluster-sampleapp/internal/mq"
)

const publishInterval = 10 * time.Second

func main() {
	logger, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}
	defer func() { _ = logger.Sync() }()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Connection + identity only (required). Queue names are derived from the workload name, matching
	// the topology library's <user>.<exchange> convention — no per-queue env to misconfigure.
	env := &mq.Env{}
	uri := mq.URIFromEnv(env)
	workload := env.Require("WORKLOAD_NAME")
	if err := env.Err(); err != nil {
		logger.Fatal("messaging config", zap.Error(err))
	}

	publisher := mq.NewPublisher(logger, uri)
	defer publisher.Close()

	var wg sync.WaitGroup
	wg.Add(3)

	// Emit a create-user-command every publishInterval.
	go func() {
		defer wg.Done()
		signupLoop(ctx, logger, publisher)
	}()

	// Consume the manager's created events (only users.created reaches this queue) and log them.
	go func() {
		defer wg.Done()
		queue := messages.SubscriptionQueue(workload, messages.ExchangeUserEvents)
		mq.Consume(ctx, logger, uri, queue, workload, func(_ context.Context, routingKey string, body []byte) error {
			logger.Info("user event received", zap.String("routingKey", routingKey), zap.ByteString("message", body))
			return nil
		})
	}()

	// Consume every audit message (fanout) and log it.
	go func() {
		defer wg.Done()
		queue := messages.SubscriptionQueue(workload, messages.ExchangeUserAuditLogger)
		mq.Consume(ctx, logger, uri, queue, workload, func(_ context.Context, _ string, body []byte) error {
			logger.Info("audit message received", zap.ByteString("message", body))
			return nil
		})
	}()

	logger.Info("signup started", zap.String("workload", workload))
	<-ctx.Done()
	logger.Info("shutting down")
	wg.Wait()
}

// signupLoop publishes a create-user-command on a ticker until ctx is cancelled.
func signupLoop(ctx context.Context, logger *zap.Logger, pub *mq.Publisher) {
	ticker := time.NewTicker(publishInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			body, _ := json.Marshal(messages.CreateUserCommand{
				UUID:      uuid.NewString(),
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			})
			// Direct exchange: routing key == the exchange name (how the topology library binds the
			// single command queue). A failed publish is logged; the ticker will try again.
			if err := pub.Publish(ctx, messages.ExchangeCreateUserCommand, messages.ExchangeCreateUserCommand, body); err != nil {
				logger.Error("publish create-user-command", zap.Error(err))
			}
		}
	}
}
