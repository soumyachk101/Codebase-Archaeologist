// Package git wraps the git CLI for commit-aware operations: snapshots of
// a commit, bounded history, commit diffs, and per-file touch history.
//
// Everything is bounded: history depth is a parameter, not a global.
// File lists come from `git log --name-only -z`, which is reliable and
// does not require a checkout.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Repo is a local git working tree.
type Repo struct {
	Path string
}

func run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

// IsRepo reports whether dir is a git work tree.
func (r *Repo) IsRepo(ctx context.Context) bool {
	out, err := run(ctx, r.Path, "rev-parse", "--git-dir")
	return err == nil && strings.TrimSpace(out) != ""
}

// HeadCommit returns the full SHA of HEAD and the short branch name.
func (r *Repo) HeadCommit(ctx context.Context) (commit, branch string, err error) {
	c, err := run(ctx, r.Path, "rev-parse", "HEAD")
	if err != nil {
		return "", "", err
	}
	commit = strings.TrimSpace(c)
	b, perr := run(ctx, r.Path, "symbolic-ref", "--short", "HEAD")
	if perr == nil {
		branch = strings.TrimSpace(b)
	}
	if branch == "" {
		branch = "HEAD"
	}
	return commit, branch, nil
}

// HeadCommitForRef resolves a ref (branch name, tag, or full SHA) to a full
// SHA.
func (r *Repo) HeadCommitForRef(ctx context.Context, ref string) (string, error) {
	out, err := run(ctx, r.Path, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// CommitMeta is one commit from the repository's history.
type CommitMeta struct {
	Commit  string   `json:"commit"`
	Parents []string `json:"parents"`
	Author  string   `json:"author"`
	Date    string   `json:"date"` // RFC3339
	Message string   `json:"message"`
	Files   []string `json:"files"`
}

// Log returns the most recent `depth` commits that exist in this history,
// newest first, with the files each commit touched.
func (r *Repo) Log(ctx context.Context, depth int) ([]CommitMeta, error) {
	if depth <= 0 {
		depth = 100
	}
	out, err := run(ctx, r.Path, "log",
		"--max-count", fmt.Sprint(depth),
		"--pretty=format:%H%x1f%P%x1f%an%x1f%aI%x1f%.",
		"--name-only", "-z")
	if err != nil {
		return nil, err
	}
	return parseZLog(out, depth)
}

// parseZLog decodes `git log -z --name-only` output with the record format
// `%H / %P / %an / %aI / %. , then NUL-separated file names`.
//
// Field order inside one record is positional:
//
//	[SHA, parents, author, date, subject, file1, file2, ...]
//
// until the next SHA begins the next record.
func parseZLog(raw string, limit int) ([]CommitMeta, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	fields := strings.Split(raw, "\x00")
	var out []CommitMeta
	var cur *CommitMeta
	pos := -1 // position within the current record
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if isSHA(f) {
			if cur != nil {
				if len(out) >= limit {
					return out, nil
				}
				out = append(out, *cur)
			}
			cur = &CommitMeta{Commit: f}
			pos = 0
			continue
		}
		if cur == nil {
			// NUL padding before the first record or between trailing fields.
			continue
		}
		if f != "" || pos >= 2 {
			// Empty leading fields (no parents, no subject) only matter at
			// position < 2; keep the positional slot in every case so the
			// record stays aligned.
			switch pos {
			case 1:
				cur.Parents = strings.Fields(f)
			case 2:
				cur.Author = f
			case 3:
				cur.Date = f
			case 4:
				cur.Message = f
			default: // pos >= 5
				if f != "" {
					cur.Files = append(cur.Files, f)
				}
			}
		}
		pos++
	}
	if cur != nil && len(out) < limit {
		out = append(out, *cur)
	}
	return out, nil
}

// isSHA reports whether s looks like a full or abbreviated commit SHA.
func isSHA(s string) bool {
	if len(s) < 7 || len(s) > 40 {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

// Diff lists files changed between two commits, classified as added,
// modified or deleted.
type DiffEntry struct {
	Status string // "A", "M", "D", "R" (rename has "old new")
	Path   string
}

// Diff compares commit a to commit b.
func (r *Repo) Diff(ctx context.Context, a, b string) ([]DiffEntry, error) {
	out, err := run(ctx, r.Path, "diff", "--name-status", a, b)
	if err != nil {
		return nil, err
	}
	var res []DiffEntry
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 2 {
			continue
		}
		res = append(res, DiffEntry{Status: parts[0], Path: parts[len(parts)-1]})
	}
	return res, nil
}

// FilesAtCommit lists every file in the tree at a commit.
func (r *Repo) FilesAtCommit(ctx context.Context, commit string) ([]string, error) {
	out, err := run(ctx, r.Path, "ls-tree", "-r", "--name-only", commit)
	if err != nil {
		return nil, err
	}
	var res []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if l != "" {
			res = append(res, l)
		}
	}
	return res, nil
}

// Snapshot extracts a commit's tree into dest (created if missing). This is
// the only mechanism by which source text reaches the parser; the parser
// sees exactly the file set the commit recorded.
func (r *Repo) Snapshot(ctx context.Context, commit, dest string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "git", "archive", commit)
	cmd.Dir = r.Path
	tarCmd := exec.Command("tar", "-x", "-C", dest)
	pr, err := cmd.Stdpipe() //nolint:staticcheck // pre-1.21 idiom
	if err != nil {
		return err
	}
	tarCmd.Stdin = pr
	tarCmd.Stdout = os.Stderr
	tarCmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	if err := tarCmd.Run(); err != nil {
		return fmt.Errorf("extract snapshot: %w", err)
	}
	return cmd.Wait()
}

// LastTouched returns up to `n` commit SHAs that touched file, newest
// first. Used to back file-level history timelines.
func (r *Repo) LastTouched(ctx context.Context, file string, n int) ([]string, error) {
	if n <= 0 {
		n = 50
	}
	args := []string{"log", "-n", fmt.Sprint(n), "--format=%H", "--", file}
	out, err := run(ctx, r.Path, args...)
	if err != nil {
		// A path that never existed yields exit 0 with empty output for
		// most git versions; treat errors conservatively.
		if out != "" {
			out = strings.TrimSpace(out)
		}
		if out == "" {
			return nil, nil
		}
		return nil, err
	}
	var out2 []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if l != "" {
			out2 = append(out2, l)
		}
	}
	return out2, nil
}
