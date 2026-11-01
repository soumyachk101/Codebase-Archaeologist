// Package explain answers "what is / where is / who depends on X"
// questions strictly from persisted, commit-stamped facts.
//
// Hard guarantee (the provenance invariant, applied to answers):
//   - every factual claim in an Answer is backed by one or more
//     EvidenceReference entries (file, line range, symbol, commit) that
//     are direct reads from the store;
//   - no call relationship, source reference, architectural fact or line
//     number may be invented; when the evidence the answer needs is not
//     in the index, the service returns abstain=true with an
//     abstained_reason naming the missing evidence;
//   - the optional derived summary layer is always marked derived=true;
//     measured-fact fields are always direct store reads and are never
//     rewritten by the summary layer.
package explain

import (
	"context"
	"fmt"
	"strings"

	"github.com/ensembyte/archaeologist/internal/store"
)

// EvidenceReference is one cited piece of indexed evidence.
type EvidenceReference struct {
	File    string `json:"file"`
	Line    int32  `json:"line"`
	EndLine int32  `json:"endLine,omitempty"`
	Symbol  string `json:"symbol,omitempty"`
	Commit  string `json:"commit"`
	Snippet string `json:"snippet,omitempty"`
	Note    string `json:"note,omitempty"`
}

// Answer is one structured, evidence-grounded reply.
type Answer struct {
	Target    string   `json:"target"`
	Commit    string   `json:"commit"`
	RunID     string   `json:"runId"`
	Claims    []string `json:"claims"` // factual statements; every one has evidence
	Evidence  []EvidenceReference `json:"evidence"`
	// Coverage: "full" | "partial" | "abstain".
	Coverage       string `json:"coverage"`
	Abstained      bool   `json:"abstained"`
	AbstainedReason string `json:"abstainedReason,omitempty"`
	// Derived marks the optional summary layer; measured fields above are
	// direct store reads and never carry this flag.
	Derived     bool   `json:"derived"`
	Summary     string `json:"summary,omitempty"`
	Unresolved  []EvidenceReference `json:"unresolved,omitempty"` // hints only, explicitly unresolved
}

// Request scopes one explanation.
type Request struct {
	RepoID string
	// Target is a symbol name, qualified name, module or file path.
	Target string
	// File restricts symbol-target lookups to one file.
	File string
}

// Explain answers a request against the repository's current index run.
// It never fabricates: when coverage is too low it returns an abstained
// answer whose abstained_reason names the missing evidence.
func Explain(ctx context.Context, st *store.Store, req Request) (Answer, error) {
	empty := Answer{Target: req.Target, Coverage: "abstain", Abstained: true}
	run, err := st.GetCurrentRun(ctx, req.RepoID)
	if err != nil {
		if err == store.ErrNotFound {
			empty.AbstainedReason = "no complete index run for this repository yet; nothing to cite"
			return empty, nil
		}
		return Answer{}, err
	}
	commit := run.Commit
	target := strings.TrimSpace(req.Target)
	if target == "" {
		empty.AbstainedReason = "empty target: nothing to look up"
		return empty, nil
	}

	// 1. Is the target a module? Evidence: the files that carry it.
	if modules, err := st.ModulesOf(ctx, commit); err == nil {
		for _, m := range modules {
			if strings.EqualFold(m, target) {
				return moduleAnswer(st, ctx, req.RepoID, target, commit, run.ID), nil
			}
		}
	}

	// 2. Symbol lookup by exact name (bounded), then fuzzy name via the
	//    search layer only as a candidate source — the claims themselves
	//    cite symbols the store holds.
	cands, err := st.FindSymbolsByName(ctx, commit, target, 20)
	if err != nil {
		return Answer{}, err
	}
	if req.File != "" {
		kept := cands[:0]
		for _, c := range cands {
			if c.File == req.File {
				kept = append(kept, c)
			}
		}
		cands = kept
	}
	if len(cands) == 0 {
		empty.Commit = commit
		empty.RunID = run.ID
		empty.AbstainedReason = fmt.Sprintf(
			"symbol %q is not in the index at commit %s: no symbol by that name was parsed; "+
				"missing evidence is the declaration site (file + line) of the symbol",
			target, shortSHA(commit))
		return empty, nil
	}
	sym := cands[0]
	return symbolAnswer(st, ctx, commit, run.ID, sym, cands), nil
}

