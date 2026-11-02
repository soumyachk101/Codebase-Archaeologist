-- Codebase Archaeologist — PostgreSQL migration.
--
-- This file mirrors internal/store/models.go's postgresDDL exactly: the
-- store applies the same DDL idempotently at startup (store.Migrate). A
-- managed-Postgres deployment may apply it directly with psql instead, and
-- it proves the uniform TEXT/INTEGER dialect strategy the code already uses.
--
-- Dialect strategy (read internal/store/models.go):
--   * every column is TEXT or INTEGER in both SQLite and PostgreSQL;
--   * dates are RFC3339 strings, booleans are 0/1, JSON payloads are TEXT;
--   * one code path serves both drivers — no DB-specific types, casts, or
--     extensions. A follow-up may move JSON payloads to JSONB without
--     changing the API surface.
--
-- Apply:  psql "$DATABASE_URL" -f infrastructure/migrations/0001_init.sql
-- Nothing here requires a specific DB name or role; the code creates no
-- objects of its own beyond these tables and indexes.

-- ------------------------------------------------------------------
-- repositories: an indexed codebase
-- ------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS repositories (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    origin TEXT NOT NULL DEFAULT 'local',
    root TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);

-- ------------------------------------------------------------------
-- index_runs: one commit-aware snapshot of a repository. A run is current
-- for its repository when it is the newest "complete" run. All facts are
-- stamped with the run's commit, so stale runs are invisible to readers.
-- ------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS index_runs (
    id TEXT PRIMARY KEY,
    repository_id TEXT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    commit TEXT NOT NULL,
    commit_date TEXT NOT NULL,
    branch TEXT NOT NULL DEFAULT '',
    parser_version TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    indexed_at TEXT,
    indexed_files INTEGER NOT NULL DEFAULT 0,
    indexer_version TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    parent_commit TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_runs_repo ON index_runs(repository_id);

-- ------------------------------------------------------------------
-- files: a source file in an index run. exports is a JSON array in TEXT.
-- ------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS files (
    id TEXT NOT NULL,
    commit TEXT NOT NULL,
    path TEXT NOT NULL,
    language TEXT NOT NULL DEFAULT '',
    module TEXT NOT NULL DEFAULT '',
    checksum TEXT NOT NULL DEFAULT '',
    size_bytes INTEGER NOT NULL DEFAULT 0,
    exports TEXT NOT NULL DEFAULT '[]',
    top_symbols INTEGER NOT NULL DEFAULT 0,
    first_commit TEXT NOT NULL DEFAULT '',
    last_commit TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (id, commit)
);
CREATE INDEX IF NOT EXISTS idx_files_commit_path ON files(commit, path);
CREATE INDEX IF NOT EXISTS idx_files_module ON files(module);

-- ------------------------------------------------------------------
-- symbols: one declaration with full provenance.
-- ------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS symbols (
    id TEXT NOT NULL,
    commit TEXT NOT NULL,
    file TEXT NOT NULL,
    kind TEXT NOT NULL,
    name TEXT NOT NULL,
    qualified_name TEXT NOT NULL,
    language TEXT NOT NULL,
    start_line INTEGER NOT NULL,
    end_line INTEGER NOT NULL,
    start_byte INTEGER NOT NULL DEFAULT 0,
    end_byte INTEGER NOT NULL DEFAULT 0,
    exported INTEGER NOT NULL DEFAULT 0,
    is_test INTEGER NOT NULL DEFAULT 0,
    parent TEXT NOT NULL DEFAULT '',
    module TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (id, commit)
);
CREATE INDEX IF NOT EXISTS idx_symbols_commit_file ON symbols(commit, file);
CREATE INDEX IF NOT EXISTS idx_symbols_commit_name ON symbols(commit, name);
CREATE INDEX IF NOT EXISTS idx_symbols_commit_kind ON symbols(commit, kind);

-- ------------------------------------------------------------------
-- symbol_versions: per-commit location of a content-stable symbol key.
-- ------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS symbol_versions (
    symbol_id TEXT NOT NULL,
    key TEXT NOT NULL,
    commit TEXT NOT NULL,
    start_line INTEGER NOT NULL,
    end_line INTEGER NOT NULL,
    changed INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (symbol_id, commit)
);
CREATE INDEX IF NOT EXISTS idx_symver_key ON symbol_versions(key);

-- ------------------------------------------------------------------
-- edges: a persisted relationship. Static edges point at a concrete
-- symbol; co-change edges link a file to a commit. Unresolved references
-- persist with resolved=0 + a target_hint (the observed source
-- expression) — never invented.
-- ------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS edges (
    id TEXT NOT NULL,
    commit TEXT NOT NULL,
    from_id TEXT NOT NULL,
    to_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    resolved INTEGER NOT NULL DEFAULT 0,
    confidence TEXT NOT NULL DEFAULT 'unresolved',
    target_hint TEXT NOT NULL DEFAULT '',
    source_file TEXT NOT NULL,
    source_line INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (id, commit)
);
CREATE INDEX IF NOT EXISTS idx_edges_commit_from ON edges(commit, from_id);
CREATE INDEX IF NOT EXISTS idx_edges_commit_to ON edges(commit, to_id);
CREATE INDEX IF NOT EXISTS idx_edges_commit_kind ON edges(commit, kind);

-- ------------------------------------------------------------------
-- co_changes: historical co-change evidence, kept in a dedicated table so
-- it is displayed distinctly from static edges (never treated as a proven
-- runtime dependency).
-- ------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS co_changes (
    file_a TEXT NOT NULL,
    file_b TEXT NOT NULL,
    commit TEXT NOT NULL,
    commit_date TEXT NOT NULL,
    count INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (file_a, file_b, commit)
);
CREATE INDEX IF NOT EXISTS idx_cochange_files ON co_changes(file_a, file_b);

-- ------------------------------------------------------------------
-- commits: git commit metadata and the (bounded, deterministic) file list
-- it touched. files is a JSON array in TEXT.
-- ------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS commits (
    commit TEXT PRIMARY KEY,
    branch TEXT NOT NULL DEFAULT '',
    message TEXT NOT NULL DEFAULT '',
    author TEXT NOT NULL DEFAULT '',
    date TEXT NOT NULL,
    files TEXT NOT NULL
);

-- ------------------------------------------------------------------
-- answers: a stored explanation with its evidence set. mode "ai" means the
-- text was model-generated and MUST display as derived; evidence is what
-- is verified. evidence is a JSON array in TEXT.
-- ------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS answers (
    id TEXT PRIMARY KEY,
    commit TEXT NOT NULL,
    question TEXT NOT NULL,
    answer TEXT NOT NULL,
    mode TEXT NOT NULL,
    confidence TEXT NOT NULL,
    derived INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    evidence TEXT NOT NULL DEFAULT '[]'
);
