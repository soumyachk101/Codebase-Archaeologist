// Package store is the single data layer for CLI and service:
// one schema, two dialects (SQLite for local, PostgreSQL for production),
// shared models, and every query the product uses.
//
// All index writes happen inside one transaction per index run; the
// schema carries index-version fencing so a stale concurrent run cannot
// overwrite a newer one.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

// Driver selects the database dialect.
type Driver string

const (
	SQLite   Driver = "sqlite"
	Postgres Driver = "postgres"
)

// Store wraps an opened database plus the dialect it speaks.
type Store struct {
	db     *sql.DB
	driver Driver
}

// Open opens a database for the given driver. SQLite expects a file path;
// Postgres expects a DSN.
func Open(driver Driver, dsn string) (*Store, error) {
	var db *sql.DB
	switch driver {
	case SQLite:
		var err error
		db, err = sql.Open("sqlite", dsn)
		if err != nil {
			return nil, err
		}
		if _, err := db.Exec(`PRAGMA journal_mode = WAL;`); err != nil {
			db.Close()
			return nil, fmt.Errorf("sqlite pragma: %w", err)
		}
		if _, err := db.Exec(`PRAGMA foreign_keys = ON;`); err != nil {
			db.Close()
			return nil, fmt.Errorf("sqlite pragma: %w", err)
		}
	case Postgres:
		pgxDb, err := stdlib.OpenDBWithDefaultOptions(dsn)
		if err != nil {
			return nil, err
		}
		db = pgxDb
	default:
		return nil, fmt.Errorf("unknown driver %q", driver)
	}
	s := &Store{db: db, driver: driver}
	if err := s.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// DB exposes the underlying handle.
func (s *Store) DB() *sql.DB { return s.db }

// DriverName returns "sqlite" or "postgres".
func (s *Store) DriverName() string { return string(s.driver) }

// Ping verifies connectivity.
func (s *Store) Ping() error { return s.db.Ping() }

// Close releases the handle.
func (s *Store) Close() error { return s.db.Close() }

// q rewrites a statement for the active dialect: `?` placeholders become
// $n for Postgres; SQLite uses `?` natively. Statements are authored with
// `?` placeholders.
func (s *Store) q(stmt string) string {
	if s.driver == SQLite {
		return stmt
	}
	out := strings.ReplaceAll(stmt, "?", "$?")
	var n int
	return regexp.MustCompile(`\$\?`).ReplaceAllStringFunc(out, func(_ string) string {
		n++
		return fmt.Sprintf("$%d", n)
	})
}

// exec runs a translated statement on the pool.
func (s *Store) exec(ctx context.Context, stmt string, args ...any) (sql.Result, error) {
	return s.db.ExecContext(ctx, s.q(stmt), args...)
}

// execTx runs a translated statement inside a transaction.
func (s *Store) execTx(ctx context.Context, tx *sql.Tx, stmt string, args ...any) (sql.Result, error) {
	return tx.ExecContext(ctx, s.q(stmt), args...)
}

// row runs a translated statement expected to return at most one row.
func (s *Store) row(ctx context.Context, stmt string, args ...any) *sql.Row {
	return s.db.QueryRowContext(ctx, s.q(stmt), args...)
}

// rows runs a translated statement expected to return many rows.
func (s *Store) rows(ctx context.Context, stmt string, args ...any) (*sql.Rows, error) {
	return s.db.QueryContext(ctx, s.q(stmt), args...)
}

// Migrate applies the schema idempotently. DDL is dialect-specific and has
// no placeholders, so it runs as-is.
func (s *Store) Migrate(ctx context.Context) error {
	stmts := schemaDDL(s.driver)
	t, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer t.Rollback() //nolint:errcheck
	for _, stmt := range stmts {
		if _, err := t.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return t.Commit()
}
