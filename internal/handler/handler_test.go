package handler_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yama6a/pgsandbox"
	"go.uber.org/zap"

	"github.com/yama6a/cluster-sampleapp/api"
	"github.com/yama6a/cluster-sampleapp/data"
	"github.com/yama6a/cluster-sampleapp/internal/handler"
	"github.com/yama6a/cluster-sampleapp/internal/store"
)

// Same major as the CNPG cluster in offgrid-private.
const pgMajor = 16

func newStore(t *testing.T) *store.Store {
	t.Helper()

	sandbox := pgsandbox.New(t, pgMajor, pgsandbox.Migrate(migrationsKey(t), migrate))

	db, err := store.NewDB(t.Context(), sandbox.DSN)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	return store.New(db)
}

// migrate goes through store.Migrate rather than pgsandbox.Migrations, so the sql-migrate
// bookkeeping table is part of every clone, as it is in production.
func migrate(ctx context.Context, conn *pgx.Conn) error {
	db, err := store.NewDB(ctx, conn.Config().ConnString())
	if err != nil {
		return fmt.Errorf("open blueprint: %w", err)
	}
	defer func() { _ = db.Close() }()

	if err := store.Migrate(db, zap.NewNop()); err != nil {
		return fmt.Errorf("migrate blueprint: %w", err)
	}
	return nil
}

func migrationsKey(t *testing.T) string {
	t.Helper()

	h := sha256.New()
	err := fs.WalkDir(data.FS, "migrations", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, err := fs.ReadFile(data.FS, path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		h.Write([]byte(path))
		h.Write(body)
		return nil
	})
	require.NoError(t, err)

	return hex.EncodeToString(h.Sum(nil))[:16]
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
	// nil audit store: TestListUsers exercises only GET /users, which doesn't touch Redis. The audit
	// store has its own testcontainer-backed test in internal/audit.
	api.HandlerFromMux(handler.NewServer(st, nil, zap.NewNop()), router)
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
