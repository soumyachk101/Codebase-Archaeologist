package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrNotFound is returned when a looked-up entity does not exist.
var ErrNotFound = errors.New("not found")

// rfc3339 normalizes times into a stable, parseable string for storage.
func rfc3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// parseTime is the inverse of rfc3339, tolerating empty values.
func parseTime(v string) *time.Time {
	if v == "" {
		return nil
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return &t
	}
	return nil
}

// b2i / i2b convert between Go booleans and the on-disk 0/1 form.
func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ---------------------------------------------------------------------------
// Repositories
// ---------------------------------------------------------------------------

// CreateRepository upserts a repository identity.
func (s *Store) CreateRepository(ctx context.Context, r Repository) error {
	_, err := s.exec(ctx,
		`INSERT INTO repositories (id, name, origin, root, created_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT (id) DO UPDATE SET name = excluded.name`,
		r.ID, r.Name, r.Origin, r.Root, rfc3339(r.CreatedAt))
	return err
}

// GetRepository fetches one repository by ID.
func (s *Store) GetRepository(ctx context.Context, id string) (Repository, error) {
	var r Repository
	var createdAt string
	err := s.row(ctx,
		`SELECT id, name, origin, root, created_at FROM repositories WHERE id = ?`, id,
	).Scan(&r.ID, &r.Name, &r.Origin, &r.Root, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Repository{}, ErrNotFound
	}
	if err != nil {
		return Repository{}, err
	}
	if t := parseTime(createdAt); t != nil {
		r.CreatedAt = *t
	}
	return r, nil
}

// ListRepositories returns all repositories, newest first.
func (s *Store) ListRepositories(ctx context.Context) ([]Repository, error) {
	rows, err := s.rows(ctx,
		`SELECT id, name, origin, root, created_at FROM repositories ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Repository
	for rows.Next() {
		var r Repository
		var createdAt string
		if err := rows.Scan(&r.ID, &r.Name, &r.Origin, &r.Root, &createdAt); err != nil {
			return nil, err
		}
		if t := parseTime(createdAt); t != nil {
			r.CreatedAt = *t
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DeleteRepository removes a repository and everything indexed for it.
// Facts carry the commit, not the repo, so the caller purges runs + commit
// data first; this deletes the identity row.
func (s *Store) DeleteRepository(ctx context.Context, id string) error {
	_, err := s.exec(ctx, `DELETE FROM repositories WHERE id = ?`, id)
	return err
}

// ---------------------------------------------------------------------------
// Index runs
// ---------------------------------------------------------------------------

// StartIndexRun records the start of a new indexing attempt.
func (s *Store) StartIndexRun(ctx context.Context, run IndexRun) (IndexRun, error) {
	if run.ID == "" {
		run.ID = newRunID(run.Commit)
	}
	run.Status = "pending"
	if _, err := s.exec(ctx,
		`INSERT INTO index_runs
		 (id, repository_id, commit, commit_date, branch, parser_version,
		  status, indexer_version, parent_commit)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.RepositoryID, run.Commit, rfc3339(run.CommitDate), run.Branch,
		run.ParserVersion, run.Status, run.IndexerVersion, run.ParentCommit,
	); err != nil {
		return run, err
	}
	return run, nil
}

// MarkIndexing moves a pending run to the "indexing" state.
func (s *Store) MarkIndexing(ctx context.Context, runID string) error {
	_, err := s.exec(ctx, `UPDATE index_runs SET status = ? WHERE id = ?`, "indexing", runID)
	return err
}

// FinishIndexRun marks a run complete (with file count) or failed. Facts
// are only visible for runs whose status is "complete", so a partial batch
// can never leak.
func (s *Store) FinishIndexRun(ctx context.Context, runID, status string, indexedFiles int, errMsg string) error {
	if status == "complete" {
		_, err := s.exec(ctx,
			`UPDATE index_runs SET status = ?, indexed_at = ?, indexed_files = ? WHERE id = ?`,
			status, rfc3339(time.Now()), indexedFiles, runID)
		return err
	}
	_, err := s.exec(ctx,
		`UPDATE index_runs SET status = ?, error_message = ? WHERE id = ?`,
		status, errMsg, runID)
	return err
}

