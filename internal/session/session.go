// Package session tracks user sessions in the manager's durable Redis instance: one hash per user UUID
// plus a set of the currently-active ones. It mirrors internal/audit in shape (*FromEnv config, a
// NewClient that pings, a Store wrapping the client) but talks to the OTHER instance, the persistent
// one, so sessions survive a restart. See raspi-cluster docs/12_redis.md.
package session

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// keyPrefix namespaces the per-user session hashes (session:<uuid>).
	keyPrefix = "session:"
	// activeKey holds the UUIDs with an open session.
	activeKey = "sessions:active"
	// endedTTL keeps a closed session around for inspection while bounding growth: the instance runs
	// `noeviction`, so an unbounded keyspace would eventually fail writes rather than evict.
	endedTTL = 24 * time.Hour
)

// OptionsFromEnv builds go-redis options from the REDIS_SESSIONS_* vars, defaulting to the in-cluster
// sessions Service. No password, same as the cache: a CiliumNetworkPolicy is the access control.
func OptionsFromEnv() *redis.Options {
	return &redis.Options{
		Addr:     getenv("REDIS_SESSIONS_ADDR", "sample-user-manager-redis-sessions.sample-user-manager.svc.cluster.local:6379"),
		Password: os.Getenv("REDIS_SESSIONS_PASSWORD"),
	}
}

// getenv returns the value of key, or fallback when it is unset or empty.
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

// End closes uuid's session, drops it from the active set and puts the hash on the endedTTL clock.
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
