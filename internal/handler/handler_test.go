package handler_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/zap"

	"github.com/yama6a/cluster-sampleapp/api"
	"github.com/yama6a/cluster-sampleapp/internal/handler"
	"github.com/yama6a/cluster-sampleapp/internal/store"
)

func startServer(t *testing.T) *httptest.Server {
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

	router := chi.NewRouter()
	api.HandlerFromMux(handler.NewServer(store.New(db), zap.NewNop()), router)

	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return srv
}

func TestGetHeaders(t *testing.T) {
	t.Parallel()

	srv := startServer(t)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/", nil)
	require.NoError(t, err)
	req.Header.Set("X-Custom-Header", "hello-world")

	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "text/plain")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	text := string(body)

	// The request header is echoed back.
	require.Contains(t, text, "X-Custom-Header: hello-world")

	// The bootstrap line is present and holds a valid RFC3339 UTC timestamp.
	const prefix = "Sample App Bootstrapped At: "
	idx := -1
	for i, line := range splitLines(text) {
		if len(line) > len(prefix) && line[:len(prefix)] == prefix {
			idx = i
			ts, perr := time.Parse(time.RFC3339Nano, line[len(prefix):])
			require.NoError(t, perr)
			require.Equal(t, time.UTC, ts.Location())
			require.WithinDuration(t, time.Now(), ts, time.Hour)
		}
	}
	require.GreaterOrEqual(t, idx, 0, "bootstrap line missing")
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
