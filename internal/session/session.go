// Package session tracks user sessions in the manager's persistent Redis instance, so they survive a restart.
package session

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	keyPrefix = "session:"
	activeKey = "sessions:active" // set of UUIDs with an open session
	// endedTTL bounds the keyspace. The instance runs noeviction: with full memory it refuses writes
	// instead of evicting keys.
	endedTTL = 24 * time.Hour
)

// OptionsFromEnv reads REDIS_SESSIONS_ADDR and REDIS_SESSIONS_PASSWORD, defaulting to the in-cluster sessions Service.
func OptionsFromEnv() *redis.Options {
	return &redis.Options{
		Addr:     getenv("REDIS_SESSIONS_ADDR", "sample-user-manager-redis-sessions.sample-user-manager.svc.cluster.local:6379"),
		Password: os.Getenv("REDIS_SESSIONS_PASSWORD"), // empty in-cluster: a CiliumNetworkPolicy gates the instance
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// NewClient opens a Redis client and verifies connectivity with a PING.
func NewClient(ctx context.Context, opts *redis.Options) (*redis.Client, error) {
	rdb := redis.NewClient(opts)
	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}
	return rdb, nil
}

// Store persists user sessions in Redis, one hash per user UUID.
type Store struct {
	rdb *redis.Client
}

func New(rdb *redis.Client) *Store {
	return &Store{rdb: rdb}
}

// Start opens uuid's session and adds it to the active set.
func (s *Store) Start(ctx context.Context, service, uuid string, at time.Time) error {
	key := keyPrefix + uuid

	pipe := s.rdb.TxPipeline()
	pipe.HSet(ctx, key,
		"uuid", uuid,
		"service", service,
		"state", "active",
		"startedAt", at.UTC().Format(time.RFC3339),
	)
	pipe.SAdd(ctx, activeKey, uuid)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("start session for %s: %w", uuid, err)
	}
	return nil
}

// End marks uuid's session ended, drops it from the active set and expires it after endedTTL.
func (s *Store) End(ctx context.Context, uuid string, at time.Time) error {
	key := keyPrefix + uuid

	pipe := s.rdb.TxPipeline()
	pipe.HSet(ctx, key,
		"state", "ended",
		"endedAt", at.UTC().Format(time.RFC3339),
	)
	pipe.SRem(ctx, activeKey, uuid)
	pipe.Expire(ctx, key, endedTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("end session for %s: %w", uuid, err)
	}
	return nil
}

// ActiveCount returns how many sessions are currently open.
func (s *Store) ActiveCount(ctx context.Context) (int64, error) {
	n, err := s.rdb.SCard(ctx, activeKey).Result()
	if err != nil {
		return 0, fmt.Errorf("count active sessions: %w", err)
	}
	return n, nil
}

// Close releases the underlying client.
func (s *Store) Close() error {
	if err := s.rdb.Close(); err != nil {
		return fmt.Errorf("close redis: %w", err)
	}
	return nil
}
