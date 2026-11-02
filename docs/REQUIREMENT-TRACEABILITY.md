# Codebase Archaeologist — Requirement Traceability

Spec: `04-Codebase-Archaeologist.md`. This maps each product requirement to
its implementation in this repository, with an honest
**verified / landed** column.

"Verified" = wired into the Go call chain and compiled under
`go build ./...`. "Landed" = the code path exists and is internally
consistent, but external validation (a live Postgres instance, a model
provider, or a non-trivial real repo) has not been run here. "Out of
scope this pass" = intentionally not built in this scope.

The column reflects the tree as it stands; nothing is claimed verified
that has not compiled.

## FR-01 — Repository ingestion

| Requirement | Implementation | Status |
|---|---|---|
| Index a specific branch / commit SHA | `ingest.Options.Commit` / `.Ref`; `git.Repo.HeadCommitForRef`; `index --commit/--ref` | Verified |
| Local git repositories | `internal/git` wraps the git CLI; `index` refuses non-git trees (`IsRepo`) | Verified |
| GitHub App integration | Not built this pass | Out of scope |
| Detect language | `parser/src/language.rs` detects JS/TS/Go by extension | Landed |
| Detect build system / repo structure | Partial: module derived from path (`moduleForPath`); no build-system detection | Landed |
| Incrementally re-index changed files | `selectAndParse` re-parses diff-changed + new files, carries the rest | Verified (equivalence pending on a large diff) |
| Preserve index status and errors | `index_runs.status` + `error_message`; `status` command | Verified |

## FR-02 — Syntax and symbol analysis

| Requirement | Implementation | Status |
|---|---|---|
| Tree-sitter multi-language parsing | `parser/` (tree-sitter 0.22 + go/js/ts grammars) | Verified (builds) |
| Extract functions, classes, types, imports, exports | `parser/src/{js,go}.rs` → `PSymbol`/`PImport`/`PCall` | Landed (JS/TS/Go only) |
| Extract routes, config keys, schemas | Not implemented this pass | Out of scope |
| Record exact path, byte/line range, commit SHA | `store.Symbol` carries `File`, `StartLine/Byte`–`EndLine/Byte`, `Commit` | Verified |
| Preserve unresolved/dynamic references as unknown | `resolver.go` emits `resolved=false` + `target_hint` | Verified |

## FR-03 — Relationship graph

| Requirement | Implementation | Status |
|---|---|---|
| Import / dependency graph | `IMPORTS` edges (Go package match, JS relative specifier) | Verified |
| Call graph where reliable | `CALLS` edges resolved via import aliases; unresolved kept as hints | Verified |
| Route-to-handler mapping | Not implemented this pass | Out of scope |
| Service-to-DB/queue/external mapping | Not implemented this pass | Out of scope |
| Test-to-symbol links | Not implemented this pass | Out of scope |
| Reverse dependency + impact traversal | `DependentsOf` + `impact` bounded BFS | Verified |

## FR-04 — Git history and engineering context

| Requirement | Implementation | Status |
|---|---|---|
| Link symbols/files to commits | `commits` table; `CommitTimeline`; co-change facts | Verified |
| Detect refactors / ownership patterns | Not implemented this pass | Out of scope |
| Link design decisions to PRs/docs | Not implemented this pass | Out of scope |
| Distinguish recorded fact from inferred explanation | `Answer.Derived` + `Answer.Mode`; co-change kept in its own table | Verified |
| Do not infer intent from commit activity | No intent inference anywhere in `explain` | Verified |

## FR-05 — Search and Q&A

| Requirement | Implementation | Status |
|---|---|---|
| Hybrid lexical + semantic search | Lexical/fuzzy in `internal/search`; **no semantic/embedding layer this pass** | Landed (lexical only) |
| Query by symbol / path | `search` + `explain` targets | Verified |
| Answers cite exact files, line ranges, commit | `EvidenceReference` on every claim | Verified |
| Display evidence snippets + graph path | `Evidence` array; symbol graph path via `DependentsOf` | Verified |
| If evidence is insufficient, say so | `Answer.Abstained` + `AbstainedReason` | Verified |
| Filter by repository, branch, commit, permission | Repo + commit scoped; branch awareness via `index_runs`; **no permission scope** | Landed |

## FR-06 — Impact analysis

| Requirement | Implementation | Status |
|---|---|---|
| Direct + transitive dependants | `impact` bounded reverse BFS (depth ≤ 5) | Verified |
| Show confidence + unresolved dynamic refs | `Edge.Confidence`; unresolved shown separately | Verified |
| Recommend tests from co-change | `CoChangesForFile` exists; **not surfaced in the impact report** | Landed |
| Do not claim exhaustive coverage for dynamic langs | CLI prints the bounded-view disclaimer; `impact` doc says the same | Verified |

## FR-07 — Freshness and versioning

| Requirement | Implementation | Status |
|---|---|---|
| Track index commit SHA | `index_runs.commit` | Verified |
| Mark answers stale if branch advanced | `StatusOf` sets `stale` when live HEAD ≠ indexed commit | Verified |
| Keep indexes branch-aware | `index_runs.branch`; per-branch runs | Verified |
| Support index rebuild and deletion | `index` re-run; `purge` / `DeleteRepositoryFully` | Verified |

## MVP definition of done (spec §18)

| MVP item | Status |
|---|---|
| Local CLI indexes a repo at a fixed commit | Verified |
| Two selected languages with accurate import/symbol extraction | Landed (JS/TS/Go parsed; accuracy on a real repo not run here) |
| Search works for exact identifiers and natural language | Exact: verified; natural-language: abstain path verified, no semantic layer |
| Architecture graph shows evidence-backed relationships | Verified (`ModuleGraph` + edges, all commit-stamped) |
| Impact analysis distinguishes static from inferred | Verified (static vs. unresolved; co-change is a separate concern, not merged) |
| Q&A cites file+line evidence and can abstain | Verified |
| Incremental re-index keeps branch freshness accurate | Verified |
| UI is a purpose-built workbench, not a chat dashboard | `webapp/` exists as a React scaffold; not built out in this pass |

## Honest gaps (landed-but-unverified or out of scope)

- **Semantic/embedding search and model-provider Q&A are not wired.** The
  `Answer` type carries `Mode:"ai"` and `Derived` for a future summary
  layer, but no provider is configured; the grounded composer is what runs.
- **Permission/tenant isolation is not enforced** in the read paths (spec
  §12). The store is single-tenant as shipped.
- **GitHub App ingestion** (webhooks, private-repo hosting) is not built.
- **External-service validation** (live Postgres, a real monorepo, a
  provider call) has not been run in this pass — those are the
  "landed" rows above.
