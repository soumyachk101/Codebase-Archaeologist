package archtest

import (
	"context"
	"sort"
	"testing"

	"github.com/ensembyte/archaeologist/internal/search"
)

// Search scoring: exact beats fuzzy, kind filter narrows, results are
// commit-stamped, cursor pagination is stable, and a zero-coverage query
// returns an explicit not-found state (never a cross-commit guess).
func TestSearchScoring(t *testing.T) {
	repo := buildGitFixture(t)
	st := openStore(t, repo.root)
	defer st.Close()

	// Index c1; c2-only symbols (e.g. `scale`) must NOT appear at c1.
	res, _ := indexFull(t, st, repo, repo.c1)
	if res.Symbols == 0 {
		t.Fatalf("index produced no symbols")
	}
	commit := res.Commit
	ctx := context.Background()

	// 1. Exact name match outranks a fuzzy/partial match for the same symbol.
	exact, err := search.Search(ctx, st, search.Options{RepoID: repoIDFor(repo.root), Q: "add", Limit: 20})
	if err != nil {
		t.Fatalf("search exact: %v", err)
	}
	if exact.NotFound {
		t.Fatalf("exact search for `add` reported not-found")
	}
	if !firstIs(t, exact.Items, "add") {
		t.Fatalf("exact `add` not ranked first: got %v", names(exact.Items))
	}
	exactScore := exact.Items[0].Score

	fuzzy, err := search.Search(ctx, st, search.Options{RepoID: repoIDFor(repo.root), Q: "ad", Limit: 20})
	if err != nil {
		t.Fatalf("search fuzzy: %v", err)
	}
	var fuzzyScore float64
	foundFuzzyAdd := false
	for _, it := range fuzzy.Items {
		if it.Name == "add" {
			foundFuzzyAdd = true
			fuzzyScore = it.Score
		}
	}
	if !foundFuzzyAdd {
		t.Fatalf("fuzzy `ad` did not surface `add` at all")
	}
	if exactScore <= fuzzyScore {
		t.Fatalf("exact score %.1f should exceed fuzzy/partial score %.1f for the same symbol", exactScore, fuzzyScore)
	}

	// 2. Kind filter narrows to that kind only.
	fnOnly, err := search.Search(ctx, st, search.Options{RepoID: repoIDFor(repo.root), Q: "", Kind: "function", Limit: 200})
	if err != nil {
		t.Fatalf("kind-filtered search: %v", err)
	}
	for _, it := range fnOnly.Items {
		if it.Kind != "function" {
			t.Fatalf("kind=filter leaked a %q result", it.Kind)
		}
	}

	// 3. Every result is commit-stamped and carries a concrete evidence ref.
	for _, it := range fnOnly.Items {
		if it.Commit != commit {
			t.Errorf("result %q stamped commit %q, want %q", it.Name, it.Commit, commit)
		}
		if it.Evidence.Commit != commit {
			t.Errorf("evidence for %q stamped commit %q, want %q", it.Name, it.Evidence.Commit, commit)
		}
		if it.Evidence.File == "" || it.Evidence.Line <= 0 {
			t.Errorf("evidence for %q lacks a concrete file+line: %+v", it.Name, it.Evidence)
		}
	}

	// 4. Cursor pagination is stable: advance until exhausted, no dupes.
	page, err := search.Search(ctx, st, search.Options{RepoID: repoIDFor(repo.root), Q: "", Limit: 3})
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	seen := map[string]bool{}
	dup := 0
	c := page.NextCursor
	for page.NextCursor != "" {
		page, err = search.Search(ctx, st, search.Options{RepoID: repoIDFor(repo.root), Q: "", Limit: 3, Offset: search.OffsetFromCursor(c)})
		if err != nil {
			t.Fatalf("page advance: %v", err)
		}
		for _, it := range page.Items {
			if seen[it.ID] {
				dup++
			}
			seen[it.ID] = true
		}
		c = page.NextCursor
	}
	if dup > 0 {
		t.Errorf("cursor pagination duplicated %d result(s)", dup)
	}

	// 5. A zero-coverage query is an explicit not-found, not a fallback.
	miss, err := search.Search(ctx, st, search.Options{RepoID: repoIDFor(repo.root), Q: "zzqqxx_nomatch", Limit: 20})
	if err != nil {
		t.Fatalf("not-found search: %v", err)
	}
	if !miss.NotFound || len(miss.Items) != 0 {
		t.Fatalf("zero-coverage query should be NotFound with no items, got NotFound=%v items=%d", miss.NotFound, len(miss.Items))
	}

	// 6. A c2-only symbol must be absent from the c1 index (no cross-commit
	//    guessing): `scale` exists only in c2.
	scale, err := search.Search(ctx, st, search.Options{RepoID: repoIDFor(repo.root), Q: "scale", Limit: 20})
	if err != nil {
		t.Fatalf("search c2-only symbol: %v", err)
	}
	if !scale.NotFound {
		t.Errorf("`scale` is a c2-only export; the c1 index must report not-found, not a fabricated match")
	}
}

func firstIs(t *testing.T, items []search.Result, name string) bool {
	t.Helper()
	if len(items) == 0 {
		return false
	}
	return items[0].Name == name
}

func names(items []search.Result) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Name)
	}
	sort.Strings(out)
	return out
}
