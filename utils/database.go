/*
Package utils/database defines the DB accessor interface, sql.DB wrapper, and opening Postgres from the environment.

Env files are not read here. Call utils.Env.Init() (or InitOps for zz-ops) first so DATABASE_URL is in os.Getenv;
then DatabaseManager.Init(ctx) reads DATABASE_URL and pings Postgres.

Live vs test: set APP_ENV before start (e.g. Docker ENV APP_ENV=live, or export APP_ENV=test locally).
APP_ENV unset or test → prefer .env.test; other values (e.g. live) → .env only.
*/
package utils

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

const dbPingTimeout = 5 * time.Second

type DB interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type Database struct {
	raw *sql.DB
}

func NewDatabase(raw *sql.DB) *Database {
	return &Database{raw: raw}
}

func (d *Database) SQL() *sql.DB {
	return d.raw
}

func (d *Database) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return d.raw.ExecContext(ctx, query, args...)
}

func (d *Database) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return d.raw.QueryContext(ctx, query, args...)
}

func (d *Database) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return d.raw.QueryRowContext(ctx, query, args...)
}

func (d *Database) Close() error {
	return d.raw.Close()
}

// --- Open from env (after Env.Init / InitOps) ---

type DatabaseFactory struct{}

var DatabaseManager = NewDatabaseFactory()

func NewDatabaseFactory() *DatabaseFactory {
	return &DatabaseFactory{}
}

func (f *DatabaseFactory) Init(ctx context.Context) (*Database, error) {
	return OpenDatabaseFromEnv(ctx)
}

func OpenDatabaseFromEnv(ctx context.Context) (*Database, error) {
	dsn := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dsn == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	raw, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, dbPingTimeout)
	defer cancel()
	if err := raw.PingContext(pingCtx); err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return NewDatabase(raw), nil
}
