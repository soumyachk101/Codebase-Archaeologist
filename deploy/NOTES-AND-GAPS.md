# Deployment Notes and Gaps

Findings from packaging; each item is concrete and verified against the
tree at the time of writing.

## 1. Go CLI does not compile yet — subcommand files are missing

`internal/cli/root.go:35-40` calls `newSearch()`, `newExplain()`,
`newImpact()`, `newDependants()`, `newPurge()`, but none of these
functions exists anywhere in the tree (`internal/cli` has only
`app.go`, `index.go`, `root.go`, `status.go`; `cli/internal` is empty).
The Go build stage of the Dockerfile will fail until the CLI head lands
those files (or a search.go / explain.go / impact.go / dependants.go /
purge.go under internal/cli). **The one smoke-test command below is
blocked on this.**

## 2. `go.sum` is missing

Only `go.mod` exists; `go.sum` has not been generated. `go build` with
the default `-mod=readonly` will fail ("missing go.sum entry"). The
Dockerfile uses `GOFLAGS=-mod=mod` as a stopgap so `go build` regenerates
the module graph and tolerates the missing sum file. The right fix is for
the lead to run `go mod tidy` and commit `go.sum`; then the stopgap flag
can be dropped.

## 3. `parser/Cargo.lock` is missing

Cargo builds will succeed either way, but pinning `Cargo.lock` at the
lead's discretion is recommended so the parser binary is reproducible
across images.

## 4. The Chi service is not buildable — container ships CLI + parser

`service/` contains only a copy of the store layer
(`service/internal/store/store.go`); `service/cmd/` is empty and
`internal/api/` is empty. No HTTP entrypoint exists to run. The image's
`CMD` therefore runs the Go CLI (`archaeologist` → cobra help), and the
image label reads "API entrypoint lands with service completion". When
the service lands, add a second image (or a second entrypoint) that runs
the service binary with `ARCH_BIND` and `ARCH_AUTH_KEY` wired; compose
already has those two env vars commented in place.

## 5. Incremental indexing is declared but not implemented

`ingest.Options.Incremental` and `Options.ParentCommit` are accepted by
the CLI (`--incremental`, `--parent`) and `ParentCommit` is persisted on
the `IndexRun`, but `ingest.Pipeline.Run` ignores both and always performs
a full re-index. `deploy/INDEXER-OPS.md` states this honestly.

## 6. No webapp build exists

`webapp/` has only empty scaffold dirs (`public/`, `src/components|
features|lib` — no files). The compose file carries a commented-out
placeholder block, not a working nginx config, per the hard rule.

## 7. No fixtures in scope

No `testdata/` or `fixtures/` directory exists yet; `.dockerignore`
pre-excludes both so fixture repos stay out of the image when they
appear. Fixtures are test data, not runtime data — they are never a
compose mount target.

## 8. `ARCH_BIND` and `ARCH_AUTH_KEY` are dead env vars until the API lands

`envx` reads them with sensible defaults, but no process in the current
image consumes them (the CLI does not bind a port). Wiring them in
compose is harmless and forward-compatible.
