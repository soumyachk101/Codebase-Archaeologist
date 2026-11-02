# Codebase Archaeologist

A version-aware, evidence-linked model of a git repository. It indexes
symbols, imports, calls, and commit history, then serves search,
grounded Q&A, impact, and reverse-dependency analysis — every fact
carrying a concrete source file + line + commit. Unresolvable references
are persisted as unknown, never invented.

- **Core** — Go CLI + store layer (`cli/`, `internal/`)
- **Service** — Chi API over the same store (`service/`)
- **Parser** — Rust tree-sitter extractor for JS / TS / Go (`parser/`)
- **Web app** — React workbench (`webapp/`)
- **Docs** — `docs/` (architecture, requirement traceability,
  evidence & provenance, known limitations)

## Install

Prereqs: Go ≥ 1.23, Rust + `cargo` (for the parser), `git`, `tar`.
For local development no database server is required — the default
driver is SQLite.

```sh
# Go CLI + store + service binaries
go build -o bin/archaeologist ./cli/cmd
go build -o bin/archaeologist-service ./service/cmd

# Rust tree-sitter parser
cargo build --release --manifest-path parser/Cargo.toml
# -> parser/target/release/parser  (rename or place on PATH as
#    "archaeologist-parser", or point ARCH_PARSER_BIN at it)
```

`go.sum` and `parser/Cargo.lock` pin the module and crate graphs; run
`go mod tidy` and `cargo generate-lockfile` if you change dependencies.

## Run the CLI (SQLite dev mode)

The default config (`internal/envx`) needs **zero env vars**: SQLite
driver, database file at `./data/archaeologist.sqlite`, snapshots under
`./data/snapshots/`. The target must be a git working tree.

```sh
# from the target repository's working tree
archaeologist init
archaeologist index            # HEAD; or: archaeologist index <commit|ref>
archaeologist status
archaeologist search "query"
archaeologist explain "symbol-or-module"
archaeologist impact path/to/file
archaeologist dependants <symbol>
archaeologist purge --yes
```

`--data-dir` overrides `ARCH_DATA_DIR`; `--root` overrides the
repository root (default: current directory). Index one repo at a time;
each absolute path is a distinct repository identity in the store.

## Run the service

**SQLite dev** (no server):

```sh
archaeologist-service          # binds ARCH_BIND (default :8080)
```

**Postgres prod** (managed or local Postgres):

```sh
DATABASE_URL=postgres://user:pass@host:5432/archaeologist \
ARCH_BIND=:8080 \
ARCH_AUTH_KEY=<key> \
archaeologist-service
```

The service applies its schema idempotently on startup; in a managed
Postgres you can pre-apply `infrastructure/migrations/0001_init.sql`
instead (see `infrastructure/DEPLOYMENT-NOTES.md`). Endpoints, list
envelopes, and error shapes are pinned in
`service/internal/api` — see `docs/ARCHITECTURE.md`.

## Environment variables

Full names and defaults are defined in `internal/envx` — the
`.env.example` at the repository root tracks them exactly.

| Variable | Meaning | Default |
|---|---|---|
| `DATABASE_URL` | Postgres DSN; setting it switches off SQLite | unset → SQLite |
| `ARCH_POSTGRES_DSN` | DSN fallback when `DATABASE_URL` is empty | unset |
| `ARCH_LOCAL_ONLY` | `"true"` forces the SQLite driver | unset |
| `ARCH_BIND` | API listen address | `:8080` |
| `ARCH_AUTH_KEY` | API key; empty disables auth | empty |
| `ARCH_DATA_DIR` | SQLite file + git snapshots | `./data` |
| `ARCH_PARSER_BIN` | parser binary path | `archaeologist-parser` on `$PATH` |
| `ARCH_LOG_LEVEL` | `debug\|info\|warn\|error` | `info` |

## Fixture tests

The parser ships golden fixtures under `testdata/fixture/`, split into
two commits so incremental carry-over can be exercised:

- `c1/` — `js/app.js`, `js/util.js`, `ts/shapes.ts`, `gomod/calc`,
  `gomod/mathx`
- `c2/` — the changed version of the same set

Drive them through the pipeline by indexing `c1` then `c2`
incrementally, or feed the parser directly:

```sh
# run the parser over a fixture tree (JSON in on stdin, JSON out)
cat fixture.json | ./parser/target/release/parser
```

The Go core is exercised by the CLI against these trees; store-layer
invariants are checked by `store.VerifyProvenance`
(`internal/store/queries_explain.go`).

## Layout

```
cli/cmd, internal/{cli,search,explain,store,ingest,git,envx}   Go CLI + core
service/cmd, service/internal/api                                Go API service
parser/                                                          Rust tree-sitter
webapp/                                                          React workbench
testdata/fixture/{c1,c2}/...                                     parser fixtures
docs/                                                            architecture, provenance, limits
infrastructure/migrations/0001_init.sql                          Postgres migration
.env.example                                                     env template
Dockerfile, docker-compose.yml, deploy/                          packaging
```

## Provenance guarantee

No node or edge exists without a concrete source file + line + commit;
unresolved references persist with `resolved=false` + `target_hint` and
are never invented. See `docs/EVIDENCE-PROVENANCE.md`.
