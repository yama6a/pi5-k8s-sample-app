// Package audit keeps the manager's audit events in Redis, one list per user, for GET /audit.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/yama6a/cluster-sampleapp/internal/messages"
)

const (
	keyPrefix = "audit:"
	ttl       = time.Hour // counts from the user's latest event
)

// OptionsFromEnv reads REDIS_ADDR and REDIS_PASSWORD, defaulting to the in-cluster cache Service.
func OptionsFromEnv() *redis.Options {
	return &redis.Options{
		Addr:     getenv("REDIS_ADDR", "sample-user-manager-cache.sample-user-manager.svc.cluster.local:6379"),
		Password: os.Getenv("REDIS_PASSWORD"), // empty in-cluster: a CiliumNetworkPolicy gates the instance
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

// Store persists audit events in Redis, one list per user UUID.
type Store struct {
	rdb *redis.Client
}

func New(rdb *redis.Client) *Store {
	return &Store{rdb: rdb}
}

// Record appends entry to its user's list and restarts the list's expiry.
func (s *Store) Record(ctx context.Context, entry messages.AuditLog) error {
	payload, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal audit log: %w", err)
	}
	key := keyPrefix + entry.UUID

	pipe := s.rdb.TxPipeline() // one MULTI, so no list is left without an expiry
	pipe.RPush(ctx, key, payload)
	pipe.Expire(ctx, key, ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("record audit log for %s: %w", entry.UUID, err)
	}
	return nil
}

// ListAll returns the audit events of every user active within ttl, keyed by user UUID.
func (s *Store) ListAll(ctx context.Context) (map[string][]messages.AuditLog, error) {
	out := make(map[string][]messages.AuditLog)

	var cursor uint64
	for {
		keys, next, err := s.rdb.Scan(ctx, cursor, keyPrefix+"*", 100).Result()
		if err != nil {
			return nil, fmt.Errorf("scan audit keys: %w", err)
		}
		for _, key := range keys {
			items, err := s.rdb.LRange(ctx, key, 0, -1).Result()
			if err != nil {
				return nil, fmt.Errorf("read audit list %q: %w", key, err)
			}
			events := make([]messages.AuditLog, 0, len(items))
			for _, item := range items {
				var entry messages.AuditLog
				if err := json.Unmarshal([]byte(item), &entry); err != nil {
					return nil, fmt.Errorf("unmarshal audit event: %w", err)
				}
				events = append(events, entry)
			}
			out[strings.TrimPrefix(key, keyPrefix)] = events
		}
		if next == 0 {
			break
		}
		cursor = next
	}
	return out, nil
}

// Close releases the underlying client.
func (s *Store) Close() error {
	if err := s.rdb.Close(); err != nil {
		return fmt.Errorf("close redis: %w", err)
	}
	return nil
}
