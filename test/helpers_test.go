// Package archtest holds the six required Codebase Archaeologist test
// categories: parser golden, provenance-invariant, incremental re-index,
// search scoring, explain-abstain, and CLI smoke.
//
// Everything is hermetic:
//   - the git fixture is built in t.TempDir() with `git init` + two commits
//     (skipped cleanly when git is unavailable);
//   - the store is a throwaway SQLite file, never a live service;
//   - the tree-sitter parser is the committed binary at parser/target/debug/
//     (skipped cleanly when it has not been built).
//
// The fixture source files live in testdata/fixture/{c1,c2}. c2 differs from
// c1 by exactly one change (a new `scale` export in js/app.js), which is
// what the incremental test asserts on.
package archtest

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ensembyte/archaeologist/internal/ingest"
	"github.com/ensembyte/archaeologist/internal/store"
)

// productRoot is the repository root containing parser/, internal/, etc.
// This test package lives at <root>/test, so the root is one level up.
func productRoot(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("locate product root: %v", err)
	}
	return abs
}

// parserBin returns the path to the built tree-sitter parser binary, or
// skips the test when it has not been compiled yet.
func parserBin(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(productRoot(t), "parser", "target", "debug", "parser")
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("parser binary not built at %s; run `cd parser && cargo build` first", bin)
	}
	return bin
}

// gitAvailable reports whether the git CLI can be used; the git-backed
// categories skip cleanly when it is absent.
func gitAvailable(t *testing.T) bool {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available; skipping git-backed test")
	}
	return true
}

// gitRepo is the fixture layout we build for the pipeline tests.
type gitRepo struct {
	root    string
	c1, c2  string // full commit SHAs
}

// buildGitFixture constructs a two-commit git repo from testdata/fixture.
// Commit 1 is the c1 tree; commit 2 applies the c2 change (js/app.js gains a
// `scale` export) on top, so exactly one file differs between c1 and c2.
func buildGitFixture(t *testing.T) gitRepo {
	t.Helper()
	gitAvailable(t)
	root := t.TempDir()
	dataDir := t.TempDir()
	_ = dataDir

	// Deterministic author/committer so no global git config is required.
	t.Setenv("GIT_AUTHOR_NAME", "Archaeologist Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Archaeologist Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")

	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}

	git("init", "-q", "-b", "main", ".")

	// Commit 1: the c1 tree.
	copyTree(t, filepath.Join(productRoot(t), "testdata", "fixture", "c1"), root)
	git("add", "-A")
	git("commit", "-q", "-m", "c1: initial indexable tree")
	c1, _ := gitOut(root, "rev-parse", "HEAD")

	// Commit 2: apply the c2 change to the single file that differs.
	overlay(t, filepath.Join(productRoot(t), "testdata", "fixture", "c2"), root, "js/app.js")
	git("add", "-A")
	git("commit", "-q", "-m", "c2: add scale() export")
	c2, _ := gitOut(root, "rev-parse", "HEAD")

	return gitRepo{root: root, c1: c1, c2: c2}
}

func gitOut(dir, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// copyTree copies srcDir's files into dest, preserving relative paths.
func copyTree(t *testing.T, srcDir, dest string) {
	t.Helper()
	for _, rel := range walkRel(srcDir) {
		data, err := os.ReadFile(filepath.Join(srcDir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read fixture %s: %v", rel, err)
		}
		target := filepath.Join(dest, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", target, err)
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			t.Fatalf("write %s: %v", target, err)
		}
	}
}

// overlay copies a single fixture file (by repo-relative path) from srcDir
// into dest — used to apply the c2 diff without re-copying the whole tree.
func overlay(t *testing.T, srcDir, dest, rel string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(srcDir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read c2 overlay %s: %v", rel, err)
	}
	target := filepath.Join(dest, filepath.FromSlash(rel))
	if err := os.WriteFile(target, data, 0o644); err != nil {
		t.Fatalf("overlay %s: %v", target, err)
	}
}

// walkRel returns every file under dir as slash-separated relative paths.
func walkRel(dir string) []string {
	var out []string
	_ = filepath.Walk(dir, func(p string, d os.FileInfo, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return nil
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	return out
}

// openStore opens a throwaway SQLite store, applies the schema, and
// registers a repository keyed by its absolute root (the same ID scheme
// the CLI derives), so pipeline and CLI both address it identically.
func openStore(t *testing.T, root string) *store.Store {
	t.Helper()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "archaeologist.sqlite")
	st, err := store.Open(store.SQLite, dbPath)
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate store: %v", err)
	}
	if err := st.CreateRepository(ctx, store.Repository{
		ID: "repo-" + root, Name: filepath.Base(root), Origin: "local", Root: root,
	}); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	return st
}

// repoIDFor mirrors the CLI's identity scheme: "repo-" + absolute root.
func repoIDFor(root string) string { return "repo-" + root }

// indexFullWith runs a complete (non-incremental) pipeline index of
// `commit` using the caller-supplied dataDir (so sibling runs can share a
// manifest cache), recording which paths actually reached the parser via a
// recording runner that delegates to the real tree-sitter binary. It
// returns the recorded path set alongside the pipeline Result.
func indexFullWith(t *testing.T, st *store.Store, repo gitRepo, commit, dataDir string) (ingest.Result, []string) {
	t.Helper()
	bin := parserBin(t)
	pipe := ingest.New(st, repo.root, dataDir)
	var recorded []string
	pipe.SetParserRunner(func(ctx context.Context, _, commitArg, snap string, paths []string) (ingest.ParserManifest, error) {
		recorded = append([]string(nil), paths...)
		return invokeParser(ctx, bin, commitArg, snap, paths)
	})
	res, err := pipe.Run(context.Background(), ingest.Options{
		Commit:    commit,
		RepoID:    repoIDFor(repo.root),
		ParserBin: bin,
	})
	if err != nil {
		t.Fatalf("index %s: %v", commit, err)
	}
	return res, recorded
}

// indexFull is indexFullWith with a throwaway data dir.
func indexFull(t *testing.T, st *store.Store, repo gitRepo, commit string) (ingest.Result, []string) {
	t.Helper()
	return indexFullWith(t, st, repo, commit, t.TempDir())
}

// invokeParser mirrors ingest's default parser runner: feed the tree-sitter
// binary a stdin manifest built from the given repo-relative paths (read
// out of the snapshot dir) and return its parsed manifest.
func invokeParser(ctx context.Context, bin, commit, snap string, paths []string) (ingest.ParserManifest, error) {
	type inFile struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	in := struct {
		Commit   string  `json:"commit"`
		Language string  `json:"language"`
		Files    []inFile `json:"files"`
	}{Commit: commit, Language: "auto"}
	for _, rel := range paths {
		data, err := os.ReadFile(filepath.Join(snap, filepath.FromSlash(rel)))
		if err != nil {
			return ingest.ParserManifest{}, err
		}
		in.Files = append(in.Files, inFile{Path: filepath.ToSlash(rel), Content: string(data)})
	}
	buf, err := json.Marshal(in)
	if err != nil {
		return ingest.ParserManifest{}, err
	}
	cmd := exec.CommandContext(ctx, bin)
	cmd.Stdin = bytes.NewReader(buf)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return ingest.ParserManifest{}, err
	}
	var m ingest.ParserManifest
	if err := json.Unmarshal(out.Bytes(), &m); err != nil {
		return ingest.ParserManifest{}, err
	}
	return m, nil
}
