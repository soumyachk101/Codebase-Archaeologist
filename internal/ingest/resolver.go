// Package ingest implements the repository ingestion pipeline:
//
//  1. snapshot a commit's file set from git
//  2. filter through .gitignore rules and size/binary exclusions
//  3. feed supported source files to the Rust tree-sitter parser
//  4. resolve import specifiers and call targets to concrete symbols
//     using the repository's own directory structure
//  5. emit a set of facts (files, symbols, edges, co-changes) that the CLI
//     commits atomically
//
// The hard invariant: every edge returned by Build resolves to a symbol
// that exists in this exact index, and carries the source file + line where
// the reference was observed. Anything we cannot resolve is returned as an
// *unresolved* edge, never promoted to a resolved one.
package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"hash/fnv"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/ensembyte/archaeologist/internal/store"
)

// ParserFile mirrors the parser's JSON wire format for one file.
type ParserFile struct {
	Path          string      `json:"path"`
	Language      string      `json:"language"`
	ParseOK       bool        `json:"parseOk"`
	ParseError    string      `json:"parseError"`
	Package       string      `json:"package"`
	Symbols       []PSymbol   `json:"symbols"`
	Imports       []PImport   `json:"imports"`
	Calls         []PCall     `json:"calls"`
	ExportedNames []string    `json:"exportedNames"`
}

// PSymbol mirrors parser symbol.
type PSymbol struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"`
	Name          string `json:"name"`
	QualifiedName string `json:"qualifiedName"`
	Language      string `json:"language"`
	File          string `json:"file"`
	StartLine     int32  `json:"startLine"`
	EndLine       int32  `json:"endLine"`
	StartByte     int32  `json:"startByte"`
	EndByte       int32  `json:"endByte"`
	Exported      bool   `json:"exported"`
	IsTest        bool   `json:"isTest"`
	Parent        string `json:"parent"`
	Key           string `json:"key"`
}

// PImport mirrors parser import fact.
type PImport struct {
	Spec     string       `json:"spec"`
	Line     int32       `json:"line"`
	Dynamic  bool        `json:"dynamic"`
	Hint     string      `json:"hint"`
	Bindings []PBinding   `json:"bindings"`
}

// PBinding mirrors parser import binding.
type PBinding struct {
	Local    string `json:"local"`
	Original string `json:"original"`
}

// PCall mirrors parser call fact.
type PCall struct {
	Line         int32  `json:"line"`
	From         string `json:"from"`
	Target       string `json:"target"`
	Hint         string `json:"hint"`
	ImportedName string `json:"importedName"`
}

// ParserManifest mirrors the parser's top-level output.
type ParserManifest struct {
	SchemaVersion  int64         `json:"schemaVersion"`
	ParserVersion  string        `json:"parserVersion"`
	Commit         string        `json:"commit"`
	Languages      []string      `json:"languages"`
	Files          []ParserFile  `json:"files"`
	Skipped        []PSkipped    `json:"skipped"`
}

// PSkipped mirrors parser skipped file.
type PSkipped struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// ---------------------------------------------------------------------------
// Resolution
// ---------------------------------------------------------------------------

// Resolved is one output fact: a symbol, a resolved edge, or an unresolved
// edge. All carry their source provenance.
type Resolved struct {
	Symbols []store.Symbol
	Edges   []store.Edge
	Files   []store.File
}

// index is the in-memory world the resolver works against.
type index struct {
	files    map[string]*ParserFile   // path -> file
	byPath   map[string]*ParserFile   // alias
	exports  map[string]map[string][]string // path -> exported name -> symbol ids
	goPkgs   map[string]*ParserFile   // go import path (last segments) -> file
	byID     map[string]store.Symbol  // symbol id -> symbol
}

