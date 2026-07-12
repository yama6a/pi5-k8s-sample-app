// Package audit persists the manager's audit events in Redis: one list per user UUID, each expiring an
// hour after that user's most recent activity. It mirrors internal/store (the Postgres store) in shape —
// *FromEnv config with defaulted, escaped env; a NewClient that pings; a Store wrapping the client — so the
// two backends read the same way. It backs GET /audit. See raspi-cluster docs/12_redis.md.
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
	// keyPrefix namespaces the per-user audit lists (audit:<uuid>).
	keyPrefix = "audit:"
	// ttl is how long a user's events live after their most recent activity (refreshed on every write).
	ttl = time.Hour
)

// OptionsFromEnv builds go-redis options from the REDIS_* vars, defaulting to the in-cluster
// sample-user-manager cache Service. There is NO password by default: the instance is locked to this
// workload by a CiliumNetworkPolicy (network-RBAC), not requirepass — see raspi-cluster docs/12_redis.md.
func OptionsFromEnv() *redis.Options {
	return &redis.Options{
		Addr:     getenv("REDIS_ADDR", "sample-user-manager-cache.sample-user-manager.svc.cluster.local:6379"),
		Password: os.Getenv("REDIS_PASSWORD"),
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

// Store persists audit events in Redis, one list per user UUID.
type Store struct {
	rdb *redis.Client
}

func New(rdb *redis.Client) *Store {
	return &Store{rdb: rdb}
}

// Record appends an audit event to the per-user list audit:<uuid> and (re)sets its TTL, so a user's
// events expire an hour after their most recent activity. RPUSH + EXPIRE run in one MULTI so the key can
// never be left without an expiry.
func (s *Store) Record(ctx context.Context, entry messages.AuditLog) error {
	payload, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal audit log: %w", err)
	}
	key := keyPrefix + entry.UUID

	pipe := s.rdb.TxPipeline()
	pipe.RPush(ctx, key, payload)
	pipe.Expire(ctx, key, ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("record audit log for %s: %w", entry.UUID, err)
	}
	return nil
}

// ListAll returns every user's audit events currently in Redis, keyed by user UUID. It SCANs the audit:*
// keyspace (cheap — the user table is capped at ~10) and LRANGEs each list. Keys expire on their own 1h
// TTL, so this only ever returns recently-active users.
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
