// Command auditor is the sample-audit-logger binary. It is a pure sink: it subscribes to the
// `user-audit-logger` fanout exchange and logs every audit message to stdout. No HTTP, no database,
// no publishing. Together with signup — which also subscribes to that fanout — it demonstrates
// fanout broadcast: one publisher (the manager), two independent consumers each with their own
// queue, both receiving every message. See raspi-cluster docs/11_messaging.md.
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

	// Connection + identity only (required). The queue name is derived from the workload name,
	// matching the topology library's <user>.<exchange> convention.
	env := &mq.Env{}
	uri := mq.URIFromEnv(env)
	workload := env.Require("WORKLOAD_NAME")
	if err := env.Err(); err != nil {
		logger.Fatal("messaging config", zap.Error(err))
	}

	queue := messages.SubscriptionQueue(workload, messages.ExchangeUserAuditLogger)
	logger.Info("auditor started", zap.String("workload", workload), zap.String("queue", queue))

	// Consume blocks (reconnecting) until ctx is cancelled.
	mq.Consume(ctx, logger, uri, queue, workload, func(_ context.Context, _ string, body []byte) error {
		logger.Info("audit message received", zap.ByteString("message", body))
		return nil
	})
	logger.Info("shutting down")
}