// Build resolves a parser manifest into a durable fact set. It is the only
// place where references become edges, and it is deterministic: given the
// same input, the same edges are produced.
func Build(manifest ParserManifest, commit, branch string) Resolved {
	// A full build: every file is both parsed and context.
	return BuildFacts(manifest.Files, manifest.Files, commit, branch)
}

// BuildFacts resolves a split fact set:
//   - parsedFiles contribute file/symbol/edge facts (a fresh parse);
//   - contextFiles contribute resolution data only (exports, symbol
//     tables) so cross-file references resolve, but emit no new facts —
//     the caller carries their facts over from a previous index run.
//
// Both lists may be the same slice (full build).
func BuildFacts(parsedFiles, contextFiles []ParserFile, commit, branch string) Resolved {
	out := Resolved{
		Symbols: []store.Symbol{},
		Edges:   []store.Edge{},
		Files:   []store.File{},
	}

	idx := &index{
		files:   map[string]*ParserFile{},
		byPath:  map[string]*ParserFile{},
		exports: map[string]map[string][]string{},
		goPkgs:  map[string]*ParserFile{},
		byID:    map[string]store.Symbol{},
	}

	// First pass: index every file (parsed + context) and its exported
	// surface. Resolution needs the whole world; facts only come from the
	// parsed set.
	seenPath := map[string]bool{}
	for _, list := range [][]ParserFile{parsedFiles, contextFiles} {
		for i := range list {
			f := &list[i]
			if seenPath[f.Path] {
				continue
			}
			seenPath[f.Path] = true
			idx.files[f.Path] = f
			idx.byPath[f.Path] = f
			exportMap := map[string][]string{}
			for _, n := range f.ExportedNames {
				for _, s := range f.Symbols {
					if s.Name == n && s.Kind != "module" && s.Kind != "package" {
						exportMap[n] = append(exportMap[n], s.ID)
					}
				}
			}
			idx.exports[f.Path] = exportMap
		}
	}

	// Second pass: build Go package index over the whole world.
	buildGoPackageIndexFiles(append(append([]ParserFile{}, parsedFiles...), contextFiles...), idx)

	// Per-file symbol + edge emission: parsed files only.
	for i := range parsedFiles {
		f := &parsedFiles[i]

		module := moduleForPath(f.Path)

		for _, ps := range f.Symbols {
			sym := store.Symbol{
				ID:            ps.ID,
				Commit:        commit,
				File:          f.Path,
				Kind:          ps.Kind,
				Name:          ps.Name,
				QualifiedName: ps.QualifiedName,
				Language:      ps.Language,
				StartLine:     ps.StartLine,
				EndLine:       ps.EndLine,
				StartByte:     ps.StartByte,
				EndByte:       ps.EndByte,
				Exported:      ps.Exported,
				IsTest:        ps.IsTest,
				Parent:        ps.Parent,
				Key:           ps.Key,
				Module:        module,
			}
			out.Symbols = append(out.Symbols, sym)
			idx.byID[ps.ID] = sym
		}

		// Emit file fact.
		exports := make([]string, 0, len(f.ExportedNames))
		exports = append(exports, f.ExportedNames...)
		out.Files = append(out.Files, store.File{
			ID:        fileID(f.Path),
			Commit:    commit,
			Path:      f.Path,
			Language:  f.Language,
			Module:    module,
			Checksum:  checksum(f),
			Exports:   exports,
			TopSymbols: countTopLevel(f),
		})

		// Resolve imports.
		out.Edges = append(out.Edges, resolveImports(f, idx, commit, branch)...)
		// Resolve calls.
		out.Edges = append(out.Edges, resolveCalls(f, idx, commit, branch)...)
	}

	// Deterministic ordering.
	sortFacts(&out)
	return out
}

// fileID is the stable file identifier for a path. The (id, commit) pair is
// the primary key of the files table, so the ID only needs to be stable per
// path; the commit is stored alongside.
func fileID(pathStr string) string {
	sum := sha256hex(pathStr)
	return "f-" + sum[:16]
}

