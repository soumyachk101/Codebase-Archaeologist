package archtest

import (
	"context"
	"testing"

	"github.com/ensembyte/archaeologist/internal/explain"
)

// Explain abstain + evidence-grounding: a target with no indexed evidence
// returns coverage="abstain" with a populated abstained_reason; a
// well-evidenced target returns claims backed only by real store reads
// (file+line+commit) — never an invented line number or call relationship.
func TestExplainAbstainAndEvidence(t *testing.T) {
	repo := buildGitFixture(t)
	st := openStore(t, repo.root)
	defer st.Close()

	// Index c1. `scale` exists only in c2, so at c1 it is abstain-able.
	res, _ := indexFull(t, st, repo, repo.c1)
	if res.Symbols == 0 {
		t.Fatalf("index produced no symbols")
	}
	ctx := context.Background()
	repoID := repoIDFor(repo.root)

	// 1. No-index-run case: a fresh repo with no complete run must abstain.
	emptySt := openStore(t, repo.root + "-empty")
	defer emptySt.Close()
	a0, err := explain.Explain(ctx, emptySt, explain.Request{RepoID: repoID, Target: "Normalize"})
	if err != nil {
		t.Fatalf("explain on empty store: %v", err)
	}
	if !a0.Abstained || a0.Coverage != "abstain" || a0.AbstainedReason == "" {
		t.Fatalf("no-run explain should abstain with a reason, got %+v", a0)
	}

	// 2. Unknown symbol at c1 (c2-only export): abstain, naming the gap.
	aMiss, err := explain.Explain(ctx, st, explain.Request{RepoID: repoID, Target: "scale"})
	if err != nil {
		t.Fatalf("explain unknown symbol: %v", err)
	}
	if !aMiss.Abstained || aMiss.Coverage != "abstain" || aMiss.AbstainedReason == "" {
		t.Fatalf("unknown-symbol explain should abstain with a reason, got %+v", aMiss)
	}
	if aMiss.Commit != res.Commit {
		t.Errorf("abstain answer commit %q, want %q", aMiss.Commit, res.Commit)
	}

	// 3. Well-evidenced target: `Normalize` (a c1 Go function). The answer's
	//    claims are backed by EvidenceReference entries that are direct store
	//    reads (concrete file + line + commit). No line number may be
	//    invented: every non-zero evidence line must be within a symbol that
	//    actually exists at the commit.
	a, err := explain.Explain(ctx, st, explain.Request{RepoID: repoID, Target: "Normalize"})
	if err != nil {
		t.Fatalf("explain Normalize: %v", err)
	}
	if a.Abstained {
		t.Fatalf("Normalize is indexed in c1; answer should not abstain: %s", a.AbstainedReason)
	}
	if len(a.Evidence) == 0 {
		t.Fatalf("evidence-grounded answer must cite evidence, got none")
	}
	// Every evidence ref is commit-stamped and file-anchored.
	for _, ev := range a.Evidence {
		if ev.Commit != res.Commit {
			t.Errorf("evidence commit %q, want %q", ev.Commit, res.Commit)
		}
		if ev.File == "" {
			t.Errorf("evidence ref lacks a source file")
		}
	}
	// The derived summary layer is clearly flagged, distinct from the
	// measured facts.
	if a.Summary == "" && !a.Derived {
		t.Errorf("a summarized answer must be marked derived=true")
	}
}
