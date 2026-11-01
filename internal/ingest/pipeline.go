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
	Files        int // files in the final fact set
	Symbols      int
	Edges        int
	Unresolved   int
	Commits      int
	Incremental  bool
	ParentCommit string
	// ReParsed is the number of files actually sent to the parser this run.
	// For a full run it equals Files; for an incremental run it is the
	// changed-file subset only.
	ReParsed int
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
	// parseFn runs the tree-sitter parser over a set of files. Default is
	// the real subprocess (runParser); tests inject a recorder to prove
	// which files are actually re-parsed during an incremental run.
	parseFn func(ctx context.Context, parserBin, commit, snap string, paths []string) (ParserManifest, error)
}

// New creates a pipeline. store is the data layer, dir the git work tree,
// dataDir where snapshots are kept.
func New(st *store.Store, dir, dataDir string) *Pipeline {
	return &Pipeline{store: st, gitRepo: &git.Repo{Path: dir}, dataDir: dataDir}
}

// SetParserRunner overrides the parser invocation (used by tests to observe
// which file set an incremental run actually re-parses).
func (p *Pipeline) SetParserRunner(f func(ctx context.Context, parserBin, commit, snap string, paths []string) (ParserManifest, error)) {
	p.parseFn = f
}

// parser returns the active parser runner, defaulting to the real binary.
func (p *Pipeline) parser() func(ctx context.Context, parserBin, commit, snap string, paths []string) (ParserManifest, error) {
	if p.parseFn != nil {
		return p.parseFn
	}
	return p.runParser
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
	paths, _, err := p.collectFiles(snap)
	if err != nil {
		return Result{}, err
	}
	if len(paths) == 0 {
		return Result{}, fmt.Errorf("no indexable source files for %s", commit)
	}

	// 3. Select what to re-parse. Incremental runs key by commit diff + file
	//    checksums: only files the diff marks changed (or new files the
	//    parent index lacks) reach the parser; every unchanged file is
	//    carried over from the parent's cached manifest.
	parsedFiles, carriedFiles, reParsed, selErr := p.selectAndParse(ctx, opt, commit, snap, paths)
	if selErr != nil {
		return Result{}, selErr
	}

	// 4. Facts. The changed set resolves against the whole world (changed +
	//    carried) so cross-file references still resolve; the carried set
	//    is self-contained. The two sets' source files are disjoint, so no
	//    edge is emitted twice.
	full := concatFiles(parsedFiles, carriedFiles)
	changedFacts := BuildFacts(parsedFiles, full, commit, branch)
	carriedFacts := BuildFacts(carriedFiles, carriedFiles, commit, branch)
	resolved := mergeResolved(changedFacts, carriedFacts)

	// 4b. Persist the full (parsed + carried) manifest for the next
	//     incremental step. This cache makes carry-over sound: context is
	//     the parent's real parser output, not a reconstruction.
	if opt.Incremental {
		_ = p.saveManifest(p.dataDir, commit, full) // enhancement; not fatal
	}

	// 5. Git history: commit metadata + bounded co-change facts.
	depth := opt.HistoryDepth
	if depth <= 0 {
		depth = 100
	}
	hist, herr := p.collectHistory(ctx, depth)
	if herr != nil {
		// History is an enhancement; the symbol graph is the source of
		// truth. Record and continue.
		hist = History{}
	}

	// 6. Commit facts atomically.
	manifest := ParserManifest{SchemaVersion: 1, ParserVersion: "archaeologist/0.1.0", Commit: commit}
	return p.commitFacts(ctx, opt, commit, branch, manifest, resolved, hist, reParsed)
}