// sha256hex returns the lowercase hex of SHA-256 over s.
func sha256hex(s string) string {
	h := sha256.Sum256([]byte("archaeologist:file:v1|" + s))
	return hex.EncodeToString(h[:])
}
func checksum(f *ParserFile) string {
	h := fnv.New64a()
	h.Write([]byte(f.Path))
	for _, s := range f.Symbols {
		h.Write([]byte(s.ID))
	}
	for _, i := range f.Imports {
		h.Write([]byte(i.Hint))
	}
	for _, c := range f.Calls {
		h.Write([]byte(c.Hint))
	}
	return itoa36(h.Sum64())
}

func itoa36(v uint64) string {
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	if v == 0 {
		return "0"
	}
	var b [64]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = digits[v%36]
		v /= 36
	}
	return string(b[i:])
}

func countTopLevel(f *ParserFile) int {
	n := 0
	for _, s := range f.Symbols {
		if s.Kind == "function" || s.Kind == "class" || s.Kind == "method" {
			n++
		}
	}
	return n
}

// moduleForPath derives the architectural module from the path. The first
// two path segments form the module; a bare top-level file is its own
// module. This is a stable, deterministic rule so the module graph is
// reproducible and grounded in the file layout.
func moduleForPath(p string) string {
	parts := strings.Split(p, "/")
	if len(parts) <= 2 {
		return parts[0]
	}
	// Skip common roots that are not real module boundaries.
	if parts[0] == "src" || parts[0] == "lib" || parts[0] == "app" || parts[0] == "packages" {
		if len(parts) > 2 {
			return parts[1]
		}
		return parts[0]
	}
	return parts[0]
}

// buildGoPackageIndexFiles maps Go import paths to the nearest source file
// across a list of files (parsed + context).
func buildGoPackageIndexFiles(files []ParserFile, idx *index) {
	// Group files by their Go package directory.
	dirPkgs := map[string]*ParserFile{}
	for i := range files {
		f := &files[i]
		if f.Language != "go" {
			continue
		}
		dir := path.Dir(f.Path)
		if dir == "." {
			dir = ""
		}
		// Keep the file with the most symbols as the representative.
		if existing, ok := dirPkgs[dir]; !ok || len(f.Symbols) > len(existing.Symbols) {
			dirPkgs[dir] = f
		}
	}

	// Candidate import suffixes: for import "foo/bar/baz", the target
	// directory is "foo/bar/baz" or any of its parent segments when the
	// import is a relative-absolute path. Go module paths are not fully
	// resolvable without go.mod; we resolve the longest matching
	// directory suffix that exists in the index.
	candidates := []string{}
	for dir := range dirPkgs {
		candidates = append(candidates, dir)
	}
	sort.Strings(candidates)

	for i := range files {
		f := &files[i]
		if f.Language != "go" {
			continue
		}
		for _, imp := range f.Imports {
			target, ok := matchGoImport(imp.Spec, dirPkgs, candidates)
			if ok {
				idx.goPkgs[imp.Spec] = target
			}
		}
	}
}

// matchGoImport finds the nearest indexed Go package for an import path.
// It prefers exact directory matches, then longest-prefix matches. Returns
// the target file (representing the package) and whether it was found.
func matchGoImport(spec string, dirPkgs map[string]*ParserFile, candidates []string) (*ParserFile, bool) {
	// Exact: a directory in the index matches the import path suffix.
	if f, ok := dirPkgs[spec]; ok {
		return f, true
	}
	// Longest suffix match: import "a/b/c" where only "b/c" is indexed.
	parts := strings.Split(spec, "/")
	for start := 1; start < len(parts); start++ {
		suffix := strings.Join(parts[start:], "/")
		if f, ok := dirPkgs[suffix]; ok {
			return f, true
		}
	}
	return nil, false
}

