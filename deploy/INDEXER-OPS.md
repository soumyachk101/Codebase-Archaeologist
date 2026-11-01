# Indexer Operations — Incremental Re-indexing

What state lives where, and what is safe to clear.

## Where state lives

| State | Location | What it is |
|---|---|---|
| SQLite database (local mode) | `ARCH_DATA_DIR/archaeologist.sqlite` | All indexed facts: files, symbols, edges, co-change, index runs. This is the source of truth for readers. |
| Snapshots | `ARCH_DATA_DIR/snapshots/<commit>/` | The extracted source tree per commit (`git archive`). Input to the parser, not stored facts. |
| Postgres (non-local mode) | remote database | Same facts as the SQLite file, but on Postgres. `ARCH_DATA_DIR` is unused for the DB, but snapshots still land under `ARCH_DATA_DIR/snapshots/`. |
| Git working tree | the repo path (`--root` or cwd) | The source repo. Lives outside the data dir; the pipeline only reads from it. |

## Keying model

Facts are keyed by **`(repo ID, commit)`**, not by file checksum alone:

- `IndexRun` rows are per-repo and stamped with `commit`, `branch`,
  `parser_version`, `indexer_version`, `indexed_at`, and `status`
  (`pending | indexing | complete | failed`).
- `files`, `symbols`, and `edges` all carry a `commit` column and an FNV-1a
  `checksum` of the path and fact content.
- Stale runs are invisible to readers: the current run for a repo is the
  newest `complete` one; every fact is stamped with its run's commit.

## Incremental re-index

`archaeologist index` accepts `--incremental` plus `--parent <previousCommit>`
in the current CLI. The intent is: re-compute only files changed since the
parent commit, and carry over unchanged facts keyed by their checksum.

**Honest status:** the incremental recompute logic is not yet implemented in
`internal/ingest` — `Options.Incremental` and `Options.ParentCommit` are
carried into `IndexRun.ParentCommit` but the pipeline always does a full
re-index. See `NOTES-AND-GAPS.md`. Until it lands, re-indexing is: run
`archaeologist index` against the new commit; the old run stays as a prior
`complete` run, invisible to readers.

## What's safe to clear

| Clear this | Effect |
|---|---|
| `ARCH_DATA_DIR/snapshots/` | Safe. Rebuilt on next index run via `git archive`. Frees disk. |
| `ARCH_DATA_DIR/archaeologist.sqlite` (or Postgres contents) | **Destructive.** All indexed facts are gone; the next `init`/`index` rebuilds from scratch. |
| The git working tree | **Do not.** The pipeline snapshots from it; moving it also changes the repo ID (`"repo-" + absolute path`). |
| `ARCH_DATA_DIR` itself | **Destructive.** Both DB and snapshots gone. |

## Practical re-index

After a branch advances, `archaeologist status` reports `STALE` when the
working tree HEAD differs from the last complete run's commit. Re-index by
running `archaeologist index` against the new commit. If you want to bound
history, pass `--depth <n>` (default 100 commits walked back).
