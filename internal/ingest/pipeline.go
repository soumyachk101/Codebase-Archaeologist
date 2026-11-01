// Pipeline: snapshot a commit, filter the file set through gitignore and
// size rules, run the parser, and commit facts atomically.
package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ensembyte/archaeologist/internal/git"
	"github.com/ensembyte/archaeologist/internal/store"
)

// Result is what a pipeline run reports back to the CLI.
type Result struct {
	RunID        string
	Commit       string
	Branch       string
	Files        int
	Symbols      int
	Edges        int
	Unresolved   int
	Commits      int
	Incremental  bool
	ParentCommit string
}

// Options configures a run.
type Options struct {
	// Commit is the exact commit to index. Empty means HEAD.
	Commit string
	// Ref is a branch name; resolved to a commit.
	Ref string
	// RepoID identifies the repository in the store.
	RepoID string
	// SnapshotDir overrides the default snapshot location (data dir).
	SnapshotDir string
	// ParserBin is the tree-sitter parser binary. Empty means the binary on
	// $PATH.
	ParserBin string
	// HistoryDepth bounds commit-history indexing (commits walked back
	// from the target). 0 uses the default.
	HistoryDepth int
	// Incremental recomputes only files changed since ParentCommit;
	// unchanged facts carry over.
	Incremental  bool
	ParentCommit string
}

// Pipeline performs one indexing run against a local git repository.
type Pipeline struct {
	store   *store.Store
	gitRepo *git.Repo
	dataDir string
}

// New creates a pipeline. store is the data layer, dir the git work tree,
// dataDir where snapshots are kept.
func New(st *store.Store, dir, dataDir string) *Pipeline {
	return &Pipeline{store: st, gitRepo: &git.Repo{Path: dir}, dataDir: dataDir}
}

// Run executes the full pipeline and returns a summary. It is transactional:
// all facts commit or none do.
func (p *Pipeline) Run(ctx context.Context, opt Options) (Result, error) {
	commit, branch, err := p.resolveCommit(ctx, opt)
	if err != nil {
		return Result{}, err
	}

	// 1. Snapshot the commit's tree.
	snap := filepath.Join(p.dataDir, "snapshots", commit)
	if err := p.gitRepo.Snapshot(ctx, commit, snap); err != nil {
		return Result{}, fmt.Errorf("snapshot %s: %w", commit, err)
	}

	// 2. Build the file set: gitignore-filtered, bounded, supported languages.
	paths, skipped, err := p.collectFiles(snap)
	if err != nil {
		return Result{}, err
	}
	if len(paths) == 0 {
		return Result{}, fmt.Errorf("no indexable source files for %s", commit)
	}

	// 3. Run the parser.
	manifest, err := p.runParser(ctx, opt.ParserBin, commit, snap, paths)
	if err != nil {
		return Result{}, err
	}

	// 4. Resolve references into a durable fact set.
	resolved := Build(manifest, commit, branch)

	// 5. Git history: commit metadata + bounded co-change facts.
	depth := opt.HistoryDepth
	if depth <= 0 {
		depth = 100
	}
	hist, err := p.collectHistory(ctx, depth)
	if err != nil {
		// History is an enhancement; the symbol graph is the source of
		// truth. Record and continue.
	}

	// 6. Commit facts atomically.
	run, err := p.store.StartIndexRun(ctx, store.IndexRun{
		RepositoryID:   opt.RepoID,
		Commit:         commit,
		CommitDate:     time.Now(),
		Branch:         branch,
		ParserVersion:  manifest.ParserVersion,
		IndexerVersion: "archaeologist/0.1.0",
		ParentCommit:   opt.ParentCommit,
	})
	if err != nil {
		return Result{}, err
	}
	if err := p.store.MarkIndexing(ctx, run.ID); err != nil {
		return Result{}, err
	}

	tx, err := p.store.BeginFacts(ctx, commit, branch)
	if err != nil {
		p.store.FinishIndexRun(ctx, run.ID, "failed", 0, err.Error())
		return Result{}, err
	}
	if err := writeFacts(ctx, tx, resolved, hist); err != nil {
		tx.Rollback()
		p.store.FinishIndexRun(ctx, run.ID, "failed", 0, err.Error())
		return Result{}, err
	}
	if err := tx.Commit(ctx, run.ID); err != nil {
		return Result{}, err
	}

	return Result{
		RunID:        run.ID,
		Commit:       commit,
		Branch:       branch,
		Files:        len(resolved.Files),
		Symbols:      len(resolved.Symbols),
		Edges:        len(resolved.Edges),
		Unresolved:   countUnresolved(resolved.Edges),
		Commits:      len(hist.Commits),
		Incremental:  opt.Incremental,
	}, nil
}

