# Codebase Archaeologist — Architecture

## What it is

A version-aware, evidence-linked model of a git repository. It indexes
symbols, imports, calls, and commit history into a durable store, then
serves search, explain (grounded Q&A), impact, and dependant analysis over
that store. The one hard invariant: **no node or edge exists without a
concrete source file + line + commit**, and anything that cannot be
resolved is persisted as `resolved=false` with a `target_hint` — never
invented. See `docs/EVIDENCE-PROVENANCE.md`.

## Process layout

Three components, all built from one repository:

| Component | Source | Binary |
|---|---|---|
| Go CLI + store | `cli/cmd` → `internal/*` | `archaeologist` |
| Go API service | `service/cmd` → `service/internal/api` | (Chi service on `ARCH_BIND`) |
| Rust tree-sitter parser | `parser/` | `archaeologist-parser` |

The documented image layout (root `Dockerfile`, `deploy/GUIDE.md`) is:

```
/opt/archaeologist/bin/archaeologist        Go CLI + store layer
/opt/archaeologist/bin/archaeologist-parser Rust tree-sitter extractor
/var/lib/archaeologist                      ARCH_DATA_DIR (sqlite + snapshots)
```

The CLI locates the parser as a separate process via `ARCH_PARSER_BIN`
(default `archaeologist-parser` on `PATH`): JSON in on stdin, JSON out on
stdout (`internal/ingest/pipeline.go` `runParser`).

## The data layer (shared by CLI and service)

`internal/store` is the single source of truth. One schema, two dialects:
SQLite for local development, PostgreSQL for production, selected by
`envx`'s driver switch. `store.Open(driver, dsn)` opens the dialect;
`store.Migrate(ctx)` applies the schema idempotently; the same DDL is the
Postgres migration in `infrastructure/migrations/0001_init.sql`.

The dialect strategy is uniform: every column is `TEXT` or `INTEGER` in
both databases; dates are RFC3339 strings, booleans are `0/1`, JSON
payloads are `TEXT`. One code path serves both drivers — no DB-specific
types or extensions.

Tables (`internal/store/models.go`): `repositories`, `index_runs`,
`files`, `symbols`, `symbol_versions`, `edges`, `co_changes`, `commits`,
`answers`.

## The indexing pipeline

`internal/ingest` drives one commit-aware index run:

1. **Snapshot** — `git archive` extracts the commit's tree into
   `ARCH_DATA_DIR/snapshots/<commit>/`. This is the only path by which
   source text reaches the parser, so the parser sees exactly the file set
   the commit recorded (`internal/git/git.go` `Snapshot`).
2. **Filter** — `.gitignore` rules and a 500 KB per-file bound; supported
   extensions only (`isSupportedExt`: `.js .mjs .cjs .ts .tsx .go`).
3. **Parse** — the Rust tree-sitter parser over JS/TS/Go (three languages
   this pass).
4. **Resolve** — `resolver.go` turns import/call facts into edges,
   resolving only where the target symbol exists in this exact index;
   everything else is emitted as an unresolved edge with a hint.
5. **Incremental carry-over** — an incremental run re-parses only
   diff-changed and new files; unchanged files carry over from the parent
   run's cached manifest, re-stamped to the new commit (`selectAndParse`).
6. **Commit atomically** — all facts are written inside one transaction
   per run (`FactTx`); the run is marked `complete` only when the batch
   commits, so a partial batch can never leak.

`index_runs` carries index-version fencing: readers see only the newest
`complete` run, so a stale concurrent run is invisible.

## Read paths

- **Search** — `internal/search` scores the store's candidate set: exact
  name/qualified-name/keyword matches dominate; weighted fuzzy (subsequence
  + n-gram) ranks the rest. Every result carries a commit-stamp and its
  own evidence. Zero coverage at the current commit returns an explicit
  not-found, never a guess from another commit.
- **Explain** — `internal/explain` composes an evidence-grounded answer:
  "what is / where is / who depends on X" strictly from persisted,
  commit-stamped facts. The measured fields are direct store reads; the
  optional summary layer is marked `derived`. When coverage is too low it
  returns an abstain that names the missing evidence.
- **Impact / dependants** — bounded reverse-edge traversal over stored
  edges; static and unresolved edges are surfaced separately.
- **Provenance check** — `store.VerifyProvenance` re-scans the stored
  facts and returns every violation of the invariant.

## The service

`service/cmd` loads `envx` config, opens the store per dialect, and serves
the pinned Chi contract (`service/internal/api`): `/api/health`,
`/api/index/status`, `/api/repositories`, `/api/repositories/{id}/modules`,
`/api/repositories/{id}/symbols`, `/api/symbols/{id}`,
`/api/symbols/{id}/dependants`, `/api/commits/{sha}/timeline`, and
`/api/explain`. Lists are `{items, next_cursor}`; errors are
`{error:{code,message}}`. Auth (`ARCH_AUTH_KEY`) and per-repository rate
limiting front the routes; shutdown is graceful on SIGINT/SIGTERM.

## Layout

```
cli/cmd, internal/{cli,search,explain,store,ingest,git,envx}   Go CLI + core
service/cmd, service/internal/api                                Go API service
parser/                                                          Rust tree-sitter
webapp/                                                          React workbench (out of scope here)
testdata/fixture/{c1,c2}/...                                     parser fixtures
docs/                                                            this directory
infrastructure/migrations/0001_init.sql                          Postgres migration
deploy/, Dockerfile, docker-compose.yml                           packaging (do not modify)
```
