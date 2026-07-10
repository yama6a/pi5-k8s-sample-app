package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/zap"

	"github.com/yama6a/cluster-sampleapp/api"
	"github.com/yama6a/cluster-sampleapp/internal/handler"
	"github.com/yama6a/cluster-sampleapp/internal/store"
)

func newStore(t *testing.T) *store.Store {
	t.Helper()

	ctx := context.Background()

	container, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("app"),
		tcpostgres.WithUsername("app"),
		tcpostgres.WithPassword("secret"),
		testcontainers.WithWaitStrategy(
			wait.ForListeningPort("5432/tcp").WithStartupTimeout(60*time.Second),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	db, err := store.NewDB(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	require.NoError(t, store.Migrate(db, zap.NewNop()))
	return store.New(db)
}

func TestListUsers(t *testing.T) {
	t.Parallel()

	st := newStore(t)

	// Seed two users with distinct creation times so ordering (oldest first) is observable.
	older := uuid.NewString()
	newer := uuid.NewString()
	require.NoError(t, st.CreateUser(t.Context(), older, time.Now().Add(-time.Hour)))
	require.NoError(t, st.CreateUser(t.Context(), newer, time.Now()))

	router := chi.NewRouter()
	api.HandlerFromMux(handler.NewServer(st, zap.NewNop()), router)
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/users", nil)
	require.NoError(t, err)

	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "application/json")

	var users []store.User
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&users))
	require.Len(t, users, 2)

	// Oldest first, and every user has a UUID + a valid UTC timestamp.
	assert.Equal(t, older, users[0].ID)
	assert.Equal(t, newer, users[1].ID)
	for _, u := range users {
		_, perr := uuid.Parse(u.ID)
		require.NoError(t, perr)
		assert.False(t, u.CreatedAt.IsZero())
	}
}
