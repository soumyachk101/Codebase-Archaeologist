package archtest

import (
	"context"
	"io"
	"os"
	"testing"

	"github.com/ensembyte/archaeologist/internal/cli"
)

// CLI smoke: drive the real cobra command tree (index → search → explain)
// against the git fixture with the real tree-sitter binary, and assert the
// commands print structured output (not a crash, not a TODO placeholder).
// cobra reads os.Args, so we set them per invocation and capture the
// default os.Stdout.
func TestCLISmoke(t *testing.T) {
	bin := parserBin(t)
	gitAvailable(t)
	repo := buildGitFixture(t)
	dataDir := t.TempDir()

	// Wire the CLI to the real parser + a known data dir + local SQLite.
	t.Setenv("ARCH_PARSER_BIN", bin)
	t.Setenv("ARCH_DATA_DIR", dataDir)
	t.Setenv("ARCH_LOCAL_ONLY", "true")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("ARCH_POSTGRES_DSN", "")
	_ = os.Unsetenv("ARCH_AUTH_KEY")

	run := func(args ...string) (string, error) {
		orig := os.Args
		os.Args = append([]string{"archaeologist"}, args...)
		defer func() { os.Args = orig }()

		oldStdout := os.Stdout
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("pipe: %v", err)
		}
		os.Stdout = w
		exerr := cli.Execute(context.Background())
		w.Close()
		os.Stdout = oldStdout
		out, _ := io.ReadAll(r)
		return string(out), exerr
	}

	// 1. index HEAD (c2) through the CLI.
	out, err := run("index", "--data-dir", dataDir, "--root", repo.root)
	if err != nil {
		t.Fatalf("cli index: %v\noutput: %s", err, out)
	}
	if out == "" || contains(out, "TODO") {
		t.Fatalf("cli index produced no structured output (crash or placeholder): %q", out)
	}

	// 2. search a c1 symbol that survives into c2 (Normalize).
	out, err = run("search", "Normalize", "--data-dir", dataDir, "--root", repo.root)
	if err != nil {
		t.Fatalf("cli search Normalize: %v\noutput: %s", err, out)
	}
	if !contains(out, "Normalize") {
		t.Errorf("cli search Normalize did not surface the symbol:\n%s", out)
	}

	// 3. search the c2-only export — it must now be found at HEAD.
	out, err = run("search", "scale", "--data-dir", dataDir, "--root", repo.root)
	if err != nil {
		t.Fatalf("cli search scale: %v\noutput: %s", err, out)
	}
	if !contains(out, "scale") {
		t.Errorf("cli search scale (a c2-only export) should be found at HEAD:\n%s", out)
	}

	// 4. explain a well-evidenced Go function.
	out, err = run("explain", "Normalize", "--data-dir", dataDir, "--root", repo.root)
	if err != nil {
		t.Fatalf("cli explain Normalize: %v\noutput: %s", err, out)
	}
	if !contains(out, "Normalize") {
		t.Errorf("cli explain Normalize produced no answer:\n%s", out)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
