// Package api implements the Codebase Archaeologist HTTP service.
//
// The pinned contract (the webapp is built against it verbatim):
//
//	GET /api/health
//	GET /api/index/status
//	GET /api/repositories
//	GET /api/repositories/{id}/modules
//	GET /api/repositories/{id}/symbols?q=&type=&limit=&cursor=
//	GET /api/symbols/{id}
//	GET /api/symbols/{id}/dependants
//	GET /api/commits/{sha}/timeline
//	GET /api/explain?repository={id}&target={symbol-or-module}
//
// All responses are JSON. List endpoints use { items, next_cursor };
// errors use { error: { code, message } }. Measured fields are direct
// store reads; nothing is invented.
package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/ensembyte/archaeologist/internal/explain"
	"github.com/ensembyte/archaeologist/internal/search"
	"github.com/ensembyte/archaeologist/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// New builds the Chi router with middleware (logging, recovery, CORS) and
// the pinned route table.
func New(st *store.Store) *chi.Mux {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(structuredLogger)

	r.Get("/api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Get("/api/index/status", func(w http.ResponseWriter, req *http.Request) {
		// Freshness per repository: newest complete run (the current one)
		// plus whether a newer non-complete run is in progress. Read-only;
		// staleness against a live git HEAD is the CLI's job (it has the
		// working tree), so this reports store-side state.
		repos, err := st.ListRepositories(req.Context())
		if err != nil {
			writeErr(w, "INTERNAL", err.Error())
			return
		}
		type repoStatus struct {
			RepositoryID string         `json:"repositoryId"`
			Name         string         `json:"name"`
			Current      *store.IndexRun `json:"current"`
			InProgress   bool           `json:"inProgress"`
		}
		out := []repoStatus{}
		for _, rep := range repos {
			entries := []store.IndexRun{}
			if cur, cErr := st.GetCurrentRun(req.Context(), rep.ID); cErr == nil {
				entries = append(entries, cur)
			}
			inProgress := false
			if last, lErr := st.LastRun(req.Context(), rep.ID); lErr == nil && last.Status != "complete" {
				inProgress = true
			}
			var cur *store.IndexRun
			if len(entries) > 0 {
				c := entries[0]
				cur = &c
			}
			out = append(out, repoStatus{rep.ID, rep.Name, cur, inProgress})
		}
		writeJSON(w, http.StatusOK, listResponse(out, ""))
	})

	r.Get("/api/repositories", func(w http.ResponseWriter, req *http.Request) {
		repos, err := st.ListRepositories(req.Context())
		if err != nil {
			writeErr(w, "INTERNAL", err.Error())
			return
		}
		if repos == nil {
			repos = []store.Repository{}
		}
		writeJSON(w, http.StatusOK, listResponse(repos, ""))
	})

	r.Get("/api/repositories/{id}/modules", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		ctx := req.Context()
		run, err := st.GetCurrentRun(ctx, id)
		if err == store.ErrNotFound {
			writeErr(w, "NOT_INDEXED", "no complete index run for this repository")
			return
		}
		if err != nil {
			writeErr(w, "INTERNAL", err.Error())
			return
		}
		nodes, edges, err := st.ModuleGraph(ctx, run.Commit)
		if err != nil {
			writeErr(w, "INTERNAL", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, struct {
			Commit   string        `json:"commit"`
			Nodes    []store.ModuleNode `json:"nodes"`
			Edges    []store.ModuleEdge `json:"edges"`
		}{run.Commit, nodes, edges})
	})

	r.Get("/api/repositories/{id}/symbols", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		q := req.URL.Query().Get("q")
		kind := req.URL.Query().Get("type")
		limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
		cursor := req.URL.Query().Get("cursor")
		page, err := search.Search(req.Context(), st, search.Options{
			RepoID: id,
			Q:      q,
			Kind:   kind,
			Limit:  limit,
			Offset: search.OffsetFromCursor(cursor),
		})
		if err != nil {
			writeErr(w, "INTERNAL", err.Error())
			return
		}
		// Pinned list shape: { items, next_cursor }. Each item carries the
		// commit-stamp and evidence reference from the Result.
		items := page.Items
		if items == nil {
			items = []search.Result{}
		}
		writeJSON(w, http.StatusOK, struct {
			Items      []search.Result `json:"items"`
			NextCursor string          `json:"next_cursor"`
			NotFound   bool            `json:"not_found,omitempty"`
		}{items, page.NextCursor, page.NotFound})
	})

	r.Get("/api/symbols/{id}", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		ctx := req.Context()
		sym, err := st.SymbolLatest(ctx, id)
		if err == store.ErrNotFound {
			writeErr(w, "NOT_FOUND", "symbol not found in current index")
			return
		}
		if err != nil {
			writeErr(w, "INTERNAL", err.Error())
			return
		}
		// Definition evidence: file, line range, commit — direct store reads.
		writeJSON(w, http.StatusOK, struct {
			Symbol store.Symbol `json:"symbol"`
			Evidence struct {
				File    string `json:"file"`
				Line    int32  `json:"line"`
				EndLine int32  `json:"endLine"`
				Commit  string `json:"commit"`
			} `json:"evidence"`
		}{sym, struct {
			File    string
			Line    int32
			EndLine int32
			Commit  string
		}{sym.File, sym.StartLine, sym.EndLine, sym.Commit}})
	})

	r.Get("/api/symbols/{id}/dependants", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		ctx := req.Context()
		sym, err := st.SymbolLatest(ctx, id)
		if err == store.ErrNotFound {
			writeErr(w, "NOT_FOUND", "symbol not found in current index")
			return
		}
		if err != nil {
			writeErr(w, "INTERNAL", err.Error())
			return
		}
		edges, err := st.DependentsOf(ctx, sym.Commit, sym.ID)
		if err != nil {
			writeErr(w, "INTERNAL", err.Error())
			return
		}
		if edges == nil {
			edges = []store.Edge{}
		}
		// Each dependant carries the edge's own source fact.
		writeJSON(w, http.StatusOK, listResponse(edges, ""))
	})

	r.Get("/api/commits/{sha}/timeline", func(w http.ResponseWriter, req *http.Request) {
		sha := chi.URLParam(req, "sha")
		tl, err := st.CommitTimeline(req.Context(), sha)
		if err == store.ErrNotFound {
			writeErr(w, "NOT_FOUND", "commit not in the indexed history")
			return
		}
		if err != nil {
			writeErr(w, "INTERNAL", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, struct {
			Info    store.CommitInfo `json:"info"`
			Symbols []store.Symbol   `json:"symbols"`
		}{tl.Info, tl.Symbols})
	})

	r.Get("/api/explain", func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		repoID := req.URL.Query().Get("repository")
		target := req.URL.Query().Get("target")
		if repoID == "" || target == "" {
			writeErr(w, "BAD_REQUEST", "both repository and target query params are required")
			return
		}
		ans, err := explain.Explain(ctx, st, explain.Request{RepoID: repoID, Target: target})
		if err != nil {
			writeErr(w, "INTERNAL", err.Error())
			return
		}
		// Pinned shape: { answer, evidence, coverage, abstained_reason? }.
		evidence := []store.EvidenceRef{}
		for _, e := range ans.Evidence {
			evidence = append(evidence, store.EvidenceRef{
				File:    e.File,
				Line:    e.Line,
				EndLine: e.EndLine,
				Symbol:  e.Symbol,
				Commit:  e.Commit,
				Snippet: e.Snippet,
				Note:    e.Note,
			})
		}
		writeJSON(w, http.StatusOK, struct {
			Answer          string              `json:"answer"`
			Evidence        []store.EvidenceRef `json:"evidence"`
			Coverage        string              `json:"coverage"`
			AbstainedReason string              `json:"abstained_reason,omitempty"`
		}{joinClaims(ans.Claims), evidence, ans.Coverage, ans.AbstainedReason})
	})

	return r
}

