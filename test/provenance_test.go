package archtest

import (
	"context"
	"testing"

	"github.com/ensembyte/archaeologist/internal/ingest"
)

// Provenance-invariant: after a full index run, the durable fact set obeys
// the hard guarantee — every edge and symbol carries a concrete
// source-file + line + commit tuple, resolved edges point at symbols that
// exist at that commit, and unresolved references persist a target hint
// rather than an invented target. store.VerifyProvenance is the checker.
func TestProvenanceInvariant(t *testing.T) {
	repo := buildGitFixture(t)
	st := openStore(t, repo.root)
	defer st.Close()

	dataDir := t.TempDir()
	res, _ := indexFullWith(t, st, repo, repo.c1, dataDir)
	if res.Edges == 0 {
		t.Fatalf("index produced no edges; expected at least the Go cross-package import and JS/Go call facts")
	}
	// The fixture intentionally leaves external references unresolved
	// (import "some-external-lib", import "math"): they must survive as
	// unresolved hints, never be dropped and never be invented.
	if res.Unresolved == 0 {
		t.Fatalf("expected unresolved external references to persist, got none")
	}

	if v, err := st.VerifyProvenance(context.Background()); err != nil {
		t.Fatalf("VerifyProvenance: %v", err)
	} else {
		for _, x := range v {
			t.Errorf("provenance violation [%s %s]: %s", x.Table, x.ID, x.Detail)
		}
	}
}

// The invariant must also hold for an incremental run, where unchanged
// files are carried from the parent manifest cache and re-stamped to the
// new commit. Both runs share one data dir so c1's cache persists for
// c2's carry-over.
func TestProvenanceInvariantIncremental(t *testing.T) {
	repo := buildGitFixture(t)
	st := openStore(t, repo.root)
	defer st.Close()

	dataDir := t.TempDir()
	// Full index of c1 first (builds the manifest cache c2 can carry from).
	if _, _ := indexFullWith(t, st, repo, repo.c1, dataDir); _ != nil {
	}

	// Incremental c2 run: only the changed file (js/app.js) is re-parsed.
	bin := parserBin(t)
	pipe := ingest.New(st, repo.root, dataDir)
	var reParsed []string
	pipe.SetParserRunner(func(ctx context.Context, _, commitArg, snap string, paths []string) (ingest.ParserManifest, error) {
		reParsed = append([]string(nil), paths...)
		return invokeParser(ctx, bin, commitArg, snap, paths)
	})
	res, err := pipe.Run(context.Background(), ingest.Options{
		Commit: repo.c2, RepoID: repoIDFor(repo.root),
		Incremental: true, ParentCommit: repo.c1, ParserBin: bin,
	})
	if err != nil {
		t.Fatalf("incremental index c2: %v", err)
	}
	if res.Files == 0 {
		t.Fatalf("incremental run committed no files")
	}
	// Carried-over files must still satisfy the invariant after re-stamp.
	if v, verr := st.VerifyProvenance(context.Background()); verr != nil {
		t.Fatalf("VerifyProvenance after incremental: %v", verr)
	} else {
		for _, x := range v {
			t.Errorf("provenance violation after incremental [%s %s]: %s", x.Table, x.ID, x.Detail)
		}
	}
	// The only changed file between c1 and c2 is js/app.js.
	onlyChanged := len(reParsed) == 1 && reParsed[0] == "js/app.js"
	if !onlyChanged {
		t.Errorf("expected exactly js/app.js re-parsed in the incremental run, got %v", reParsed)
	}
}
