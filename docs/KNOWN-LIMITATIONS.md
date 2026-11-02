# Codebase Archaeologist — Known Limitations

Honest list of what this pass does and does not cover. Nothing here is
presented as a capability; each item names the concrete bound.

## Parser languages (this pass)

The tree-sitter parser (`parser/`) extracts symbols, imports, calls, and
exports for **JavaScript, TypeScript, and Go only**:

- JS: `.js`, `.mjs`, `.cjs` (`parser/src/js.rs`, `language.rs`)
- TS/TSX: `.ts`, `.tsx` (the tree-sitter-typescript grammar builds both)
- Go: `.go` (`parser/src/go.rs`)

Any other extension is skipped by `internal/ingest` (`isSupportedExt`)
and reported in the manifest's `skipped` list — it is never guessed.
Python, Rust, Java, and the other languages in the spec's roadmap are
**not** indexed.

## The AI-summary layer is optional and labelled derived

`Answer` carries `Mode` (`grounded` | `ai` | `abstain`), `Derived`, and
`Summary`. The **grounded** composer (`internal/explain`) is what runs
today: its measured fields (claims, evidence, coverage) are direct store
reads. The `Summary` field is the optional derived layer — currently a
trivial, deterministic restatement of the first claim — and is always
emitted with `Derived=true` so the UI can label it as such. No model
provider is configured or called in this pass; `Mode:"ai"` is a reserved
value for a future provider that would set it.

## External-service validation is pending

The following are wired and compile, but have not been exercised against
a live service in this pass:

- **PostgreSQL.** The store speaks both dialects (`internal/store`) and the
  migration is in `infrastructure/migrations/0001_init.sql`, but
  validation was against the SQLite dev path. A live Postgres run is
  pending.
- **Incremental equivalence at scale.** `selectAndParse` re-parses only
  the diff-changed file set; the carry-over path is exact by
  construction, but equivalence on a large, deep-diff repository has not
  been measured.
- **The service endpoints against a populated Postgres/SQLite store**
  (auth, rate limiting, the pinned JSON contract) are unit-level correct
  but not load-validated.

## Other bounds

- **Search is lexical, not semantic.** `internal/search` is exact-match +
  weighted fuzzy over indexed symbols. There is no embedding or
  vector layer in this pass, so "natural language" questions rely on
  the grounded composer abstaining rather than on semantic retrieval.
- **Impact is a bounded static view.** Reverse-edge traversal caps at
  depth 5 and 2000 edges; the CLI and docs state plainly that this is not
  exhaustive impact coverage for dynamic languages.
- **No permission/tenant isolation** in the read paths; the store is
  single-tenant as shipped.
- **No GitHub App ingestion** (webhooks, private-repo hosting); indexing
  is local-git only via the CLI.
- **Co-change ≠ dependency.** `co_changes` records files touched in the
  same commit and is kept in a dedicated table precisely so it is never
  displayed as a proven runtime dependency.