// ---------------------------------------------------------------------------
// Auth + rate-limit middleware
// ---------------------------------------------------------------------------

// Auth authenticates every request when an API key is configured, matching
// envx's existing auth env handling. Scoping to a repository happens in the
// route handlers via the {id} path param; this middleware enforces the
// credential.
func Auth(key string, next http.Handler) http.Handler {
	if key == "" {
		return next // CLI-only / local usage: no auth
	}
	fn := func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("Authorization")
		got = strings.TrimPrefix(got, "Bearer ")
		if got != key {
			writeErr(w, "UNAUTHORIZED", "missing or invalid API key")
			return
		}
		next.ServeHTTP(w, r)
	}
	return http.HandlerFunc(fn)
}

// RateLimiter is a per-repository token bucket for the public explain and
// symbol endpoints. The caller wires it in front of the routes it wants to
// throttle.
func RateLimiter(ratePerSecond int, next http.Handler) http.Handler {
	lim := newRateLimiter(ratePerSecond)
	fn := func(w http.ResponseWriter, r *http.Request) {
		repo := strings.TrimSpace(r.URL.Query().Get("repository"))
		if repo == "" {
			repo = chi.URLParam(r, "id")
		}
		if repo == "" {
			repo = "public"
		}
		if !lim.allow(repo) {
			writeErr(w, "RATE_LIMITED", "too many requests for this repository")
			return
		}
		next.ServeHTTP(w, r)
	}
	return http.HandlerFunc(fn)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// listResponse is the pinned { items, next_cursor } envelope.
func listResponse(items any, nextCursor string) map[string]any {
	if items == nil {
		items = []any{}
	}
	return map[string]any{"items": items, "next_cursor": nextCursor}
}

// writeErr emits the pinned { error: { code, message } } envelope.
func writeErr(w http.ResponseWriter, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatusFor(code))
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": code, "message": msg},
	})
}

