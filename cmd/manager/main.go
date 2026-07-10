// Command manager is the default binary of the sample-app image (sample-user-manager). It owns the
// user domain: an HTTP read model (GET /users) backed by Postgres, and the command/event hub of the
// messaging demo. It consumes the `create-user-command` (direct) and, per command, persists the user,
// emits `users.created` on the `user-events` topic + an audit message on the `user-audit-logger`
// fanout, and — once there are more than maxUsers — deletes the oldest user, emitting `users.deleted`
// + another audit message. See raspi-cluster docs/11_messaging.md.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"

	"github.com/yama6a/cluster-sampleapp/api"
	"github.com/yama6a/cluster-sampleapp/internal/handler"
	"github.com/yama6a/cluster-sampleapp/internal/messages"
	"github.com/yama6a/cluster-sampleapp/internal/mq"
	"github.com/yama6a/cluster-sampleapp/internal/store"
)

// maxUsers caps the table: after each insert, if the count exceeds this, the oldest user is evicted
// (and a users.deleted event + audit message are emitted). Small so the delete path is easy to see.
const maxUsers = 10

func main() {
	logger, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}
	defer func() { _ = logger.Sync() }()

	if err := run(logger); err != nil {
		logger.Fatal("startup failed", zap.Error(err))
	}
}

func run(logger *zap.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	db, err := store.NewDB(connectCtx, store.DSNFromEnv())
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer func() { _ = db.Close() }()

	if err := store.Migrate(db, logger); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}
	st := store.New(db)

	// Messaging config: connection + identity only (required — no silent defaults). The topology
	// names are compile-time constants in internal/messages, so they can't be misconfigured per-pod.
	env := &mq.Env{}
	uri := mq.URIFromEnv(env)
	workload := env.Require("WORKLOAD_NAME")
	if err := env.Err(); err != nil {
		return fmt.Errorf("messaging config: %w", err)
	}

	publisher := mq.NewPublisher(logger, uri)
	defer publisher.Close()

	// Consume the command topic and react. The handler owns the whole write path; it reconnects
	// internally and stops when ctx is cancelled, so a broker outage can't take down the HTTP server.
	go mq.Consume(ctx, logger, uri, messages.ExchangeCreateUserCommand, workload,
		func(ctx context.Context, _ string, body []byte) error {
			return handleCreateUser(ctx, logger, st, publisher, workload, body)
		})

	router := chi.NewRouter()
	router.Use(middleware.Logger)
	router.Use(middleware.Recoverer)
	api.HandlerFromMux(handler.NewServer(st, logger), router)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		// Detach from the now-cancelled parent so in-flight requests get the full grace period.
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	logger.Info("starting server", zap.String("addr", server.Addr))
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}

// handleCreateUser is the command handler: persist the user, announce it (event + audit), and evict
// the oldest if we're over the cap (announcing that too). A failed step returns an error so mq logs
// it; with autoAck the message isn't redelivered (fine for this demo).
func handleCreateUser(ctx context.Context, logger *zap.Logger, st *store.Store, pub *mq.Publisher, workload string, body []byte) error {
	var cmd messages.CreateUserCommand
	if err := json.Unmarshal(body, &cmd); err != nil {
		return fmt.Errorf("unmarshal command: %w", err)
	}
	createdAt, err := time.Parse(time.RFC3339, cmd.Timestamp)
	if err != nil {
		return fmt.Errorf("parse command timestamp %q: %w", cmd.Timestamp, err)
	}

	if err := st.CreateUser(ctx, cmd.UUID, createdAt); err != nil {
		return fmt.Errorf("persist user %s: %w", cmd.UUID, err)
	}
	logger.Info("user created", zap.String("uuid", cmd.UUID))
	publishUserEvent(ctx, logger, pub, messages.RoutingKeyUserCreated, cmd.UUID, cmd.Timestamp)
	publishAudit(ctx, logger, pub, workload, messages.ActionUserCreated, cmd.UUID)

	count, err := st.CountUsers(ctx)
	if err != nil {
		return fmt.Errorf("count users: %w", err)
	}
	if count > maxUsers {
		deleted, err := st.DeleteOldestUser(ctx)
		if err != nil {
			return fmt.Errorf("evict oldest user: %w", err)
		}
		logger.Info("user evicted", zap.String("uuid", deleted.ID), zap.Int("countBefore", count))
		publishAudit(ctx, logger, pub, workload, messages.ActionUserDeleted, deleted.ID)
		publishUserEvent(ctx, logger, pub, messages.RoutingKeyUserDeleted, deleted.ID, deleted.CreatedAt.UTC().Format(time.RFC3339))
	}
	return nil
}

// publishUserEvent emits a UserEvent to the user-events topic exchange with the given routing key.
// Publish failures are logged, not fatal: the DB write already succeeded and the loop reconnects.
func publishUserEvent(ctx context.Context, logger *zap.Logger, pub *mq.Publisher, routingKey, uuid, timestamp string) {
	body, _ := json.Marshal(messages.UserEvent{UUID: uuid, Timestamp: timestamp})
	if err := pub.Publish(ctx, messages.ExchangeUserEvents, routingKey, body); err != nil {
		logger.Error("publish user event", zap.String("routingKey", routingKey), zap.Error(err))
	}
}

// publishAudit broadcasts an AuditLog to the user-audit-logger fanout exchange (routing key ignored).
func publishAudit(ctx context.Context, logger *zap.Logger, pub *mq.Publisher, workload, action, uuid string) {
	body, _ := json.Marshal(messages.AuditLog{
		Service:   workload,
		Action:    action,
		UUID:      uuid,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
	if err := pub.Publish(ctx, messages.ExchangeUserAuditLogger, "", body); err != nil {
		logger.Error("publish audit log", zap.String("action", action), zap.Error(err))
	}
}
