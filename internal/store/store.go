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

// User is a persisted user: its id and creation time both originate in the create-user-command.
type User struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
}

// CreateUser persists a user with the given id and creation time (both from the command payload).
func (s *Store) CreateUser(ctx context.Context, id string, createdAt time.Time) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO users (id, created_at) VALUES ($1, $2)", id, createdAt)
	if err != nil {
		return fmt.Errorf("insert user: %w", err)
	}
	return nil
}

// ListUsers returns every user, oldest first.
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, created_at FROM users ORDER BY created_at")
	if err != nil {
		return nil, fmt.Errorf("query users: %w", err)
	}
	defer func() { _ = rows.Close() }()

	users := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate users: %w", err)
	}
	return users, nil
}

// CountUsers returns the number of persisted users.
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM users").Scan(&n); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return n, nil
}

// DeleteOldestUser deletes the oldest user (by created_at) and returns it. It returns
// sql.ErrNoRows if the table is empty.
func (s *Store) DeleteOldestUser(ctx context.Context) (User, error) {
	var u User
	err := s.db.QueryRowContext(ctx,
		"DELETE FROM users WHERE id = (SELECT id FROM users ORDER BY created_at ASC LIMIT 1) RETURNING id, created_at",
	).Scan(&u.ID, &u.CreatedAt)
	if err != nil {
		return User{}, fmt.Errorf("delete oldest user: %w", err)
	}
	return u, nil
}