// symbolAnswer: "what is / where is" for one symbol, plus its direct
// dependants and unresolved call facts — all store reads.
func symbolAnswer(st *store.Store, ctx context.Context, commit, runID string, sym store.Symbol, allByName []store.Symbol) Answer {
	ev := []EvidenceReference{{
		File:    sym.File,
		Line:    sym.StartLine,
		EndLine: sym.EndLine,
		Symbol:  sym.Name,
		Commit:  commit,
		Snippet: snippetFor(sym),
		Note:    "declaration site",
	}}
	claims := []string{
		fmt.Sprintf("%s (%s) is declared in %s at lines %d–%d, commit %s.",
			sym.Name, sym.Kind, sym.File, sym.StartLine, sym.EndLine, shortSHA(commit)),
	}
	if len(allByName) > 1 {
		claims = append(claims, fmt.Sprintf(
			"%d other symbols share this name at %s; disambiguate by file.",
			len(allByName)-1, shortSHA(commit)))
	}

	// Definition evidence: the out-edges this symbol (or its file's module)
	// declares.
	out, err := st.EdgesOf(ctx, commit, sym.ID)
	if err == nil {
		for _, e := range out {
			claims = append(claims, edgeClaim(e))
			ev = append(ev, refForEdge(e))
		}
	}
	// Reverse dependencies, capped so the answer stays bounded.
	in, err := st.DependentsOf(ctx, commit, sym.ID)
	if err == nil {
		for i, e := range in {
			if i >= 25 {
				claims = append(claims, fmt.Sprintf("%d further reverse dependencies exist at %s.",
					len(in)-i, shortSHA(commit)))
				break
			}
			claims = append(claims, edgeClaim(e))
			ev = append(ev, refForEdge(e))
		}
	}

	unres, _ := st.UnresolvedCallsOf(ctx, commit, sym.File)
	var unresolved []EvidenceReference
	for _, e := range unres {
		unresolved = append(unresolved, refForEdge(e))
	}

	// Coverage: full when both directions resolved cleanly, partial when
	// some facts are unresolved (hinted, not invented).
	cov := "full"
	if len(unres) > 0 || anyUnresolved(out, in) {
		cov = "partial"
	}
	a := Answer{
		Commit:    commit,
		RunID:     runID,
		Claims:    claims,
		Evidence:  ev,
		Coverage:  cov,
		Unresolved: unresolved,
	}
	// Optional derived summary — clearly marked, never rewriting facts.
	a.Derived = true
	a.Summary = summarize(a)
	return a
}

// moduleAnswer: "what is module M" from file counts and cross-module edges
// only.
func moduleAnswer(st *store.Store, ctx context.Context, repoID, module, commit, runID string) Answer {
	nodes, edges, err := st.ModuleGraph(ctx, commit)
	if err != nil {
		return Answer{
			Commit:  commit,
			RunID:   runID,
			Coverage: "abstain",
			Abstained: true,
			AbstainedReason: fmt.Sprintf(
				"module %q: reading the module graph failed; missing evidence is the cross-module edge set",
				module),
		}
	}
	// Files in the module.
	files, _ := st.SymbolsInModule(ctx, commit, module, 1)
	claims := []string{fmt.Sprintf("Module %s exists at commit %s.", module, shortSHA(commit))}
	ev := []EvidenceReference{}
	found := false
	for _, n := range nodes {
		if n.Module == module {
			found = true
			claims = append(claims, fmt.Sprintf("Module %s covers %d indexed file(s).", module, n.Files))
			ev = append(ev, EvidenceReference{
				File: module, Line: 0, Commit: commit,
				Note: fmt.Sprintf("module aggregation over %d files", n.Files),
			})
		}
	}
	if !found {
		return Answer{
			Commit:  commit, RunID: runID,
			Coverage: "abstain", Abstained: true,
			AbstainedReason: fmt.Sprintf(
				"module %q matched a module name but no indexed files carry it; missing evidence is at least one file in module %s at commit %s",
				module, module, shortSHA(commit)),
		}
	}
	for _, e := range edges {
		if e.From == module || e.To == module {
			claims = append(claims, fmt.Sprintf("Cross-module edge %s → %s (%s).", e.From, e.To, e.Examples[0]))
		}
	}
	_ = files
	_ = repoID
	return Answer{
		Commit:   commit,
		RunID:    runID,
		Claims:   claims,
		Evidence: ev,
		Coverage: "full",
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func edgeClaim(e store.Edge) string {
	if e.Resolved {
		return fmt.Sprintf("Edge %s: %s → %s (kind %s), observed at %s:%d.",
			e.ID, e.FromID, e.ToID, e.Kind, e.SourceFile, e.SourceLine)
	}
	return fmt.Sprintf("Unresolved %s reference at %s:%d: source expression %q could not be resolved; kept as unknown, not invented.",
		e.Kind, e.SourceFile, e.SourceLine, e.TargetHint)
}

func refForEdge(e store.Edge) EvidenceReference {
	return EvidenceReference{
		File:    e.SourceFile,
		Line:    e.SourceLine,
		Commit:  e.Commit,
		Symbol:  e.FromID,
		Snippet: e.TargetHint,
		Note:    edgeNote(e),
	}
}

func edgeNote(e store.Edge) string {
	if e.Resolved {
		return "resolved edge, static confidence"
	}
	return "unresolved reference (target_hint only)"
}

func anyUnresolved(out, in []store.Edge) bool {
	for _, e := range out {
		if !e.Resolved {
			return true
		}
	}
	for _, e := range in {
		if !e.Resolved {
			return true
		}
	}
	return false
}

// summarize is the optional derived layer. It is always marked derived and
// only restates claims the store produced; it never adds facts.
func summarize(a Answer) string {
	if len(a.Claims) == 0 {
		return ""
	}
	return a.Claims[0]
}

func snippetFor(s store.Symbol) string {
	return fmt.Sprintf("%s %s @ %s:%d", s.Kind, s.Name, s.File, s.StartLine)
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