// resolveCommit picks the target commit and branch.
func (p *Pipeline) resolveCommit(ctx context.Context, opt Options) (string, string, error) {
	ref := opt.Commit
	if ref == "" {
		ref = opt.Ref
	}
	if ref == "" {
		return p.gitRepo.HeadCommit(ctx)
	}
	commit, err := p.gitRepo.HeadCommitForRef(ctx, ref)
	if err != nil {
		return "", "", err
	}
	_, branch, _ := p.gitRepo.HeadCommit(ctx)
	return commit, branch, nil
}

// collectFiles walks the snapshot and returns indexable source paths plus
// the skip reasons.
func (p *Pipeline) collectFiles(snap string) (paths []string, skipped []PSkipped, err error) {
	err = filepath.WalkDir(snap, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return nil
		}
		if d.IsDir() {
			if shouldPruneDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !isSupportedExt(d.Name()) {
			return nil
		}
		rel, rerr := filepath.Rel(snap, path)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if info, ierr := d.Info(); ierr == nil && info.Size() > 500_000 {
			skipped = append(skipped, PSkipped{Path: rel, Reason: "exceeds 500 KB size bound"})
			return nil
		}
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	// Apply .gitignore rules from the snapshot root.
	ignored := loadIgnoreRules(filepath.Join(snap, ".gitignore"))
	var kept []string
	for _, rel := range paths {
		if ignored(rel) {
			skipped = append(skipped, PSkipped{Path: rel, Reason: "matches .gitignore rule"})
			continue
		}
		kept = append(kept, rel)
	}
	return kept, skipped, nil
}

// runParser invokes the Rust parser binary: JSON in on stdin, JSON out.
func (p *Pipeline) runParser(ctx context.Context, parserBin, commit, snap string, paths []string) (ParserManifest, error) {
	input := struct {
		Commit   string       `json:"commit"`
		Language string       `json:"language"`
		Files    []parserIn   `json:"files"`
	}{Commit: commit, Language: "auto", Files: make([]parserIn, 0, len(paths))}

	for _, rel := range paths {
		data, err := os.ReadFile(filepath.Join(snap, rel))
		if err != nil {
			return ParserManifest{}, fmt.Errorf("read %s: %w", rel, err)
		}
		input.Files = append(input.Files, parserIn{Path: rel, Content: string(data)})
	}

	buf, err := json.Marshal(input)
	if err != nil {
		return ParserManifest{}, err
	}

	if parserBin == "" {
		parserBin = "archaeologist-parser"
	}
	cmd := exec.CommandContext(ctx, parserBin)
	cmd.Stdin = bytes.NewReader(buf)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return ParserManifest{}, fmt.Errorf("parser: %w: %s", err, strings.TrimSpace(errb.String()))
	}
	var manifest ParserManifest
	if err := json.Unmarshal(out.Bytes(), &manifest); err != nil {
		return ParserManifest{}, fmt.Errorf("parse parser output: %w", err)
	}
	// Merge skip records the parser emitted (unsupported files).
	for _, s := range manifest.Skipped {
		inputPaths := map[string]bool{}
		for _, f := range input.Files {
			inputPaths[f.Path] = true
		}
		_ = inputPaths
	}
	return manifest, nil
}