func httpStatusFor(code string) int {
	switch code {
	case "NOT_FOUND":
		return http.StatusNotFound
	case "NOT_INDEXED":
		return http.StatusNotFound
	case "UNAUTHORIZED":
		return http.StatusUnauthorized
	case "RATE_LIMITED":
		return http.StatusTooManyRequests
	case "BAD_REQUEST":
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// joinClaims renders a grounded answer's claims as the "answer" string.
// Only statements backed by stored evidence are joined — never a guess.
func joinClaims(claims []string) string {
	return strings.Join(claims, " ")
}

// structuredLogger emits one structured log line per request. It
// deliberately logs only request metadata (method, path, status, request-id)
// — no body, no Authorization header, no secrets.
func structuredLogger(next http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		ow := middleware.NewMemResponseWriter()
		start := time.Now()
		next.ServeHTTP(ow, r)
		log.Printf("method=%s path=%s status=%d dur_ms=%d req_id=%s",
			r.Method, r.URL.Path, ow.StatusCode,
			int(time.Since(start).Milliseconds()), middleware.GetReqID(r.Context()))
	}
	return http.HandlerFunc(fn)
}

// ---------------------------------------------------------------------------
// per-repository token bucket (small, allocation-light)
// ---------------------------------------------------------------------------

type rateLimiter struct {
	rate    int // tokens per second
	perRepo map[string]*bucket
}

type bucket struct {
	tokens   int
	lastFill int64
}

func newRateLimiter(ratePerSecond int) *rateLimiter {
	if ratePerSecond <= 0 {
		ratePerSecond = 10
	}
	return &rateLimiter{rate: ratePerSecond, perRepo: map[string]*bucket{}}
}

func (l *rateLimiter) allow(repo string) bool {
	now := time.Now().Unix()
	b, ok := l.perRepo[repo]
	if !ok {
		b = &bucket{tokens: l.rate, lastFill: now}
		l.perRepo[repo] = b
	}
	if elapsed := now - b.lastFill; elapsed > 0 {
		b.tokens = minInt(b.tokens+int(elapsed)*l.rate, l.rate*2)
		b.lastFill = now
	}
	if b.tokens > 0 {
		b.tokens--
		return true
	}
	return false
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