func (s *Store) scanRun(rows *sql.Row) (IndexRun, error) {
	var run IndexRun
	var commitDate, indexedAt string
	err := rows.Scan(&run.ID, &run.Commit, &commitDate, &run.Branch, &run.ParserVersion,
		&run.Status, &indexedAt, &run.IndexedFiles, &run.ErrorMessage, &run.ParentCommit)
	if err != nil {
		return run, err
	}
	if t := parseTime(commitDate); t != nil {
		run.CommitDate = *t
	}
	if t := parseTime(indexedAt); t != nil {
		run.IndexedAt = t
	}
	return run, nil
}

// GetCurrentRun returns the newest complete run for a repository.
func (s *Store) GetCurrentRun(ctx context.Context, repoID string) (IndexRun, error) {
	run, err := s.scanRun(s.row(ctx,
		`SELECT id, commit, commit_date, branch, parser_version, status,
		        indexed_at, indexed_files, error_message, parent_commit
		 FROM index_runs
		 WHERE repository_id = ? AND status = 'complete'
		 ORDER BY indexed_at DESC LIMIT 1`, repoID))
	if errors.Is(err, sql.ErrNoRows) {
		return IndexRun{}, ErrNotFound
	}
	if err != nil {
		return IndexRun{}, err
	}
	run.RepositoryID = repoID
	return run, nil
}

// LastRun returns the most recent run regardless of status, for
// "index in progress" states.
func (s *Store) LastRun(ctx context.Context, repoID string) (IndexRun, error) {
	run, err := s.scanRun(s.row(ctx,
		`SELECT id, commit, commit_date, branch, parser_version, status,
		        indexed_at, indexed_files, error_message, parent_commit
		 FROM index_runs
		 WHERE repository_id = ?
		 ORDER BY indexed_at DESC LIMIT 1`, repoID))
	if errors.Is(err, sql.ErrNoRows) {
		return IndexRun{}, ErrNotFound
	}
	if err != nil {
		return IndexRun{}, err
	}
	run.RepositoryID = repoID
	return run, nil
}

