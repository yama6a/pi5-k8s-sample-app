// Command auditor is the sample-audit-logger binary: it logs every message on the user-audit-logger
// fanout exchange.
package main

import (
	"context"
	"os/signal"
	"syscall"

	"go.uber.org/zap"

	"github.com/yama6a/cluster-sampleapp/internal/messages"
	"github.com/yama6a/cluster-sampleapp/internal/mq"
)

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

	queue := messages.SubscriptionQueue(workload, messages.ExchangeUserAuditLogger)
	logger.Info("auditor started", zap.String("workload", workload), zap.String("queue", queue))

	mq.Consume(ctx, logger, uri, queue, workload, func(_ context.Context, _ string, body []byte) error {
		logger.Info("audit message received", zap.ByteString("message", body))
		return nil
	})
	logger.Info("shutting down")
}