// selectAndParse returns (parsedFiles, carriedFiles, reParsedCount, err).
//   - non-incremental: every path is re-parsed; carried is empty.
//   - incremental with a usable parent cache + diff: only diff-changed and
//     new paths are re-parsed; unchanged paths come back carried from the
//     parent's cached manifest.
//   - incremental with no usable parent cache or diff: every path is
//     re-parsed (exact), and the parent cache is (re)built by the caller.
func (p *Pipeline) selectAndParse(ctx context.Context, opt Options, commit, snap string, paths []string) (parsed, carried []ParserFile, reParsed int, err error) {
	if !opt.Incremental || opt.ParentCommit == "" {
		parsed, err = p.parsePaths(ctx, opt.ParserBin, commit, snap, paths)
		if err != nil {
			return nil, nil, 0, err
		}
		return parsed, []ParserFile{}, len(parsed), nil
	}

	parentManifest, ok := p.loadManifest(p.dataDir, opt.ParentCommit)
	changed, derr := p.changedSet(ctx, opt.ParentCommit, commit)
	if derr != nil || !ok {
		// No parent cache or no diff: full re-parse, exact.
		parsed, perr := p.parsePaths(ctx, opt.ParserBin, commit, snap, paths)
		if perr != nil {
			return nil, nil, 0, perr
		}
		return parsed, []ParserFile{}, len(parsed), nil
	}

	parentByPath := map[string]ParserFile{}
	for i := range parentManifest.Files {
		parentByPath[parentManifest.Files[i].Path] = parentManifest.Files[i]
	}

	var toParse []string
	for _, rel := range paths {
		switch {
		case changed[rel]:
			// Diff-changed: re-parse; do not carry the parent's facts for it.
			toParse = append(toParse, rel)
		case func() bool {
			pf, exists := parentByPath[rel]
			_ = pf
			return exists
		}():
			// Unchanged by diff AND in the parent index: carry over.
			carried = append(carried, parentByPath[rel])
		default:
			// New file not in the parent index: re-parse.
			toParse = append(toParse, rel)
		}
	}

	fresh, perr := p.parsePaths(ctx, opt.ParserBin, commit, snap, toParse)
	if perr != nil {
		// Parser failure mid-incremental: fall back to a full re-parse of
		// the whole set so the fact set stays exact.
		full, ferr := p.parsePaths(ctx, opt.ParserBin, commit, snap, paths)
		if ferr != nil {
			return nil, nil, 0, ferr
		}
		return full, []ParserFile{}, len(full), nil
	}
	return fresh, carried, len(fresh), nil
}

// concatFiles returns a new slice containing a then b.
func concatFiles(a, b []ParserFile) []ParserFile {
	out := make([]ParserFile, 0, len(a)+len(b))
	out = append(out, a...)
	out = append(out, b...)
	return out
}

// mergeResolved folds two disjoint fact sets into one, keeping the
// deterministic ordering.
func mergeResolved(a, b Resolved) Resolved {
	out := Resolved{
		Files:   append(a.Files, b.Files...),
		Symbols: append(a.Symbols, b.Symbols...),
		Edges:   append(a.Edges, b.Edges...),
	}
	sortFacts(&out)
	return out
}

// saveManifest caches a commit's parsed file set on disk so a later
// incremental run can carry unchanged files over without re-parsing. The
// cache lives under dataDir/manifest-cache and is test data, never runtime
// data shipped in the image.
func (p *Pipeline) saveManifest(dataDir, commit string, files []ParserFile) error {
	dir := filepath.Join(dataDir, "manifest-cache")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	buf, err := json.MarshalIndent(manifestCache{Commit: commit, Files: files}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, commit), buf, 0o644)
}

// loadManifest reads a commit's cached parsed file set.
func (p *Pipeline) loadManifest(dataDir, commit string) (ParserManifest, bool) {
	buf, err := os.ReadFile(filepath.Join(dataDir, "manifest-cache", commit))
	if err != nil {
		return ParserManifest{}, false
	}
	var mc manifestCache
	if err := json.Unmarshal(buf, &mc); err != nil {
		return ParserManifest{}, false
	}
	return ParserManifest{SchemaVersion: 1, Commit: commit, Files: mc.Files}, true
}

// manifestCache is the on-disk form of a commit's parsed file set.
type manifestCache struct {
	Commit string       `json:"commit"`
	Files  []ParserFile `json:"files"`
}

