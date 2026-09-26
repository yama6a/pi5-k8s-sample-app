package audit_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/yama6a/cluster-sampleapp/internal/audit"
	"github.com/yama6a/cluster-sampleapp/internal/messages"
)

// newStore also returns the raw client, for assertions the Store does not expose, such as TTLs.
func newStore(t *testing.T) (*audit.Store, *redis.Client) {
	t.Helper()

	ctx := context.Background()

	container, err := tcredis.Run(ctx, "redis:7-alpine",
		testcontainers.WithWaitStrategy(
			wait.ForListeningPort("6379/tcp").WithStartupTimeout(60*time.Second),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	endpoint, err := container.ConnectionString(ctx)
	require.NoError(t, err)

	opts, err := redis.ParseURL(endpoint)
	require.NoError(t, err)

	rdb, err := audit.NewClient(ctx, opts)
	require.NoError(t, err)
	t.Cleanup(func() { _ = rdb.Close() })

	return audit.New(rdb), rdb
}

func entry(user, action string) messages.AuditLog {
	return messages.AuditLog{
		Service:   "sample-user-manager",
		Action:    action,
		UUID:      user,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
}

func TestRecordAndListAll(t *testing.T) {
	t.Parallel()

	st, _ := newStore(t)
	ctx := t.Context()

	userA := uuid.NewString()
	userB := uuid.NewString()

	// Two events for A, create then delete, and one for B. Each user keeps its events in insertion order.
	require.NoError(t, st.Record(ctx, entry(userA, messages.ActionUserCreated)))
	require.NoError(t, st.Record(ctx, entry(userA, messages.ActionUserDeleted)))
	require.NoError(t, st.Record(ctx, entry(userB, messages.ActionUserCreated)))

	all, err := st.ListAll(ctx)
	require.NoError(t, err)

	require.Len(t, all, 2)
	require.Len(t, all[userA], 2)
	require.Len(t, all[userB], 1)
	assert.Equal(t, messages.ActionUserCreated, all[userA][0].Action)
	assert.Equal(t, messages.ActionUserDeleted, all[userA][1].Action)
	assert.Equal(t, userB, all[userB][0].UUID)
}

func TestRecordSetsTTL(t *testing.T) {
	t.Parallel()

	st, rdb := newStore(t)
	ctx := t.Context()

	user := uuid.NewString()
	require.NoError(t, st.Record(ctx, entry(user, messages.ActionUserCreated)))

	// The list expires within ttl.
	ttl, err := rdb.TTL(ctx, "audit:"+user).Result()
	require.NoError(t, err)
	assert.Positive(t, ttl)
	assert.LessOrEqual(t, ttl, time.Hour)
}

func TestListAllEmpty(t *testing.T) {
	t.Parallel()

	st, _ := newStore(t)

	all, err := st.ListAll(t.Context())
	require.NoError(t, err)
	assert.Empty(t, all)
}
