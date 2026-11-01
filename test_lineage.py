"""Tests for lineage.py, run with `python3 -m unittest` or pytest."""

import json
import os
import subprocess
import sys
import tempfile


def _make_repo_with_commits(directory, commits):
    """Create a Git repository in `directory` with the given number of commits."""
    subprocess.run(["git", "init", "-q", directory], check=True)
    for index in range(commits):
        with open(os.path.join(directory, "notes.txt"), "a") as handle:
            handle.write(f"line {index}\n")
        subprocess.run(
            [
                "git", "-C", directory,
                "add", "notes.txt",
                "-c", "user.name=Test", "-c", "user.email=test@example.com",
                "commit", "-q", "-m", f"commit {index}",
            ],
            check=True,
        )


def _run_lineage(directory):
    """Run lineage.py against `directory` and return (returncode, parsed JSON, stderr)."""
    script = os.path.join(os.path.dirname(__file__), "lineage.py")
    result = subprocess.run(
        [sys.executable, script, directory],
        capture_output=True,
        text=True,
    )
    report = json.loads(result.stdout) if result.stdout.strip() else None
    return result.returncode, report, result.stderr


def test_multi_commit_repo_reports_parents():
    with tempfile.TemporaryDirectory() as directory:
        _make_repo_with_commits(directory, commits=3)
        returncode, report, stderr = _run_lineage(directory)
        assert returncode == 0, stderr
        assert report["parent"] is not None, "a 3-commit repo should have a parent commit"
        assert len(report["lines_by_commit"]) == 3


def test_single_commit_repo_succeeds():
    """A repository with exactly one commit should not crash the tool."""
    with tempfile.TemporaryDirectory() as directory:
        _make_repo_with_commits(directory, commits=1)
        returncode, report, stderr = _run_lineage(directory)
        assert returncode == 0, stderr
        assert report["parent"] is None, "the root commit has no parent commit"
        assert len(report["lines_by_commit"]) == 1