// ListRuns returns the most recent N complete runs for a repository,
// newest first.
func (s *Store) ListRuns(ctx context.Context, repoID string, limit int) ([]IndexRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.rows(ctx,
		`SELECT id, commit, commit_date, branch, parser_version, status,
		        indexed_at, indexed_files, error_message, parent_commit
		 FROM index_runs
		 WHERE repository_id = ? AND status = 'complete'
		 ORDER BY indexed_at DESC LIMIT ` + fmt.Sprint(limit), repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IndexRun
	for rows.Next() {
		var run IndexRun
		var commitDate, indexedAt string
		if err := rows.Scan(&run.ID, &run.Commit, &commitDate, &run.Branch,
			&run.ParserVersion, &run.Status, &indexedAt, &run.IndexedFiles,
			&run.ErrorMessage, &run.ParentCommit); err != nil {
			return nil, err
		}
		if t := parseTime(commitDate); t != nil {
			run.CommitDate = *t
		}
		if t := parseTime(indexedAt); t != nil {
			run.IndexedAt = t
		}
		run.RepositoryID = repoID
		out = append(out, run)
	}
	return out, rows.Err()
}

// RunStatus summarizes freshness for the repository overview screen.
type RunStatus struct {
	Current   IndexRun `json:"current"`
	Branch    string   `json:"branch"`
	Stale     bool     `json:"stale"`
	InProgress bool    `json:"inProgress"`
	LastError string   `json:"lastError,omitempty"`
}

// StatusOf builds a freshness summary. Stale=true when the latest run on a
// branch is not the newest indexed commit we know about — i.e. a re-index is
// pending because the branch advanced.
func (s *Store) StatusOf(ctx context.Context, repoID, headCommit string) (RunStatus, error) {
	var st RunStatus
	current, err := s.GetCurrentRun(ctx, repoID)
	if err != nil {
		return st, err
	}
	st.Current = current
	st.Branch = current.Branch
	// If the caller supplies the live head commit and it differs from the
	// indexed commit, the index is stale.
	if headCommit != "" && headCommit != current.Commit {
		st.Stale = true
	}
	if last, err := s.LastRun(ctx, repoID); err == nil && last.Status != "complete" {
		st.InProgress = true
		st.LastError = last.ErrorMessage
	}
	return st, nil
}

// ---------------------------------------------------------------------------
// Facts: files, symbols, edges — written inside one transaction per index
// run, stamped with the run's commit.
// ---------------------------------------------------------------------------

// FactTx batches fact writes for one index run and applies them atomically.
type FactTx struct {
	s         *Store
	tx        *sql.Tx
	commit    string
	branch    string
	fileCount int
}

// BeginFacts opens the write transaction for a completed parse batch.
func (s *Store) BeginFacts(ctx context.Context, commit, branch string) (*FactTx, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &FactTx{s: s, tx: tx, commit: commit, branch: branch}, nil
}

// AddFile upserts a source file fact.
func (f *FactTx) AddFile(file File) error {
	exports, _ := json.Marshal(file.Exports)
	_, err := f.s.execTx(context.Background(), f.tx,
		`INSERT INTO files (id, commit, path, language, module, checksum, size_bytes,
		                    exports, top_symbols, first_commit, last_commit)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (id, commit) DO UPDATE SET
		    language = excluded.language,
		    module = excluded.module,
		    checksum = excluded.checksum,
		    size_bytes = excluded.size_bytes,
		    exports = excluded.exports,
		    top_symbols = excluded.top_symbols,
		    first_commit = excluded.first_commit,
		    last_commit = excluded.last_commit`,
		file.ID, f.commit, file.Path, file.Language, file.Module, file.Checksum,
		file.SizeBytes, string(exports), file.TopSymbols, file.FirstCommit, file.LastCommit)
	if err == nil {
		f.fileCount++
	}
	return err
}

// AddSymbol upserts one symbol fact.
func (f *FactTx) AddSymbol(sym Symbol) error {
	_, err := f.s.execTx(context.Background(), f.tx,
		`INSERT INTO symbols (id, commit, file, kind, name, qualified_name,
		                      language, start_line, end_line, start_byte, end_byte,
		                      exported, is_test, parent, module)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (id, commit) DO UPDATE SET
		    file = excluded.file,
		    qualified_name = excluded.qualified_name,
		    start_line = excluded.start_line,
		    end_line = excluded.end_line,
		    start_byte = excluded.start_byte,
		    end_byte = excluded.end_byte,
		    exported = excluded.exported,
		    is_test = excluded.is_test,
		    parent = excluded.parent,
		    module = excluded.module`,
		sym.ID, f.commit, sym.File, sym.Kind, sym.Name, sym.QualifiedName,
		sym.Language, sym.StartLine, sym.EndLine, sym.StartByte, sym.EndByte,
		b2i(sym.Exported), b2i(sym.IsTest), sym.Parent, sym.Module)
	return err
}

// AddEdge upserts one graph edge.
func (f *FactTx) AddEdge(e Edge) error {
	_, err := f.s.execTx(context.Background(), f.tx,
		`INSERT INTO edges (id, commit, from_id, to_id, kind, resolved, confidence,
		                   target_hint, source_file, source_line)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (id, commit) DO UPDATE SET
		    to_id = excluded.to_id,
		    resolved = excluded.resolved,
		    confidence = excluded.confidence,
		    target_hint = excluded.target_hint`,
		e.ID, f.commit, e.FromID, e.ToID, e.Kind, b2i(e.Resolved),
		e.Confidence, e.TargetHint, e.SourceFile, e.SourceLine)
	return err
}

// AddCoChange records historical co-change evidence.
func (f *FactTx) AddCoChange(c CoChange) error {
	_, err := f.s.execTx(context.Background(), f.tx,
		`INSERT INTO co_changes (file_a, file_b, commit, commit_date, count)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT (file_a, file_b, commit) DO UPDATE SET count = excluded.count`,
		c.FileA, c.FileB, c.Commit, rfc3339(c.CommitDate), c.Count)
	return err
}

// Commit applies all facts atomically and completes the index run.
func (f *FactTx) Commit(ctx context.Context, runID string) error {
	if err := f.tx.Commit(); err != nil {
		return err
	}
	return f.s.FinishIndexRun(ctx, runID, "complete", f.fileCount, "")
}

// Rollback discards the transaction.
func (f *FactTx) Rollback() error { return f.tx.Rollback() }

// ---------------------------------------------------------------------------
// Reads
// ---------------------------------------------------------------------------

const symbolColumns = `id, commit, file, kind, name, qualified_name, language,
                       start_line, end_line, start_byte, end_byte,
                       exported, is_test, parent, module`

func scanSymbol(rows *sql.Rows, into *Symbol) error {
	var exported, isTest int
	err := rows.Scan(&into.ID, &into.Commit, &into.File, &into.Kind, &into.Name,
		&into.QualifiedName, &into.Language, &into.StartLine, &into.EndLine,
		&into.StartByte, &into.EndByte, &exported, &isTest, &into.Parent, &into.Module)
	if err != nil {
		return err
	}
	into.Exported = exported == 1
	into.IsTest = isTest == 1
	return nil
}

// SymbolAt loads a symbol by ID at a specific commit.
func (s *Store) SymbolAt(ctx context.Context, commit, symbolID string) (Symbol, error) {
	var sym Symbol
	var exported, isTest int
	err := s.row(ctx,
		`SELECT `+symbolColumns+` FROM symbols WHERE id = ? AND commit = ?`,
		symbolID, commit).Scan(&sym.ID, &sym.Commit, &sym.File, &sym.Kind, &sym.Name,
		&sym.QualifiedName, &sym.Language, &sym.StartLine, &sym.EndLine,
		&sym.StartByte, &sym.EndByte, &exported, &isTest, &sym.Parent, &sym.Module)
	if errors.Is(err, sql.ErrNoRows) {
		return Symbol{}, ErrNotFound
	}
	if err != nil {
		return Symbol{}, err
	}
	sym.Exported = exported == 1
	sym.IsTest = isTest == 1
	return sym, nil
}

// DependentsOf returns edges pointing at the given symbol at the commit.
func (s *Store) DependentsOf(ctx context.Context, commit, symbolID string) ([]Edge, error) {
	return s.edgesBy(ctx, commit, `to_id = ?`, symbolID)
}

// EdgesOf returns edges leaving a symbol at the commit.
func (s *Store) EdgesOf(ctx context.Context, commit, symbolID string) ([]Edge, error) {
	return s.edgesBy(ctx, commit, `from_id = ?`, symbolID)
}

func (s *Store) edgesBy(ctx context.Context, commit, where string, arg any) ([]Edge, error) {
	rows, err := s.rows(ctx,
		`SELECT id, commit, from_id, to_id, kind, resolved, confidence,
		        target_hint, source_file, source_line
		 FROM edges WHERE `+where+` AND commit = ? ORDER BY kind, source_file, source_line`,
		arg, commit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Edge
	for rows.Next() {
		var e Edge
		var resolved int
		if err := rows.Scan(&e.ID, &e.Commit, &e.FromID, &e.ToID, &e.Kind,
			&resolved, &e.Confidence, &e.TargetHint, &e.SourceFile, &e.SourceLine); err != nil {
			return nil, err
		}
		e.Resolved = resolved == 1
		out = append(out, e)
	}
	return out, rows.Err()
}

// UnresolvedCallsOf returns unresolved call facts for a file at a commit.
func (s *Store) UnresolvedCallsOf(ctx context.Context, commit, file string) ([]Edge, error) {
	rows, err := s.rows(ctx,
		`SELECT id, commit, from_id, to_id, kind, resolved, confidence,
		        target_hint, source_file, source_line
		 FROM edges
		 WHERE source_file = ? AND commit = ? AND kind = 'CALLS' AND resolved = 0`,
		file, commit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Edge
	for rows.Next() {
		var e Edge
		var resolved int
		if err := rows.Scan(&e.ID, &e.Commit, &e.FromID, &e.ToID, &e.Kind,
			&resolved, &e.Confidence, &e.TargetHint, &e.SourceFile, &e.SourceLine); err != nil {
			return nil, err
		}
		e.Resolved = resolved == 1
		out = append(out, e)
	}
	return out, rows.Err()
}

// ModulesOf returns distinct modules at a commit.
func (s *Store) ModulesOf(ctx context.Context, commit string) ([]string, error) {
	rows, err := s.rows(ctx,
		`SELECT DISTINCT module FROM files WHERE commit = ? AND module != '' ORDER BY module`,
		commit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ModulesCrossEdges returns module-boundary edges: distinct modules with a
// file count and the number of inter-module edges, at a commit.
type ModuleNode struct {
	Module string `json:"module"`
	Files  int    `json:"files"`
}

type ModuleEdge struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Count    int    `json:"count"`
	Examples []string `json:"examples"`
}

// ModuleGraph computes the module boundary graph. Modules are derived from
// paths; inter-module edges are those whose from/to symbols live in
// different modules. All data comes from stored facts.
func (s *Store) ModuleGraph(ctx context.Context, commit string) ([]ModuleNode, []ModuleEdge, error) {
	fileRows, err := s.rows(ctx,
		`SELECT module, COUNT(*) FROM files WHERE commit = ? AND module != ''
		 GROUP BY module`, commit)
	if err != nil {
		return nil, nil, err
	}
	var nodes []ModuleNode
	modules := map[string]bool{}
	for fileRows.Next() {
		var m ModuleNode
		if err := fileRows.Scan(&m.Module, &m.Files); err != nil {
			fileRows.Close()
			return nil, nil, err
		}
		modules[m.Module] = true
		nodes = append(nodes, m)
	}
	fileRows.Close()
	if err := fileRows.Err(); err != nil {
		return nil, nil, err
	}

	edgeRows, err := s.rows(ctx,
		`SELECT fm.module, tm.module, COUNT(*)
		 FROM edges e
		 JOIN files fm ON fm.id = e.from_id AND fm.commit = e.commit
		 JOIN files tm ON tm.id = e.to_id AND tm.commit = e.commit
		 WHERE e.commit = ? AND e.kind IN ('CALLS','IMPORTS')
		       AND e.to_id != '' AND fm.module != tm.module
		 GROUP BY fm.module, tm.module
		 HAVING COUNT(*) > 0`, commit)
	if err != nil {
		return nil, nil, err
	}
	defer edgeRows.Close()
	var edges []ModuleEdge
	seen := map[string]bool{}
	for edgeRows.Next() {
		var e ModuleEdge
		if err := edgeRows.Scan(&e.From, &e.To, &e.Count); err != nil {
			return nil, nil, err
		}
		if !modules[e.From] || !modules[e.To] {
			continue // keep only modules that actually have files
		}
		key := e.From + "\x00" + e.To
		if !seen[key] {
			seen[key] = true
			e.Examples = []string{fmt.Sprintf("%d cross-module edge(s)", e.Count)}
			edges = append(edges, e)
		}
	}
	return nodes, edges, edgeRows.Err()
}

// FileCount returns the number of files at a commit.
func (s *Store) FileCount(ctx context.Context, commit string) (int, error) {
	var n int
	err := s.row(ctx, `SELECT COUNT(*) FROM files WHERE commit = ?`, commit).Scan(&n)
	return n, err
}

// SymbolCount returns the number of symbols at a commit.
func (s *Store) SymbolCount(ctx context.Context, commit string) (int, error) {
	var n int
	err := s.row(ctx, `SELECT COUNT(*) FROM symbols WHERE commit = ?`, commit).Scan(&n)
	return n, err
}

// EdgeCount returns the number of edges at a commit.
func (s *Store) EdgeCount(ctx context.Context, commit string) (int, error) {
	var n int
	err := s.row(ctx, `SELECT COUNT(*) FROM edges WHERE commit = ?`, commit).Scan(&n)
	return n, err
}

// ---------------------------------------------------------------------------
// Commits + co-change (git history indexing)
// ---------------------------------------------------------------------------

// CommitInfo is one git commit and the files it touched, as read from the
// repository's .git history. Files are bounded and deterministic.
type CommitInfo struct {
	Commit  string    `json:"commit"`
	Branch  string    `json:"branch"`
	Message string    `json:"message"`
	Author  string    `json:"author"`
	Date    time.Time `json:"date"`
	Files   []string  `json:"files"`
}

// InsertCommit stores commit metadata and the file list it touched.
func (s *Store) InsertCommit(ctx context.Context, c CommitInfo) error {
	files, _ := json.Marshal(c.Files)
	_, err := s.exec(ctx,
		`INSERT INTO commits (commit, branch, message, author, date, files)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT (commit) DO UPDATE SET
		    branch = excluded.branch,
		    message = excluded.message,
		    author = excluded.author,
		    date = excluded.date,
		    files = excluded.files`,
		c.Commit, c.Branch, c.Message, c.Author, rfc3339(c.Date), string(files))
	return err
}

// CommitsForFile returns the commits that touched a file, newest first,
// bounded to limit. The file path is matched against the JSON file list
// stored on each commit.
func (s *Store) CommitsForFile(ctx context.Context, file string, limit int) ([]CommitInfo, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.rows(ctx,
		`SELECT commit, branch, message, author, date, files
		 FROM commits
		 WHERE files LIKE ?
		 ORDER BY date DESC LIMIT `+fmt.Sprint(limit),
		`%"`+file+`"%`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CommitInfo
	for rows.Next() {
		var c CommitInfo
		var dateStr, filesJSON string
		if err := rows.Scan(&c.Commit, &c.Branch, &c.Message, &c.Author,
			&dateStr, &filesJSON); err != nil {
			return nil, err
		}
		if t := parseTime(dateStr); t != nil {
			c.Date = *t
		}
		_ = json.Unmarshal([]byte(filesJSON), &c.Files)
		out = append(out, c)
	}
	return out, rows.Err()
}

// CoChangesForFile returns historical co-change pairs for a file, most
// frequent first, bounded to limit.
func (s *Store) CoChangesForFile(ctx context.Context, file string, limit int) ([]CoChange, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.rows(ctx,
		`SELECT file_a, file_b, commit, commit_date, count
		 FROM co_changes
		 WHERE file_a = ? OR file_b = ?
		 ORDER BY count DESC, commit_date DESC LIMIT `+fmt.Sprint(limit),
		file, file)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CoChange
	for rows.Next() {
		var c CoChange
		var dateStr string
		if err := rows.Scan(&c.FileA, &c.FileB, &c.Commit, &dateStr, &c.Count); err != nil {
			return nil, err
		}
		if t := parseTime(dateStr); t != nil {
			c.CommitDate = *t
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func newRunID(commit string) string {
	return "run-" + commit
}

// purgeByCommit removes all facts, runs, co-change rows and commit records
// tied to a specific commit. Used by DeleteRepository.
func (s *Store) purgeByCommit(ctx context.Context, commit string) error {
	for _, tbl := range []string{
		"DELETE FROM files WHERE commit = ?",
		"DELETE FROM symbols WHERE commit = ?",
		"DELETE FROM edges WHERE commit = ?",
		"DELETE FROM symbol_versions WHERE commit = ?",
		"DELETE FROM co_changes WHERE commit = ?",
		"DELETE FROM commits WHERE commit = ?",
	} {
		if _, err := s.exec(ctx, tbl, commit); err != nil {
			return err
		}
	}
	_, err := s.exec(ctx, `DELETE FROM index_runs WHERE commit = ?`, commit)
	return err
}

// DeleteRepositoryFully removes a repository and all of its indexed data.
func (s *Store) DeleteRepositoryFully(ctx context.Context, repoID string) error {
	rows, err := s.rows(ctx, `SELECT commit FROM index_runs WHERE repository_id = ?`, repoID)
	if err != nil {
		return err
	}
	var commits []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			rows.Close()
			return err
		}
		commits = append(commits, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, c := range commits {
		if err := s.purgeByCommit(ctx, c); err != nil {
			return err
		}
	}
	_, err = s.exec(ctx, `DELETE FROM index_runs WHERE repository_id = ?`, repoID)
	if err != nil {
		return err
	}
	_, err = s.exec(ctx, `DELETE FROM repositories WHERE id = ?`, repoID)
	return err
}

// Answers
func (s *Store) SaveAnswer(ctx context.Context, a Answer) error {
	ev, _ := json.Marshal(a.Evidence)
	_, err := s.exec(ctx,
		`INSERT INTO answers (id, commit, question, answer, mode, confidence,
		                     derived, created_at, evidence)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (id) DO UPDATE SET answer = excluded.answer,
		    confidence = excluded.confidence,
		    evidence = excluded.evidence`,
		a.ID, a.Commit, a.Question, a.Answer, a.Mode, a.Confidence,
		b2i(a.Derived), rfc3339(a.CreatedAt), string(ev))
	return err
}

// GetAnswer returns a stored answer by ID.
func (s *Store) GetAnswer(ctx context.Context, id string) (Answer, error) {
	var a Answer
	var derived int
	var createdAt, evidence string
	err := s.row(ctx,
		`SELECT id, commit, question, answer, mode, confidence, derived,
		        created_at, evidence
		 FROM answers WHERE id = ?`, id).
		Scan(&a.ID, &a.Commit, &a.Question, &a.Answer, &a.Mode, &a.Confidence,
			&derived, &createdAt, &evidence)
	if errors.Is(err, sql.ErrNoRows) {
		return Answer{}, ErrNotFound
	}
	if err != nil {
		return Answer{}, err
	}
	a.Derived = derived == 1
	if t := parseTime(createdAt); t != nil {
		a.CreatedAt = *t
	}
	_ = json.Unmarshal([]byte(evidence), &a.Evidence)
	return a, nil
}

// CommitSummaries returns the most recent commits for a repository for the
// history timeline screen.
func (s *Store) CommitSummaries(ctx context.Context, limit int) ([]CommitInfo, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.rows(ctx,
		`SELECT commit, branch, message, author, date, files
		 FROM commits
		 ORDER BY date DESC LIMIT `+fmt.Sprint(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CommitInfo
	for rows.Next() {
		var c CommitInfo
		var dateStr, filesJSON string
		if err := rows.Scan(&c.Commit, &c.Branch, &c.Message, &c.Author,
			&dateStr, &filesJSON); err != nil {
			return nil, err
		}
		if t := parseTime(dateStr); t != nil {
			c.Date = *t
		}
		_ = json.Unmarshal([]byte(filesJSON), &c.Files)
		out = append(out, c)
	}
	return out, rows.Err()
}
