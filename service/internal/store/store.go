// Package store is the shared data layer for the CLI and the service.
//
// One schema, two dialects: SQLite (local development and local CLI) and
// PostgreSQL (production). The schema is written with `?` placeholders and
// a `:json` column-type token; Open translates them per driver. All
// transactional writes (index runs) commit or roll back as a unit.
package store

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"

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

// Open opens a database for the given driver. SQLite expects a file path
// (or "file::memory:?cache=shared"); Postgres expects a DSN.
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

// DB exposes the underlying handle for query code in other packages.
func (s *Store) DB() *sql.DB { return s.db }

// DriverName returns "sqlite" or "postgres".
func (s *Store) DriverName() string { return string(s.driver) }

// Ping verifies connectivity.
func (s *Store) Ping() error {
	return s.db.Ping()
}

// Close releases the handle.
func (s *Store) Close() error {
	return s.db.Close()
}

// Migrate applies the schema to the database, idempotently.
func (s *Store) Migrate() error {
	stmts, err := s.schemaStatements()
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			tx.Rollback()
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return tx.Commit()
}

var qMarks = regexp.MustCompile(`\?`)

// translate rewrites `?` placeholders to `$n` and expands `:json` column
// types for the Postgres dialect.
func (s *Store) translate(stmt string) string {
	if s.driver == SQLite {
		return strings.ReplaceAll(stmt, ":json", "TEXT")
	}
	out := strings.ReplaceAll(stmt, ":json", "JSONB")
	var n int
	return qMarks.ReplaceAllStringFunc(out, func(_ string) string {
		n++
		return fmt.Sprintf("$%d", n)
	})
}

// Exec executes a translated statement with args.
func (s *Store) Exec(stmt string, args ...any) (sql.Result, error) {
	return s.db.Exec(s.translate(stmt), args...)
}

// Query runs a translated statement, returning rows.
func (s *Store) Query(stmt string, args ...any) (*sql.Rows, error) {
	return s.db.Query(s.translate(stmt), args...)
}

// QueryRow runs a translated statement expecting a single row.
func (s *Store) QueryRow(stmt string, args ...any) *sql.Row {
	return s.db.QueryRow(s.translate(stmt), args...)
}
