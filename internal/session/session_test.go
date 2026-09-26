package session_test

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

	"github.com/yama6a/cluster-sampleapp/internal/session"
)

// newStore also returns the raw client, for assertions the Store does not expose, such as TTLs and hash fields.
func newStore(t *testing.T) (*session.Store, *redis.Client) {
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

	rdb, err := session.NewClient(ctx, opts)
	require.NoError(t, err)
	t.Cleanup(func() { _ = rdb.Close() })

	return session.New(rdb), rdb
}

func TestStartAndEnd(t *testing.T) {
	t.Parallel()

	st, rdb := newStore(t)
	ctx := t.Context()

	userA := uuid.NewString()
	userB := uuid.NewString()
	now := time.Now().UTC()

	require.NoError(t, st.Start(ctx, "sample-user-manager", userA, now))
	require.NoError(t, st.Start(ctx, "sample-user-manager", userB, now))

	active, err := st.ActiveCount(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(2), active)

	fields, err := rdb.HGetAll(ctx, "session:"+userA).Result()
	require.NoError(t, err)
	assert.Equal(t, "active", fields["state"])
	assert.Equal(t, userA, fields["uuid"])
	assert.Equal(t, now.Format(time.RFC3339), fields["startedAt"])

	// Ending drops A from the active set and puts its hash on the retention clock.
	require.NoError(t, st.End(ctx, userA, now.Add(time.Minute)))

	active, err = st.ActiveCount(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), active)

	fields, err = rdb.HGetAll(ctx, "session:"+userA).Result()
	require.NoError(t, err)
	assert.Equal(t, "ended", fields["state"])
	assert.Equal(t, now.Add(time.Minute).Format(time.RFC3339), fields["endedAt"])

	ttl, err := rdb.TTL(ctx, "session:"+userA).Result()
	require.NoError(t, err)
	assert.Positive(t, ttl)
	assert.LessOrEqual(t, ttl, 24*time.Hour)
}

func TestActiveSessionHasNoTTL(t *testing.T) {
	t.Parallel()

	st, rdb := newStore(t)
	ctx := t.Context()

	user := uuid.NewString()
	require.NoError(t, st.Start(ctx, "sample-user-manager", user, time.Now().UTC()))

	// -1 is redis for "key exists, no expiry": an open session must not vanish under the app.
	ttl, err := rdb.TTL(ctx, "session:"+user).Result()
	require.NoError(t, err)
	assert.Equal(t, time.Duration(-1), ttl)
}

func TestActiveCountEmpty(t *testing.T) {
	t.Parallel()

	st, _ := newStore(t)

	active, err := st.ActiveCount(t.Context())
	require.NoError(t, err)
	assert.Zero(t, active)
}
