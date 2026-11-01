package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// queries_explain.go: read paths for search, explain, impact/dependants,
// commit timelines, incremental carry-over and the provenance invariant.
// Every query here reads only persisted, commit-stamped facts; nothing is
// derived or guessed.

// ---------------------------------------------------------------------------
// Search
// ---------------------------------------------------------------------------

// SearchSymbols returns symbol candidates at commit matching q (case
// insensitive; empty q lists all) optionally filtered by kind. Results are
// ordered by (name, kind, id) so cursor pagination (offset) is stable.
// Scoring happens in internal/search, not here: the store returns the raw
// candidate set, the scorer decides what a match means.
func (s *Store) SearchSymbols(ctx context.Context, commit, q, kind string, limit, offset int) ([]Symbol, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	where := "commit = ?"
	args := []any{commit}
	if q != "" {
		pattern := "%" + q + "%"
		where += " AND (name LIKE ? OR qualified_name LIKE ?)"
		args = append(args, pattern, pattern)
	}
	if kind != "" {
		where += " AND kind = ?"
		args = append(args, kind)
	}
	args = append(args, limit, offset)
	stmt := "SELECT " + symbolColumns + " FROM symbols WHERE " + where +
		" ORDER BY name, kind, id LIMIT ? OFFSET ?"
	rows, err := s.rows(ctx, stmt, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Symbol
	for rows.Next() {
		var sym Symbol
		if err := scanSymbol(rows, &sym); err != nil {
			return nil, err
		}
		out = append(out, sym)
	}
	return out, rows.Err()
}

// FindSymbolsByName returns symbols with the exact name at commit, ordered
// deterministically. Bounded; the caller treats the first entries as the
// best matches.
func (s *Store) FindSymbolsByName(ctx context.Context, commit, name string, limit int) ([]Symbol, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.rows(ctx,
		"SELECT "+symbolColumns+" FROM symbols WHERE commit = ? AND name = ? "+
			"ORDER BY kind, file, id LIMIT ?",
		commit, name, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Symbol
	for rows.Next() {
		var sym Symbol
		if err := scanSymbol(rows, &sym); err != nil {
			return nil, err
		}
		out = append(out, sym)
	}
	return out, rows.Err()
}

// SymbolsInModule returns symbols belonging to a module at commit.
func (s *Store) SymbolsInModule(ctx context.Context, commit, module string, limit int) ([]Symbol, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.rows(ctx,
		"SELECT "+symbolColumns+" FROM symbols WHERE commit = ? AND module = ? "+
			"AND kind NOT IN ('module','package') ORDER BY name, id LIMIT ?",
		commit, module, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Symbol
	for rows.Next() {
		var sym Symbol
		if err := scanSymbol(rows, &sym); err != nil {
			return nil, err
		}
		out = append(out, sym)
	}
	return out, rows.Err()
}

// SymbolsForFile returns every symbol declared in one file at commit.
func (s *Store) SymbolsForFile(ctx context.Context, commit, file string) ([]Symbol, error) {
	rows, err := s.rows(ctx,
		"SELECT "+symbolColumns+" FROM symbols WHERE commit = ? AND file = ? ORDER BY start_line, id",
		commit, file)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Symbol
	for rows.Next() {
		var sym Symbol
		if err := scanSymbol(rows, &sym); err != nil {
			return nil, err
		}
		out = append(out, sym)
	}
	return out, rows.Err()
}

// SymbolLatest loads the symbol row for an ID at its newest stored commit.
// Symbol IDs are content-stable: the newest commit a symbol appears in is
// its current identity.
func (s *Store) SymbolLatest(ctx context.Context, id string) (Symbol, error) {
	var sym Symbol
	var exported, isTest int
	err := s.row(ctx,
		"SELECT "+symbolColumns+" FROM symbols WHERE id = ? ORDER BY commit DESC LIMIT 1", id).
		Scan(&sym.ID, &sym.Commit, &sym.File, &sym.Kind, &sym.Name,
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

// ---------------------------------------------------------------------------
// Incremental carry-over
// ---------------------------------------------------------------------------

// FilesAtCommit returns every file fact at commit, with exports decoded.
func (s *Store) FilesAtCommit(ctx context.Context, commit string) ([]File, error) {
	rows, err := s.rows(ctx,
		`SELECT id, commit, path, language, module, checksum, size_bytes,
		        exports, top_symbols, first_commit, last_commit
		 FROM files WHERE commit = ? ORDER BY path`, commit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []File
	for rows.Next() {
		var f File
		var exportsJSON string
		if err := rows.Scan(&f.ID, &f.Commit, &f.Path, &f.Language, &f.Module,
			&f.Checksum, &f.SizeBytes, &exportsJSON, &f.TopSymbols,
			&f.FirstCommit, &f.LastCommit); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(exportsJSON), &f.Exports)
		out = append(out, f)
	}
	return out, rows.Err()
}

// SymbolsAtCommit returns every symbol fact at commit.
func (s *Store) SymbolsAtCommit(ctx context.Context, commit string) ([]Symbol, error) {
	rows, err := s.rows(ctx,
		"SELECT "+symbolColumns+" FROM symbols WHERE commit = ? ORDER BY id", commit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Symbol
	for rows.Next() {
		var sym Symbol
		if err := scanSymbol(rows, &sym); err != nil {
			return nil, err
		}
		out = append(out, sym)
	}
	return out, rows.Err()
}

// EdgesAtCommit returns every edge fact at commit.
func (s *Store) EdgesAtCommit(ctx context.Context, commit string) ([]Edge, error) {
	rows, err := s.rows(ctx,
		`SELECT id, commit, from_id, to_id, kind, resolved, confidence,
		        target_hint, source_file, source_line
		 FROM edges WHERE commit = ? ORDER BY id`, commit)
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

// ---------------------------------------------------------------------------
// Commit timeline
// ---------------------------------------------------------------------------

// Timeline is one commit's changed files plus the symbols declared in them
// (symbols are only known for commits that carry a complete index run; for
// unindexed commits the slice is empty, never invented).
type Timeline struct {
	Info    CommitInfo
	Symbols []Symbol
}

// CommitTimeline reads the commit record and, if the commit is indexed, the
// symbols declared in the files it touched.
func (s *Store) CommitTimeline(ctx context.Context, sha string) (Timeline, error) {
	var info CommitInfo
	var dateStr, filesJSON string
	err := s.row(ctx,
		`SELECT commit, branch, message, author, date, files FROM commits WHERE commit = ?`, sha).
		Scan(&info.Commit, &info.Branch, &info.Message, &info.Author, &dateStr, &filesJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return Timeline{}, ErrNotFound
	}
	if err != nil {
		return Timeline{}, err
	}
	_ = json.Unmarshal([]byte(filesJSON), &info.Files)
	sort.Strings(info.Files)
	if len(info.Files) == 0 {
		return Timeline{Info: info}, nil
	}
	tl := Timeline{Info: info}
	for _, f := range info.Files {
		syms, err := s.SymbolsForFile(ctx, sha, f)
		if err != nil {
			return Timeline{}, err
		}
		tl.Symbols = append(tl.Symbols, syms...)
	}
	return tl, nil
}

// ---------------------------------------------------------------------------
// Provenance invariant
// ---------------------------------------------------------------------------

// ProvenanceViolation is one stored fact that breaks the hard guarantee:
// every node and edge must carry a concrete source-file + line + commit
// tuple; resolved edges must point at a symbol that exists at the same
// commit; unresolved references must carry a target_hint.
type ProvenanceViolation struct {
	Table  string
	ID     string
	Detail string
}

// VerifyProvenance scans the stored facts and returns every violation.
// An empty result is the invariant holding: no node or edge exists without
// concrete provenance, and no resolved edge dangles into a symbol that the
// store does not hold at that commit.
func (s *Store) VerifyProvenance(ctx context.Context) ([]ProvenanceViolation, error) {
	var v []ProvenanceViolation

	// 1. Edges without the source-file + line + commit tuple.
	rows, err := s.rows(ctx,
		`SELECT id FROM edges WHERE commit = '' OR source_file = '' OR source_line <= 0 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		v = append(v, ProvenanceViolation{Table: "edges", ID: id,
			Detail: "missing source-file, source-line or commit tuple"})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 2. Resolved edges whose target symbol does not exist at the edge's
	//    commit — a resolved fact may never point at an invented symbol.
	rows, err = s.rows(ctx,
		`SELECT e.id FROM edges e
		 WHERE e.resolved = 1 AND e.to_id != ''
		   AND NOT EXISTS (SELECT 1 FROM symbols sy WHERE sy.id = e.to_id AND sy.commit = e.commit)
		 ORDER BY e.id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		v = append(v, ProvenanceViolation{Table: "edges", ID: id,
			Detail: "resolved edge points at a symbol that does not exist at its commit"})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 3. Unresolved reference edges without a hint: an unresolved edge must
	//    persist the source expression it could not resolve.
	rows, err = s.rows(ctx,
		`SELECT id FROM edges WHERE resolved = 0 AND target_hint = '' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		v = append(v, ProvenanceViolation{Table: "edges", ID: id,
			Detail: "unresolved edge has no target_hint"})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 4. Symbols without a concrete file or commit.
	rows, err = s.rows(ctx,
		`SELECT id FROM symbols WHERE commit = '' OR file = '' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		v = append(v, ProvenanceViolation{Table: "symbols", ID: id,
			Detail: "symbol missing file or commit provenance"})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return v, nil
}
