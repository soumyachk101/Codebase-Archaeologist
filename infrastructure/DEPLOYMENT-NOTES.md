# Codebase Archaeologist — Infrastructure Deployment Notes

Companion to `deploy/GUIDE.md` (which owns the operational procedures).
This directory only adds the Postgres migration and how to apply it.

## The migration

`migrations/0001_init.sql` is a column-for-column mirror of the DDL the
store applies in-process (`internal/store/models.go`
`postgresDDL`, executed by `store.Migrate` on startup). Two equivalent
ways to reach the same schema:

- **In-process (default):** the service/CLI opens Postgres and runs
  `Migrate()` — DDL is `IF NOT EXISTS`, so it is idempotent and safe
  under concurrent startup.
- **Out-of-band (preferred for managed Postgres):** apply the SQL
  directly, so the schema is visible to the DBA before the first run:

```sh
psql "$DATABASE_URL" -f infrastructure/migrations/0001_init.sql
```

Either path produces the same objects; they are not cumulative
migrations.

## Schema notes (why it is dialect-neutral)

Per `internal/store/models.go`'s dialect strategy, every column is
`TEXT` or `INTEGER` in both SQLite and PostgreSQL:

- dates are RFC3339 `TEXT` (`created_at`, `commit_date`, `indexed_at`,
  `date`);
- booleans are `INTEGER` 0/1 (`resolved`, `exported`, `is_test`,
  `changed`, `derived`);
- JSON payloads are `TEXT` with a `'[]'` default (`files.exports`,
  `commits.files`, `answers.evidence`).

Consequences for operators:

- **No extensions required.** A vanilla Postgres 16 (or managed
  equivalent) is sufficient — no `pgvector`, no FTS extension, no
  specific DB name or role mandated by the code (the compose file's
  `POSTGRES_DB`/`POSTGRES_USER` are deployment convenience only).
- **`TEXT` primary keys are content-stable IDs.** Symbol, file, and edge
  IDs are deterministic hashes of their content plus the commit, so
  re-indexing a commit is an upsert, not a new identity.
- **A follow-up may move JSON payloads to `JSONB`** without changing the
  application: the code round-trips JSON as strings and marshals/unmarshals
  itself, so the column type is opaque to it.

## Tables

| Table | Purpose |
|---|---|
| `repositories` | indexed codebase identity |
| `index_runs` | one commit-aware snapshot; only `complete` runs are readable |
| `files` | source-file facts at a commit (composite PK `(id, commit)`) |
| `symbols` | declaration facts with full provenance (composite PK) |
| `symbol_versions` | per-commit location of a content-stable symbol key |
| `edges` | import/call relationships; `resolved` + `target_hint` per
  `docs/EVIDENCE-PROVENANCE.md` (composite PK) |
| `co_changes` | historical co-change pairs, kept distinct from `edges` |
| `commits` | git commit metadata + bounded touched-file list (JSON) |
| `answers` | stored explanations + evidence set (`derived` flag) |

## Operating notes

- **Concurrency:** `index_runs` status fencing means readers only see
  the newest `complete` run; a running index never leaks partial facts.
  The migration itself is safe to re-run any time.
- **Retention:** facts are commit-stamped, so purging a repository
  (`archaeologist purge` / `Store.DeleteRepositoryFully`) removes its
  rows rather than truncating the database.
- **Storage growth:** every indexed commit adds rows to
  `files`/`symbols`/`edges`. Prune old commits with
  `DELETE FROM <table> WHERE commit = '<sha>'` in the same order
  `purgeByCommit` uses, and keep `index_runs` rows so `status` remains
  meaningful.
