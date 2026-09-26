// Command manager is the sample-user-manager binary: it serves GET /users and GET /audit, and turns
// each create-user-command into a stored user, events, an audit message and a session.
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
	"github.com/yama6a/cluster-sampleapp/internal/audit"
	"github.com/yama6a/cluster-sampleapp/internal/handler"
	"github.com/yama6a/cluster-sampleapp/internal/messages"
	"github.com/yama6a/cluster-sampleapp/internal/mq"
	"github.com/yama6a/cluster-sampleapp/internal/session"
	"github.com/yama6a/cluster-sampleapp/internal/store"
)

// maxUsers is small, so the eviction path runs soon after start.
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

	rdb, err := audit.NewClient(connectCtx, audit.OptionsFromEnv())
	if err != nil {
		return fmt.Errorf("connect redis: %w", err)
	}
	defer func() { _ = rdb.Close() }()
	auditStore := audit.New(rdb)

	sdb, err := session.NewClient(connectCtx, session.OptionsFromEnv())
	if err != nil {
		return fmt.Errorf("connect sessions redis: %w", err)
	}
	defer func() { _ = sdb.Close() }()
	sessionStore := session.New(sdb)

	env := &mq.Env{}
	uri := mq.URIFromEnv(env)
	workload := env.Require("WORKLOAD_NAME")
	if err := env.Err(); err != nil {
		return fmt.Errorf("messaging config: %w", err)
	}

	publisher := mq.NewPublisher(logger, uri)
	defer publisher.Close()

	// Consume reconnects on its own, so a broker outage never stops the HTTP server.
	go mq.Consume(ctx, logger, uri, messages.ExchangeCreateUserCommand, workload,
		func(ctx context.Context, _ string, body []byte) error {
			return handleCreateUser(ctx, logger, st, auditStore, sessionStore, publisher, workload, body)
		})

	router := chi.NewRouter()
	router.Use(middleware.Logger)
	router.Use(middleware.Recoverer)
	api.HandlerFromMux(handler.NewServer(st, auditStore, logger), router)

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
		// Detach from the cancelled parent, so in-flight requests get the full grace period.
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

// handleCreateUser returns an error only for mq to log. With autoAck, a failed command is not redelivered.
func handleCreateUser(ctx context.Context, logger *zap.Logger, st *store.Store, auditStore *audit.Store, sessionStore *session.Store, pub *mq.Publisher, workload string, body []byte) error {
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
	recordAndPublishAudit(ctx, logger, auditStore, pub, workload, messages.ActionUserCreated, cmd.UUID)
	if err := sessionStore.Start(ctx, workload, cmd.UUID, createdAt); err != nil {
		logger.Error("start session", zap.String("uuid", cmd.UUID), zap.Error(err))
	}

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
		recordAndPublishAudit(ctx, logger, auditStore, pub, workload, messages.ActionUserDeleted, deleted.ID)
		publishUserEvent(ctx, logger, pub, messages.RoutingKeyUserDeleted, deleted.ID, deleted.CreatedAt.UTC().Format(time.RFC3339))
		if err := sessionStore.End(ctx, deleted.ID, time.Now().UTC()); err != nil {
			logger.Error("end session", zap.String("uuid", deleted.ID), zap.Error(err))
		}
	}

	if active, err := sessionStore.ActiveCount(ctx); err != nil {
		logger.Error("count active sessions", zap.Error(err))
	} else {
		logger.Info("active sessions", zap.Int64("count", active))
	}
	return nil
}

// publishUserEvent only logs a failure, because the user is already stored.
func publishUserEvent(ctx context.Context, logger *zap.Logger, pub *mq.Publisher, routingKey, uuid, timestamp string) {
	body, err := json.Marshal(messages.UserEvent{UUID: uuid, Timestamp: timestamp})
	if err != nil {
		logger.Error("marshal user event", zap.String("routingKey", routingKey), zap.Error(err))
		return
	}
	if err := pub.Publish(ctx, messages.ExchangeUserEvents, routingKey, body); err != nil {
		logger.Error("publish user event", zap.String("routingKey", routingKey), zap.Error(err))
	}
}

// recordAndPublishAudit writes the entry to the audit Redis and the fanout exchange. It only logs a
// failure, because the user is already stored.
func recordAndPublishAudit(ctx context.Context, logger *zap.Logger, auditStore *audit.Store, pub *mq.Publisher, workload, action, uuid string) {
	entry := messages.AuditLog{
		Service:   workload,
		Action:    action,
		UUID:      uuid,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	if err := auditStore.Record(ctx, entry); err != nil {
		logger.Error("record audit log", zap.String("action", action), zap.Error(err))
	}
	body, err := json.Marshal(entry)
	if err != nil {
		logger.Error("marshal audit log", zap.String("action", action), zap.Error(err))
		return
	}
	if err := pub.Publish(ctx, messages.ExchangeUserAuditLogger, "", body); err != nil {
		logger.Error("publish audit log", zap.String("action", action), zap.Error(err))
	}
}
