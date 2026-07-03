// Package db provides shared PostgreSQL connectivity and schema migrations for the backend. One pool
// and one goose migration sequence back every store (sessions, laps, ...); stores receive the pool
// and never own its lifecycle or run their own migrations.
package db

import (
	"context"
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// migrationsFS holds every backend migration, embedded so the binary is self-contained.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// Connect opens a pgx pool for dsn and verifies it with a ping. The caller owns the pool and must
// Close it on shutdown.
func Connect(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("db: pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}
	return pool, nil
}

// Migrate applies the embedded goose migrations (idempotent). goose speaks database/sql, so the pool
// is adapted via stdlib.OpenDBFromPool; closing that *sql.DB does not close the underlying pool.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer func() { _ = sqlDB.Close() }()

	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("db: goose dialect: %w", err)
	}
	if err := goose.UpContext(ctx, sqlDB, "migrations"); err != nil {
		return fmt.Errorf("db: migrate: %w", err)
	}
	return nil
}
