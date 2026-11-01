# Codebase Archaeologist — Webapp

The browser workbench for the Codebase Archaeologist service. It is a
React + TypeScript + Vite app that talks to the Go service over a pinned REST
contract. The five workspaces are:

| Tab | Job |
| --- | --- |
| **Architect** | Module graph (SVG layout + accessible table fallback), cross-module edges, focus-a-module and jump into symbol search. |
| **Inspect** | Definition evidence for one symbol (file, line range, commit — copyable, never a fabricated permalink), reverse dependencies with each dependant's source fact, and the symbol's commit timeline. |
| **Search** | Hybrid symbol/keyword search with a `q` + `type` filter and cursor pagination. Each result shows its commit-stamp and evidence reference; low-confidence and empty states are surfaced plainly. |
| **History** | Pick a symbol or file; the commit list is driven by the commit-stamps the index already records. Selecting a commit fetches `/commits/{sha}/timeline` and shows the files + symbols that changed. Includes the one permitted carousel: a symbol's commit-snapshot evolution. |
| **Explain** | An evidence-linked explanation for a symbol or module. The answer and every evidence reference are shown verbatim; coverage (full / partial / abstain) makes it unambiguous which parts are measured indexed facts and which are the derived/summary layer. |

## Setup

Requires Node 18+ and npm.

```bash
cd webapp
npm install
```

## Dev server

```bash
npm run dev
```

Runs the Vite dev server (default port `5173`) with HMR.

## Build

```bash
npm run build      # type-check + production bundle to ./dist
npm run preview    # serve the production build locally
npm run typecheck  # type-check only
```

## Pointing it at the service

The app never hard-codes where the Go service lives. The base URL is resolved
in `src/lib/env.ts` in this order:

1. **localStorage override** — the `API` field in the top bar. Type a base URL
   there and it is stored and used immediately; it survives reloads. This is
   the fastest way to redirect the running UI.
2. **`VITE_API_BASE_URL`** — a build-time env var. Copy `.env.example` to
   `.env` and set it:
   ```bash
   cp .env.example .env
   echo 'VITE_API_BASE_URL=http://my-host:8080' >> .env
   npm run build
   ```
3. **Default** — `http://localhost:8080`, where the service runs locally.

If the base URL points somewhere the service cannot be reached, the app does
not silently show a dead UI: the top bar shows an **offline** status dot and a
banner explains which base is in use and how to fix it. When the service is up
but has not indexed a repository, a banner says so instead of pretending data
exists.

## The Go service is being completed in parallel

The endpoints this UI calls (`/api/health`, `/api/index/status`,
`/api/repositories`, `/api/repositories/{id}/modules`,
`/api/repositories/{id}/symbols`, `/api/symbols/{id}`,
`/api/symbols/{id}/dependants`, `/api/commits/{sha}/timeline`,
`/api/explain`) are implemented by the Go service that a sibling head is
building at the same time. **Until that service lands and answers, every
workspace shows its honest connection/empty/error state — the app does not
invent or fabricate data.** That is by design: a loading spinner, an empty
"no repositories indexed" notice, or an "unreachable" banner are all correct
states while the backend is still being finished.

### Commands the lead should run

```bash
cd webapp
npm install          # install dependencies
npm run dev          # start the dev server on :5173
npm run typecheck    # verify types across the app
npm run build        # production build to ./dist
```

To run the app against a live service, set the base URL in the top-bar `API`
field (or `.env` / `VITE_API_BASE_URL`), e.g. `http://localhost:8080` for the
local Go service.
