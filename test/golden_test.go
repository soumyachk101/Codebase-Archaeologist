package archtest

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Parser golden: invoke the real tree-sitter binary against the committed
// c1 fixture and assert the emitted manifest matches the committed golden
// byte-for-byte. This is the regression anchor for the extraction logic —
// a parser change that alters the fact set (symbols/imports/calls) fails
// here and must be reviewed before the golden is regenerated.
func TestParserGolden(t *testing.T) {
	bin := parserBin(t)
	root := productRoot(t)
	base := filepath.Join(root, "testdata", "fixture", "c1")
	goldenPath := filepath.Join(base, "c1.golden.json")

	golden, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v", goldenPath, err)
	}

	// Build the stdin manifest from the fixture's source files.
	type inFile struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	var files []inFile
	for _, rel := range walkRel(base) {
		if rel == "c1.golden.json" {
			continue // the golden is an output, not an input
		}
		data, err := os.ReadFile(filepath.Join(base, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read fixture %s: %v", rel, err)
		}
		files = append(files, inFile{Path: rel, Content: string(data)})
	}
	in := struct {
		Commit   string   `json:"commit"`
		Language string   `json:"language"`
		Files    []inFile `json:"files"`
	}{Commit: "c1", Language: "auto", Files: files}
	// walkRel includes the "c1.golden.json" guard above; keep the input
	// manifest keyed by the fixture's own relative layout.
	inJSON, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}

	// The golden was captured with paths relative to the fixture dir; the
	// walk above is already fixture-relative, so the two line up.
	cmd := exec.Command(bin)
	cmd.Stdin = bytes.NewReader(inJSON)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("run parser: %v: %s", err, errb.String())
	}

	// Byte-determinism check (the parser emits a canonical ordering).
	if !bytes.Equal(out.Bytes(), golden) {
		// Show a compact diff-friendly summary, not a 9KB blob.
		var got, want map[string]any
		_ = json.Unmarshal(out.Bytes(), &got)
		_ = json.Unmarshal(golden, &want)
		t.Fatalf("parser output diverged from golden (commit=c1):\n got bytes=%d want bytes=%d\n"+
			"regenerate the golden only after verifying the extraction change is intended.\n"+
			"got   file count=%v\nwant  file count=%v",
			len(out.Bytes()), len(golden), fileCount(got), fileCount(want))
	}

	// Structural spot-checks on top of the byte match: the fixture's known
	// facts must be present (this makes a regressed golden readable, not
	// just "bytes differ").
	var m map[string]any
	if err := json.Unmarshal(out.Bytes(), &m); err != nil {
		t.Fatalf("parser output not JSON: %v", err)
	}
	mani, _ := json.Marshal(m)
	if !bytes.Contains(mani, []byte(`"total"`)) {
		t.Errorf("golden lacks the exported JS symbol `total` — extraction regression?")
	}
	if !bytes.Contains(mani, []byte(`"example.com/fixture/gomod/mathx"`)) {
		t.Errorf("golden lacks the Go cross-package import spec — extraction regression?")
	}
	_ = context.Background() // keep import for symmetry with pipeline tests
}

func fileCount(m map[string]any) int {
	if m == nil {
		return -1
	}
	if f, ok := m["files"].([]any); ok {
		return len(f)
	}
	return -1
}
