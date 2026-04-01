package utils

import (
	"context"
	"database/sql"
)

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