// importHint is the persisted display form of an import fact: the parser's
// own hint when it set one, else the source spec as written. The value is
// the observed source expression, never an inference.
func importHint(imp PImport) string {
	if imp.Hint != "" {
		return imp.Hint
	}
	return imp.Spec
}

// callHint is the persisted display form of a call fact: the parser's hint
// when set, else the target expression as written.
func callHint(c PCall) string {
	if c.Hint != "" {
		return c.Hint
	}
	return c.Target
}

// ---------------------------------------------------------------------------
// Import resolution
// ---------------------------------------------------------------------------

// resolveImports turns one file's import facts into graph edges.
func resolveImports(f *ParserFile, idx *index, commit, branch string) []store.Edge {
	var out []store.Edge

	switch f.Language {
	case "go":
		out = resolveGoImports(f, idx, commit)
	default:
		out = resolveJSImports(f, idx, commit)
	}

	for _, e := range out {
		e.Commit = commit
	}
	return out
}

// resolveGoImports: import path -> target package's representative symbol.
func resolveGoImports(f *ParserFile, idx *index, commit string) []store.Edge {
	var out []store.Edge
	from := moduleID(f)

	for _, imp := range f.Imports {
		// Go import spec is an absolute package path.
		hint := importHint(imp)
		target, ok := idx.goPkgs[imp.Spec]
		if !ok {
			// Unresolved: external package or out-of-index module.
			out = append(out, store.Edge{
				ID:          edgeID("IMPORTS", from, hint, f.Path, imp.Line),
				FromID:      from,
				ToID:        "",
				Kind:        "IMPORTS",
				Resolved:    false,
				Confidence:  "unresolved",
				TargetHint:  hint,
				SourceFile:  f.Path,
				SourceLine:  imp.Line,
			})
			continue
		}
		// The target is the package symbol of the resolved file.
		var toID string
		for _, s := range target.Symbols {
			if s.Kind == "package" {
				toID = s.ID
				break
			}
		}
		if toID == "" {
			toID = target.Symbols[0].ID
		}
		out = append(out, store.Edge{
			ID:         edgeID("IMPORTS", from, hint, f.Path, imp.Line),
			FromID:     from,
			ToID:       toID,
			Kind:       "IMPORTS",
			Resolved:   true,
			Confidence: "static",
			TargetHint: hint,
			SourceFile: f.Path,
			SourceLine: imp.Line,
		})
	}
	return out
}

// resolveJSImports: specifier -> target module file.
func resolveJSImports(f *ParserFile, idx *index, commit string) []store.Edge {
	var out []store.Edge
	from := moduleID(f)

	for _, imp := range f.Imports {
		hint := importHint(imp)
		targetPath, resolved := resolveJSSpecifier(f, imp.Spec, idx)
		if !resolved {
			out = append(out, store.Edge{
				ID:         edgeID("IMPORTS", from, hint, f.Path, imp.Line),
				FromID:     from,
				ToID:       "",
				Kind:       "IMPORTS",
				Resolved:   false,
				Confidence: "unresolved",
				TargetHint: hint,
				SourceFile: f.Path,
				SourceLine: imp.Line,
			})
			continue
		}
		// The target is the module symbol of the resolved file.
		var toID string
		if tf := idx.byPath[targetPath]; tf != nil {
			for _, s := range tf.Symbols {
				if s.Kind == "module" {
					toID = s.ID
					break
				}
			}
		}
		out = append(out, store.Edge{
			ID:         edgeID("IMPORTS", from, hint, f.Path, imp.Line),
			FromID:     from,
			ToID:       toID,
			Kind:       "IMPORTS",
			Resolved:   true,
			Confidence: "static",
			TargetHint: hint,
			SourceFile: f.Path,
			SourceLine: imp.Line,
		})
	}
	return out
}

