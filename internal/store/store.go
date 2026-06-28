package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // register the "pgx" database/sql driver
	migrate "github.com/rubenv/sql-migrate"
	"go.uber.org/zap"

	"github.com/yama6a/cluster-sampleapp/data"
)

// defaultDSNTemplate is the in-cluster connection string. The password is taken
// from the PG_PASSWORD environment variable.
const defaultDSNTemplate = "postgresql://app:%s@cnpg-cluster-rw.databases.svc.cluster.local:5432/app"

// DSNFromEnv returns the connection string. DATABASE_URL wins when set (handy for
// local runs and tests); otherwise the in-cluster DSN is built from PG_PASSWORD.
func DSNFromEnv() string {
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		return dsn
	}
	return fmt.Sprintf(defaultDSNTemplate, os.Getenv("PG_PASSWORD"))
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