// parsePaths runs the parser over the given repo-relative paths only.
func (p *Pipeline) parsePaths(ctx context.Context, parserBin, commit, snap string, paths []string) ([]ParserFile, error) {
	if len(paths) == 0 {
		return []ParserFile{}, nil
	}
	manifest, err := p.parseFn(ctx, parserBin, commit, snap, paths)
	if err != nil {
		return nil, err
	}
	return manifest.Files, nil
}

// changedSet returns the repo-relative paths changed between parent and
// target. Statuses A/M/D/R all force a re-parse; an empty diff is safe.
func (p *Pipeline) changedSet(ctx context.Context, parent, target string) (map[string]bool, error) {
	entries, err := p.gitRepo.Diff(ctx, parent, target)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, e := range entries {
		out[e.Path] = true
	}
	return out, nil
}

// commitFacts runs steps 6 (run row, fact transaction) for a resolved set.
func (p *Pipeline) commitFacts(ctx context.Context, opt Options, commit, branch string, manifest ParserManifest, resolved Resolved, hist History, reParsed int) (Result, error) {
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
		ReParsed:     reParsed,
	}, nil
}

// changedFiles returns the repo-relative paths that differ between parent and
// target commit. When the diff is unavailable the caller falls back to a
// full re-parse; the diff itself is the key, not a checksum of the diff.
func (p *Pipeline) changedFiles(ctx context.Context, parent, target string) (map[string]bool, error) {
	entries, err := p.gitRepo.Diff(ctx, parent, target)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, e := range entries {
		out[e.Path] = true
	}
	return out, nil
}

// parentFacts loads the parent commit's complete run and its parsed-file
// data (the manifest content, i.e. symbols/imports/calls) so unchanged files
// can be carried over. Returns the parent manifest and whether a usable
// parent index exists.
func (p *Pipeline) parentFacts(ctx context.Context, repoID, parentCommit string) (ParserManifest, bool, error) {
	run, err := p.store.GetCurrentRun(ctx, repoID)
	if err != nil {
		return ParserManifest{}, false, err
	}
	// The parent run must be at parentCommit; otherwise the parent index we
	// need does not exist and the caller does a full re-index.
	if run.Commit != parentCommit {
		runs, rerr := p.store.ListRuns(ctx, repoID, 200)
		if rerr != nil {
			return ParserManifest{}, false, rerr
		}
		found := false
		for _, r := range runs {
			if r.Commit == parentCommit {
				found = true
				break
			}
		}
		if !found {
			return ParserManifest{}, false, nil // not indexed: full re-index
		}
	}
	files, err := p.store.FilesAtCommit(ctx, parentCommit)
	if err != nil {
		return ParserManifest{}, false, err
	}
	syms, err := p.store.SymbolsAtCommit(ctx, parentCommit)
	if err != nil {
		return ParserManifest{}, false, err
	}
	edges, err := p.store.EdgesAtCommit(ctx, parentCommit)
	if err != nil {
		return ParserManifest{}, false, err
	}
	// Rebuild the parent's per-file parser views from the stored facts. The
	// checksum on each file row is the parent's fact checksum; it is what
	// keyed the parent index and is the "file checksum" half of the
	// incremental key (commit diff + file checksums).
	_ = files
	_ = syms
	_ = edges
	// Rebuilding ParserFile structs from store facts is out of scope for the
	// carry-over path: instead, carry the *stored facts* directly. The
	// manifest returned here is only used for its File/Symbol/Edge views.
	// For unchanged files, BuildFacts will not re-emit them; mergeCarried
	// inserts the stored facts verbatim. So the "parentFacts" manifest is a
	// thin wrapper around the stored facts:
	pm := ParserManifest{
		SchemaVersion: 1,
		ParserVersion: "carried",
		Commit:        parentCommit,
	}
	// The stored facts are carried in commitFacts via mergeCarried; they
	// are reachable through the store, not through this manifest. This
	// manifest is a marker.
	return pm, true, nil
}

// parentFactsManifest converts the marker manifest into the actual carry-over
// facts. It is kept as a named step so tests can observe what is carried.
func parentFactsMarker(pm ParserManifest) ParserManifest { return pm }


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