// resolveJSSpecifier resolves a JS/TS import specifier relative to the
// importing file. Relative specs ("./x", "../y") resolve against the
// importing file's directory; bare specs ("lodash") are external and remain
// unresolved. Extension candidates (.js, .ts, /index) are tried in order.
func resolveJSSpecifier(from *ParserFile, spec string, idx *index) (string, bool) {
	if !strings.HasPrefix(spec, ".") {
		// Bare specifier: external dependency, not in the index.
		return "", false
	}
	base := path.Dir(from.Path)
	joined := path.Join(base, spec)
	candidates := []string{
		joined,
		joined + ".js",
		joined + ".ts",
		joined + ".tsx",
		joined + ".mjs",
		joined + ".cjs",
		path.Join(joined, "index.js"),
		path.Join(joined, "index.ts"),
	}
	for _, c := range candidates {
		if idx.byPath[c] != nil {
			return c, true
		}
	}
	return joined, false
}

// moduleID returns the module/package symbol of a JS file or Go package.
func moduleID(f *ParserFile) string {
	for _, s := range f.Symbols {
		if s.Kind == "module" || s.Kind == "package" {
			return s.ID
		}
	}
	// Fallback: a synthetic ID so the edge always has a source.
	return "synthetic:" + f.Path
}

// ---------------------------------------------------------------------------
// Call resolution
// ---------------------------------------------------------------------------

// resolveCalls turns one file's call facts into graph edges. A call is
// resolved when its target (via an import alias) maps to a concrete
// exported symbol in a known file; otherwise it is emitted as unresolved.
func resolveCalls(f *ParserFile, idx *index, commit, branch string) []store.Edge {
	var out []store.Edge

	switch f.Language {
	case "go":
		out = resolveGoCalls(f, idx, commit)
	default:
		out = resolveJSCalls(f, idx, commit)
	}
	for _, e := range out {
		e.Commit = commit
	}
	return out
}

// resolveJSCalls: target "x" or "x.y" where x is an imported binding.
func resolveJSCalls(f *ParserFile, idx *index, commit string) []store.Edge {
	// Build a local alias -> (imported original, target file) map from this
	// file's imports that resolved.
	alias := map[string]struct {
		original  string
		targetFile string
	}{}
	for _, imp := range f.Imports {
		targetPath, ok := resolveJSSpecifier(f, imp.Spec, idx)
		if !ok {
			continue
		}
		for _, b := range imp.Bindings {
			alias[b.Local] = struct {
				original   string
				targetFile string
			}{original: b.Original, targetFile: targetPath}
		}
	}

	for _, c := range f.Calls {
		from := c.From
		hint := callHint(c)
		if from == "" {
			from = moduleID(f)
		}
		// Decompose target: base identifier + optional member.
		base := c.Target
		member := ""
		if dot := strings.Index(c.Target, "."); dot >= 0 {
			base = c.Target[:dot]
			member = c.Target[dot+1:]
		}

		a, ok := alias[base]
		if !ok {
			// Not an import binding: a local or dynamic reference. Emit
			// unresolved so we never invent a target.
			out = append(out, store.Edge{
				ID:         edgeID("CALLS", from, hint, f.Path, c.Line),
				FromID:     from,
				ToID:       "",
				Kind:       "CALLS",
				Resolved:   false,
				Confidence: "unresolved",
				TargetHint: hint,
				SourceFile: f.Path,
				SourceLine: c.Line,
			})
			continue
		}

		// Look up the concrete symbol in the target file.
		name := member
		if name == "" {
			name = a.original
		}
		if tf := idx.byPath[a.targetFile]; tf != nil {
			if ids, found := idx.exports[a.targetFile][name]; found && len(ids) > 0 {
				// Resolve to the first matching exported symbol.
				toID := ids[0]
				out = append(out, store.Edge{
					ID:         edgeID("CALLS", from, hint, f.Path, c.Line),
					FromID:     from,
					ToID:       toID,
					Kind:       "CALLS",
					Resolved:   true,
					Confidence: "static",
					TargetHint: hint,
					SourceFile: f.Path,
					SourceLine: c.Line,
				})
				continue
			}
		}
		// Target symbol not found in the target file's exported surface:
		// leave unresolved rather than guess.
		out = append(out, store.Edge{
			ID:         edgeID("CALLS", from, hint, f.Path, c.Line),
			FromID:     from,
			ToID:       "",
			Kind:       "CALLS",
			Resolved:   false,
			Confidence: "unresolved",
			TargetHint: hint,
			SourceFile: f.Path,
			SourceLine: c.Line,
		})
	}
	return out
}

