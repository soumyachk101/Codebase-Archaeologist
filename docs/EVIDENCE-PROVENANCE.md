# Codebase Archaeologist — Evidence & Provenance

The hard guarantee, stated precisely: **no node or edge in the index
exists without a concrete source file + line + commit.** Every fact the
service or CLI returns is a direct read of a stored, commit-stamped row.
When a reference cannot be resolved, it is persisted as
`resolved = false` with a `target_hint` (the source expression as
written) — it is never promoted to a resolved edge, and nothing is
invented to fill the gap.

## Where the guarantee is enforced

### At write time — `internal/ingest/resolver.go`

Resolution happens in exactly one place: `BuildFacts`. It is
deterministic (same input, same edges). Three rules:

1. **Every symbol carries provenance.** Each emitted
   `store.Symbol` records `File`, `StartLine`/`EndLine`,
   `StartByte`/`EndByte`, and the `Commit` it was parsed at
   (`BuildFacts`, symbol loop).
2. **Resolved edges point at a real symbol.** A `CALLS`/`IMPORTS` edge
   is marked `resolved=true` only when its target was looked up in the
   index's own export/symbol tables at this exact commit
   (`resolveJSCalls`, `resolveGoCalls`, `resolveGoImports`). The `to_id`
   is that symbol's ID — never a guessed or synthesized name.
3. **Unresolved references persist as unknown.** When the target is not
   in the index (external package, bare specifier, dynamic reference,
   missing export), the edge is emitted with `resolved=false`,
   `confidence="unresolved"`, and `target_hint` set to the parser's hint
   or the source expression as written (`importHint`, `callHint`). The
   hint is the *observed* source text, never an inference.

The `FromID` of an edge is the declaring symbol, or a `synthetic:` ID
keyed to the file — always a concrete source, so even an unresolved
edge has a known origin (`moduleID`).

### At read time — `internal/store`

- `SymbolLatest`, `SymbolAt`, `DependentsOf`, `EdgesOf`,
  `CommitTimeline`, `SearchSymbols` all read only persisted,
  commit-stamped rows. Search returns the store's candidate set and the
  scorer only *orders* it (`internal/search`); it never adds a symbol
  the parser did not index.
- Zero-coverage queries return an explicit not-found / abstain, not a
  fallback to another commit.

### As a verifiable invariant — `store.VerifyProvenance`

`internal/store/queries_explain.go` `VerifyProvenance` re-scans the store
and returns every violation of the guarantee:

1. an edge whose `commit`, `source_file`, or `source_line` is missing;
2. a `resolved=true` edge whose `to_id` does not name a symbol that
   exists at that edge's commit (a resolved fact may never dangle into an
   invented symbol);
3. a `resolved=false` edge with an empty `target_hint`;
4. a symbol missing its `file` or `commit`.

An empty result set is the invariant holding.

## In answers — `internal/explain`

The answer composer applies the same rule to text:

- **Every factual claim** in an `Answer` is backed by one or more
  `EvidenceReference` (file, line range, symbol, commit) drawn
  directly from the store.
- **No claim may be invented.** When the evidence an answer needs is not
  in the index, `Explain` returns `Abstained=true` with an
  `AbstainedReason` naming the specific missing evidence — never a guess.
- **The derived layer is labelled.** The optional `Summary` (currently a
  trivial restatement of the first claim) is always marked
  `Answer.Derived=true`; the measured fields (claims, evidence,
  coverage) are direct reads and never carry the derived flag, so a
  summary can never rewrite a measured fact.

## Unresolved references are data, not noise

The `edges` table's `resolved` + `confidence` + `target_hint` columns are
deliberate: an unresolved reference is a *persisted fact about the
source* (here is the expression, at this file:line, at this commit, and
it did not resolve in this index). The CLI and service surface it
explicitly — `dependants` marks it `UNRESOLVED` and prints the hint;
`explain` lists it under "unresolved references (kept as unknown, not
invented)". Co-change edges are kept in a separate `co_changes` table and
are never presented as runtime dependencies.
