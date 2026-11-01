# Codebase Archaeologist — Deployment Guide

Covers three targets: local development with SQLite, single-host Docker
Compose with Postgres, and managed Postgres. Plus the repository ingestion
path, which is the operational surface you actually touch day to day.

## 1. Local development (SQLite)

Default config (`internal/envx`) with zero env vars: SQLite driver, database
file at `./data/archaeologist.sqlite`, snapshots under `./data/snapshots/`.
No env vars needed; run the Go CLI in the target repo's directory:

```sh
# from the target repository's working tree
archaeologist init
archaeologist index            # HEAD; or: archaeologist index v1.2.3
archaeologist status
```

`--data-dir` overrides `ARCH_DATA_DIR`; `--root` overrides the repository
root (default: current working directory).

## 2. Single-host compose (Postgres)

The `docker-compose.yml` at the repo root brings up Postgres 16 plus the
archaeologist image. Env vars (names match `internal/envx` exactly):

| Var | Meaning | Default |
|---|---|---|
| `DATABASE_URL` | Postgres DSN (primary switch to non-local driver) | — |
| `ARCH_POSTGRES_DSN` | Fallback DSN if `DATABASE_URL` unset | — |
| `ARCH_LOCAL_ONLY` | `"true"` forces SQLite even when a DSN is set | unset |
| `ARCH_BIND` | API listen address (used when the service lands) | `:8080` |
| `ARCH_AUTH_KEY` | API key; empty disables auth | empty |
| `ARCH_DATA_DIR` | snapshots + local data | `./data` |
| `ARCH_PARSER_BIN` | parser binary path | `archaeologist-parser` on PATH |
| `ARCH_LOG_LEVEL` | `debug\|info\|warn\|error` | `info` |

Driver switching rule: if `DATABASE_URL` (or `ARCH_POSTGRES_DSN`) is set,
the store opens Postgres; otherwise it falls back to local SQLite under
`ARCH_DATA_DIR`.

Typical run, indexing a repo mounted into the container (see §4):

```sh
docker compose run --rm archaeologist archaeologist index --root /repo
```

## 3. Managed Postgres

Point `DATABASE_URL` at the managed instance; nothing else changes:

```sh
DATABASE_URL=postgres://user:pass@host:5432/db \
ARCH_AUTH_KEY=<key> \
archaeologist index --root /path/to/repo
```

The schema is dialect-neutral (TEXT/INTEGER columns only, RFC3339 date
strings, JSON-in-TEXT payloads), so no managed-DB-specific extensions are
required. The DSN goes on the `DATABASE_URL` var because that is what
`envx.Load()` checks first; `ARCH_POSTGRES_DSN` is only read when
`DATABASE_URL` is empty.

## 4. Repository ingestion path

How the indexer expects target repos on disk (verified from
`internal/ingest` and `internal/cli`, not guessed):

- **The target must be a git working tree.** `archaeologist index` checks
  `git rev-parse --git-dir` and refuses non-repos. Plain source checkouts
  without `.git` are not indexable.
- **Repo location:** the working directory, or `--root <path>` (accepted by
  `index`, `init`, `status`). The repo ID is derived from that absolute
  path (`"repo-" + path`), so moving the repo on disk creates a new repo
  identity.
- **One repo at a time.** A single run indexes exactly one repository at
  one commit (or one ref; `--commit` for an exact SHA, `--ref` for a
  branch/tag). For multiple repos, run the CLI once per repo, each with its
  own `--root`; they coexist in the same store under distinct repo IDs.
- **How files reach the parser:** the pipeline extracts the commit's tree
  via `git archive` into `ARCH_DATA_DIR/snapshots/<commit>/` and feeds the
  Rust parser via **stdin JSON** (`archaeologist-parser`, JSON in / JSON
  out) — no filesystem hand-off beyond that snapshot dir.
- **In compose:** mount the repo read-only at a fixed path (e.g.
  `/repo`) and pass `--root /repo`. Only the snapshots dir and the SQLite
  file need to be persistent; the repo mount itself can be a bind mount or
  a cloned volume.

## 5. Webapp

Not deployed yet. The compose file carries a commented-out placeholder
block for it; uncomment when the webapp build lands.