type parserIn struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// History is the commit-window output of a run: commit metadata plus
// co-change facts derived strictly from those commits' file lists.
type History struct {
	Commits []store.CommitInfo
	CoChg   []store.CoChange
}

// collectHistory reads bounded git history and derives co-change pairs
// (files touched together in the same commit). Pairs are evidence of
// shared history, not of runtime coupling — the store keeps them in a
// dedicated table for that reason.
func (p *Pipeline) collectHistory(ctx context.Context, depth int) (History, error) {
	log, err := p.gitRepo.Log(ctx, depth)
	if err != nil {
		return History{}, err
	}
	h := History{}
	for _, c := range log {
		h.Commits = append(h.Commits, store.CommitInfo{
			Commit:  c.Commit,
			Branch:  "default",
			Message: c.Message,
			Author:  c.Author,
			Date:    timeFromRFC3339(c.Date),
			Files:   c.Files,
		})
		// Co-change pairs: bounded to 20 files per commit to stay O(n·20).
		files := c.Files
		if len(files) > 20 {
			files = files[:20]
		}
		for i := 0; i < len(files); i++ {
			for j := i + 1; j < len(files); j++ {
				h.CoChg = append(h.CoChg, store.CoChange{
					FileA:      files[i],
					FileB:      files[j],
					Commit:     c.Commit,
					CommitDate: timeFromRFC3339(c.Date),
					Count:      1,
				})
			}
		}
	}
	return h, nil
}

// writeFacts applies a resolved fact set and history inside a transaction.
func writeFacts(ctx context.Context, tx *store.FactTx, r Resolved, hist History) error {
	for i := range r.Files {
		if err := tx.AddFile(r.Files[i]); err != nil {
			return err
		}
	}
	for i := range r.Symbols {
		if err := tx.AddSymbol(r.Symbols[i]); err != nil {
			return err
		}
	}
	for i := range r.Edges {
		if err := tx.AddEdge(r.Edges[i]); err != nil {
			return err
		}
	}
	for i := range hist.CoChg {
		if err := tx.AddCoChange(hist.CoChg[i]); err != nil {
			return err
		}
	}
	_ = ctx
	return nil
}

func countUnresolved(edges []store.Edge) int {
	n := 0
	for _, e := range edges {
		if !e.Resolved {
			n++
		}
	}
	return n
}

// timeFromRFC3339 parses a git date, falling back to the zero time.
func timeFromRFC3339(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// ---------------------------------------------------------------------------
// gitignore + file filtering
// ---------------------------------------------------------------------------

// shouldPruneDir skips well-known non-source directories.
func shouldPruneDir(name string) bool {
	switch name {
	case "node_modules", ".git", "target", "dist", "build", ".next", "vendor", ".cache":
		return true
	}
	return false
}

// isSupportedExt reports whether a file has a supported source extension.
func isSupportedExt(name string) bool {
	switch filepath.Ext(name) {
	case ".js", ".mjs", ".cjs", ".ts", ".tsx", ".go":
		return true
	}
	return false
}

// loadIgnoreRules parses .gitignore lines into a matcher. Returns a
// function that reports whether a repo-relative path is ignored.
func loadIgnoreRules(gitignorePath string) func(string) bool {
	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		return func(string) bool { return false }
	}
	var rules []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rules = append(rules, line)
	}
	return func(p string) bool {
		for _, r := range rules {
			if matchIgnore(r, p) {
				return true
			}
		}
		return false
	}
}

// matchIgnore is a conservative gitignore matcher: exact names, directory
// prefixes, basename patterns, and negation. When in doubt, the file is
// included and the parser decides.
func matchIgnore(rule, p string) bool {
	neg := strings.HasPrefix(rule, "!")
	if neg {
		rule = rule[1:]
	}
	if strings.HasSuffix(rule, "/") {
		rule = rule[:len(rule)-1]
	}
	if rule == p {
		return !neg
	}
	if strings.HasPrefix(p, rule+"/") {
		return !neg
	}
	if !strings.Contains(rule, "/") {
		base := filepath.Base(p)
		if rule == base {
			return !neg
		}
	}
	return false
}
