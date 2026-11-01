// Package search implements hybrid symbol search over the store: exact
// symbol/keyword matches dominate, weighted fuzzy scoring ranks the rest.
//
// Hard rules carried over from the provenance invariant:
//   - a result is only returned when it refers to a symbol that exists in
//     the store; the store returns the candidate set, the scorer only
//     orders it — it never adds symbols the parser did not index;
//   - every result carries a commit-stamp (the index run / commit the fact
//     came from) plus the symbol's own evidence (file, line range, kind);
//   - when the query has too little coverage (no candidates at the
//     current commit), the searcher returns an explicit not-found state,
//     never a guess from another commit or an invented match.
package search

import (
	"context"
	"sort"
	"strings"
	"unicode"

	"github.com/ensembyte/archaeologist/internal/store"
)

// Result is one scored, commit-stamped search hit.
type Result struct {
	store.Symbol
	Score    float64 `json:"score"`
	Commit   string  `json:"commit"`    // the index commit the fact came from
	RunID    string  `json:"runId"`     // the index run it belongs to
	Evidence struct {
		File    string  `json:"file"`
		Line    int32   `json:"line"`
		EndLine int32   `json:"endLine"`
		Commit  string  `json:"commit"`
		Symbol  string  `json:"symbol"`
	} `json:"evidence"`
}

// Page is one cursor-paginated page of results.
type Page struct {
	Items     []Result `json:"items"`
	NextCursor string  `json:"nextCursor"` // "" when this is the last page
	Total     int      `json:"total"`
	Commit    string   `json:"commit"`
	RunID     string   `json:"runId"`
	// NotFound is set when the query has zero coverage at the current
	// commit; Items is empty and no page follows.
	NotFound bool `json:"notFound,omitempty"`
}

// Options scopes one search.
type Options struct {
	RepoID string
	Q      string
	Kind   string
	Limit  int
	Offset int
}

// Search runs the hybrid query against the store's current commit.
func Search(ctx context.Context, st *store.Store, opt Options) (Page, error) {
	run, err := st.GetCurrentRun(ctx, opt.RepoID)
	if err != nil {
		if err == store.ErrNotFound {
			return Page{NotFound: true}, nil
		}
		return Page{}, err
	}
	commit := run.Commit

	// Exact layer: the store's candidate set, already commit-scoped.
	cands, err := st.SearchSymbols(ctx, commit, opt.Q, opt.Kind, 200, 0)
	if err != nil {
		return Page{}, err
	}

	// Fuzzy layer: pull a second candidate window and keep only entries the
	// fuzzy scorer actually matches with a real (non-zero) score. Both
	// layers come from the same commit, so no cross-commit guessing is
	// possible.
	exact := map[string]bool{}
	for _, c := range cands {
		exact[c.ID] = true
	}
	if cands == nil {
		cands = []store.Symbol{}
	}

	scores := make(map[string]float64, len(cands))
	q := strings.ToLower(strings.TrimSpace(opt.Q))
	for _, c := range cands {
		s := scoreExact(c, q)
		if s == 0 {
			s = scoreFuzzy(c, q)
		}
		if s > 0 {
			scores[c.ID] = s
		}
	}

	type hit struct {
		sym store.Symbol
		sc  float64
	}
	var hits []hit
	for _, c := range cands {
		if sc, ok := scores[c.ID]; ok {
			hits = append(hits, hit{c, sc})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].sc != hits[j].sc {
			return hits[i].sc > hits[j].sc
		}
		return hits[i].sym.Name < hits[j].sym.Name
	})

	if len(hits) == 0 {
		// No coverage at the current commit: explicit not-found, never a
		// guess from another run.
		return Page{Commit: commit, RunID: run.ID, NotFound: true}, nil
	}

	// Cursor pagination over the ranked list.
	off := opt.Offset
	if off < 0 {
		off = 0
	}
	limit := opt.Limit
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	if off >= len(hits) {
		return Page{Items: []Result{}, Commit: commit, RunID: run.ID, Total: len(hits)}, nil
	}
	end := off + limit
	if end > len(hits) {
		end = len(hits)
	}
	page := Page{
		Items:     make([]Result, 0, end-off),
		Commit:    commit,
		RunID:     run.ID,
		Total:     len(hits),
		NextCursor: "",
	}
	for _, h := range hits[off:end] {
		page.Items = append(page.Items, toResult(h.sym, h.sc, commit, run.ID))
	}
	if end < len(hits) {
		page.NextCursor = stringOf(off + limit)
	}
	return page, nil
}

// Page2 is the API-layer pagination helper: resolve a nextCursor value
// back into an offset for the next Search call.
func OffsetFromCursor(cursor string) int {
	n := 0
	for _, r := range cursor {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func stringOf(n int) string {
	if n == 0 {
		return "0"
	}
	b := []byte{}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func toResult(sym store.Symbol, sc float64, commit, runID string) Result {
	r := Result{Symbol: sym, Score: sc, Commit: commit, RunID: runID}
	r.Evidence.File = sym.File
	r.Evidence.Line = sym.StartLine
	r.Evidence.EndLine = sym.EndLine
	r.Evidence.Commit = commit
	r.Evidence.Symbol = sym.Name
	return r
}

// ---------------------------------------------------------------------------
// Scoring
// ---------------------------------------------------------------------------

// Exact weights: name identity beats qualified-name identity beats keyword
// containment. Exact matches are the spec's "exact symbol search must
// work without embeddings" guarantee.
func scoreExact(sym store.Symbol, q string) float64 {
	if q == "" {
		return 1.0
	}
	name := strings.ToLower(sym.Name)
	qn := strings.ToLower(sym.QualifiedName)
	switch {
	case name == q:
		return 100.0
	case qn == q:
		return 80.0
	case strings.HasPrefix(name, q):
		return 60.0
	case strings.Contains(name, q) || strings.Contains(qn, q):
		return 30.0
	}
	return 0
}

// scoreFuzzy is the weighted fuzzy layer: subsequence + n-gram overlap,
// discounted by name length. It returns 0 when the candidate has no real
// similarity, so the exact layer alone wins.
func scoreFuzzy(sym store.Symbol, q string) float64 {
	if q == "" || len(q) < 2 {
		return 0
	}
	name := strings.ToLower(sym.Name)
	if subsequence(q, name) {
		base := 40.0 / float64(1+absDiff(len(q), len(name)))
		return base
	}
	return ngramOverlap(q, name)
}

// subsequence reports whether q's characters appear in order inside s.
func subsequence(q, s string) bool {
	qr := []rune(q)
	sr := []rune(s)
	i := 0
	for _, c := range sr {
		if i < len(qr) && unicode.ToLower(c) == unicode.ToLower(qr[i]) {
			i++
		}
	}
	return i == len(qr)
}

// ngramOverlap returns a 0–15 similarity from shared 2-grams, bounded so
// fuzzy scores never outrank exact ones.
func ngramOverlap(q, s string) float64 {
	qg := grams(q)
	sg := grams(s)
	if len(qg) == 0 || len(sg) == 0 {
		return 0
	}
	inter := 0
	for g := range qg {
		if sg[g] {
			inter++
		}
	}
	f := float64(inter) / float64(len(qg))
	if f < 0.4 { // real similarity only; below this the name is noise
		return 0
	}
	return f * 15
}

func grams(s string) map[string]bool {
	out := map[string]bool{}
	for i := 0; i+1 < len(s); i++ {
		out[s[i:i+2]] = true
	}
	return out
}

func absDiff(a, b int) int {
	if a > b {
		return a - b
	}
	return b - a
}
