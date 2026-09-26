// Command signup is the sample-user-signup binary: it publishes a create-user-command on a fixed
// interval and logs the users.created events and audit messages the manager emits.
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

	go func() {
		defer wg.Done()
		signupLoop(ctx, logger, publisher)
	}()

	// The queue binds only users.created, so users.deleted never arrives here.
	go func() {
		defer wg.Done()
		queue := messages.SubscriptionQueue(workload, messages.ExchangeUserEvents)
		mq.Consume(ctx, logger, uri, queue, workload, func(_ context.Context, routingKey string, body []byte) error {
			logger.Info("user event received", zap.String("routingKey", routingKey), zap.ByteString("message", body))
			return nil
		})
	}()

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

func signupLoop(ctx context.Context, logger *zap.Logger, pub *mq.Publisher) {
	ticker := time.NewTicker(publishInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			body, err := json.Marshal(messages.CreateUserCommand{
				UUID:      uuid.NewString(),
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			})
			if err != nil {
				logger.Error("marshal create-user-command", zap.Error(err))
				continue
			}
			// The topology chart binds the command queue with the exchange name as its routing key.
			if err := pub.Publish(ctx, messages.ExchangeCreateUserCommand, messages.ExchangeCreateUserCommand, body); err != nil {
				logger.Error("publish create-user-command", zap.Error(err))
			}
		}
	}
}
