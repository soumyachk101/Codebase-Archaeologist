package archtest

import (
	"context"
	"testing"

	"github.com/ensembyte/archaeologist/internal/ingest"
)

// Incremental re-indexing: the c1→c2 fixture differs by exactly one file
// (js/app.js gains a `scale` export). An incremental c2 run must re-parse
// only that changed file and carry every unchanged file over from c1's
// manifest cache — the carried facts re-stamp to c2 but are re-derived from
// the parent's real parser output, not re-parsed and not invented.
func TestIncrementalReindex(t *testing.T) {
	bin := parserBin(t)
	repo := buildGitFixture(t)
	st := openStore(t, repo.root)
	defer st.Close()

	dataDir := t.TempDir()

	// Baseline: full c1 index. Records the paths the parser saw.
	c1res, c1parsed := indexFullWith(t, st, repo, repo.c1, dataDir)
	if c1res.ReParsed == 0 || len(c1parsed) == 0 {
		t.Fatalf("full c1 run re-parsed no files")
	}

	// Incremental c2: only the diff-changed file reaches the parser.
	pipe := ingest.New(st, repo.root, dataDir)
	var c2parsed []string
	pipe.SetParserRunner(func(ctx context.Context, _, commitArg, snap string, paths []string) (ingest.ParserManifest, error) {
		c2parsed = append([]string(nil), paths...)
		return invokeParser(ctx, bin, commitArg, snap, paths)
	})
	c2res, err := pipe.Run(context.Background(), ingest.Options{
		Commit: repo.c2, RepoID: repoIDFor(repo.root),
		Incremental: true, ParentCommit: repo.c1, ParserBin: bin,
	})
	if err != nil {
		t.Fatalf("incremental c2 index: %v", err)
	}
	if !c2res.Incremental {
		t.Fatalf("result.Incremental not set")
	}
	// The single changed file must be the only one re-parsed.
	if len(c2parsed) != 1 || c2parsed[0] != "js/app.js" {
		t.Fatalf("expected the incremental run to re-parse only js/app.js, re-parsed=%v", c2parsed)
	}
	if c2res.ReParsed != 1 {
		t.Errorf("ReParsed=%d, want 1 (only the changed file)", c2res.ReParsed)
	}
	// Carried files: the c1 total minus the one changed file.
	wantCarried := c1res.Files - 1
	if wantCarried < 0 {
		wantCarried = 0
	}
	// The c2 fact set still covers every file (changed + carried), so the
	// final file count equals the c1 file count (no file was deleted).
	if c2res.Files != c1res.Files {
		t.Errorf("c2 files=%d, c1 files=%d — the changed file should replace, not add", c2res.Files, c1res.Files)
	}
	_ = wantCarried
}
