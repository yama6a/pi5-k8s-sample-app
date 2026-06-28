package store

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // register the "pgx" database/sql driver
	migrate "github.com/rubenv/sql-migrate"
	"go.uber.org/zap"

	"github.com/yama6a/cluster-sampleapp/data"
)

// DSNFromEnv assembles the Postgres connection string from the PG_* vars,
// defaulting to the in-cluster sample-workload CloudNativePG read-write Service.
// The password is escaped via url.URL, so a rotated password with URL-special
// characters can't malform the DSN.
func DSNFromEnv() string {
	dsn := url.URL{
		Scheme: "postgresql",
		User:   url.UserPassword(getenv("PG_USER", "app"), os.Getenv("PG_PASSWORD")),
		Host: net.JoinHostPort(
			getenv("PG_HOST", "sample-workload-cluster-rw.sample-workload.svc.cluster.local"),
			getenv("PG_PORT", "5432"),
		),
		Path: "/" + getenv("PG_DATABASE", "app"),
	}
	return dsn.String()
}

// getenv returns the value of key, or fallback when it is unset or empty.
func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func NewDB(ctx context.Context, dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return db, nil
}

func Migrate(db *sql.DB, logger *zap.Logger) error {
	src := migrate.EmbedFileSystemMigrationSource{
		FileSystem: data.FS,
		Root:       "migrations",
	}
	n, err := migrate.Exec(db, "postgres", src, migrate.Up)
	if err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	logger.Info("migrations applied", zap.Int("count", n))
	return nil
}

type Store struct {
	db *sql.DB
}

func New(db *sql.DB) *Store {
	return &Store{db: db}
}

// BootstrapTime returns the timestamp of the row seeded when the database was
// first migrated.
func (s *Store) BootstrapTime(ctx context.Context) (time.Time, error) {
	var t time.Time
	err := s.db.QueryRowContext(ctx, "SELECT created_at FROM sample ORDER BY created_at LIMIT 1").Scan(&t)
	if err != nil {
		return time.Time{}, fmt.Errorf("query bootstrap time: %w", err)
	}
	return t, nil
}