// resolveGoCalls: target "pkg.Symbol" where pkg is an import alias.
func resolveGoCalls(f *ParserFile, idx *index, commit string) []store.Edge {
	// Build alias (local name) -> target Go file.
	alias := map[string]*ParserFile{}
	for _, imp := range f.Imports {
		target, ok := idx.goPkgs[imp.Spec]
		if !ok {
			continue
		}
		for _, b := range imp.Bindings {
			if b.Local != "" {
				alias[b.Local] = target
			}
		}
	}

	for _, c := range f.Calls {
		from := c.From
		hint := callHint(c)
		if from == "" {
			from = moduleID(f)
		}
		// Go call target is "Alias.Symbol".
		base := c.Target
		sym := ""
		if dot := strings.Index(c.Target, "."); dot >= 0 {
			base = c.Target[:dot]
			sym = c.Target[dot+1:]
		}
		target, ok := alias[base]
		if !ok {
			out = append(out, store.Edge{
				ID:         edgeID("CALLS", from, hint, f.Path, c.Line),
				FromID:     from,
				ToID:       "",
				Kind:       "CALLS",
				Resolved:   false,
				Confidence: "unresolved",
				TargetHint: hint,
				SourceFile: f.Path,
				SourceLine: c.Line,
			})
			continue
		}
		// Find the symbol in the target Go package.
		var toID string
		for _, s := range target.Symbols {
			if s.Name == sym && s.Kind == "function" {
				toID = s.ID
				break
			}
		}
		if toID == "" {
			for _, s := range target.Symbols {
				if s.Name == sym {
					toID = s.ID
					break
				}
			}
		}
		if toID == "" {
			out = append(out, store.Edge{
				ID:         edgeID("CALLS", from, hint, f.Path, c.Line),
				FromID:     from,
				ToID:       "",
				Kind:       "CALLS",
				Resolved:   false,
				Confidence: "unresolved",
				TargetHint: hint,
				SourceFile: f.Path,
				SourceLine: c.Line,
			})
			continue
		}
		out = append(out, store.Edge{
			ID:         edgeID("CALLS", from, hint, f.Path, c.Line),
			FromID:     from,
			ToID:       toID,
			Kind:       "CALLS",
			Resolved:   true,
			Confidence: "static",
			TargetHint: hint,
			SourceFile: f.Path,
			SourceLine: c.Line,
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// Determinism
// ---------------------------------------------------------------------------

func edgeID(kind, from, hint, file string, line int32) string {
	h := fnv.New64a()
	h.Write([]byte("archaeologist:edge:v1|"))
	h.Write([]byte(kind))
	h.Write([]byte("|"))
	h.Write([]byte(from))
	h.Write([]byte("|"))
	h.Write([]byte(hint))
	h.Write([]byte("|"))
	h.Write([]byte(file))
	h.Write([]byte("|"))
	h.Write([]byte(strconv.FormatInt(int64(line), 10)))
	return itoa36(h.Sum64())
}

func sortFacts(r *Resolved) {
	sort.Slice(r.Symbols, func(i, j int) bool { return r.Symbols[i].ID < r.Symbols[j].ID })
	sort.Slice(r.Edges, func(i, j int) bool { return r.Edges[i].ID < r.Edges[j].ID })
	sort.Slice(r.Files, func(i, j int) bool { return r.Files[i].Path < r.Files[j].Path })
}
