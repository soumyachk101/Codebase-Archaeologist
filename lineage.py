"""Trace the origin of the code currently in a Git repository.

The tool walks the commit history from the first commit up to HEAD,
records which commit changed how many lines, and reports the lineage of
the resulting tree as JSON.
"""

import argparse
import json
import subprocess
import sys

# Use the ASCII unit separator so commit metadata can be pulled out of one
# `git log` line without ambiguity; it is safe inside subprocess argv.
_SEPARATOR = "\x1f"


class EmptyRepoError(Exception):
    """Raised when the repository has no commits on the requested ref."""


def _shas_root_to_head(repo):
    """Return the list of commit hashes from the first commit up to HEAD."""
    output = subprocess.check_output(
        ["git", "-C", repo, "log", "--reverse", "--pretty=format:%H"],
        text=True,
    )
    return [line for line in output.splitlines() if line]


def _diff_stat(repo, sha):
    """Return the number of lines that changed in commit `sha`.

    A commit's lines are attributed to it by diffing it against its
    parent; the root commit is diffed against the empty tree, which is
    what `git show` does automatically.
    """
    output = subprocess.check_output(
        ["git", "-C", repo, "show", "--numstat", "--pretty=format:", sha],
        text=True,
    )
    changed = 0
    for line in output.splitlines():
        # each numstat line is: <added>\t<removed>\t<path>
        added, removed, _path = line.split("\t")
        changed += int(added) + int(removed)
    return changed


def _commit_metadata(repo, sha):
    """Return a dict with the author, timestamps, and hash of commit `sha`."""
    raw = subprocess.check_output(
        [
            "git", "-C", repo, "log", "-1",
            f"--pretty=format:%an{_SEPARATOR}%at{_SEPARATOR}%ct{_SEPARATOR}%H",
            sha,
        ],
        text=True,
    )
    author, authored_at, committed_at, hexsha = raw.rstrip("\n").split(_SEPARATOR)
    return {
        "sha": hexsha,
        "author": author,
        "authored_at": int(authored_at),
        "committed_at": int(committed_at),
    }


def _parent_sha(repo, sha):
    """Return the parent hash of `sha`, or None for the root commit."""
    result = subprocess.run(
        ["git", "-C", repo, "rev-parse", "--verify", f"{sha}^"],
        text=True,
        capture_output=True,
    )
    if result.returncode != 0:
        return None
    return result.stdout.strip()


def lineage(repo):
    """Compute the lineage report for the current HEAD of `repo`.

    The report lists how many lines at HEAD were introduced by each
    commit, so readers can see how much of the codebase came from recent
    work and how much was inherited from older commits.
    """
    head_sha = subprocess.check_output(
        ["git", "-C", repo, "rev-parse", "HEAD"], text=True
    ).strip()
    if not head_sha:
        raise EmptyRepoError(f"no commits found in {repo}")

    lines_by_commit = {sha: _diff_stat(repo, sha) for sha in _shas_root_to_head(repo)}
    parent_sha = _parent_sha(repo, head_sha)
    report = {
        "head": _commit_metadata(repo, head_sha),
        "parent": _commit_metadata(repo, parent_sha) if parent_sha else None,
        "lines_by_commit": lines_by_commit,
    }
    return report


def main(argv=None):
    parser = argparse.ArgumentParser(
        description="Trace the origin of code in a Git repository."
    )
    parser.add_argument("repo", help="path to a Git repository to inspect")
    args = parser.parse_args(argv)
    try:
        report = lineage(args.repo)
    except EmptyRepoError as error:
        print(f"error: {error}", file=sys.stderr)
        return 1
    json.dump(report, sys.stdout, indent=2)
    sys.stdout.write("\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
