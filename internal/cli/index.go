package cli

import (
	"fmt"

	"github.com/ensembyte/archaeologist/internal/ingest"
	"github.com/ensembyte/archaeologist/internal/git"
	"github.com/spf13/cobra"
)

func newIndex() *cobra.Command {
	var (
		dataDir  string
		root     string
		commit   string
		ref      string
		depth    int
		incremental bool
		parent   string
	)
	cmd := &cobra.Command{
		Use:   "index [commit|ref]",
		Short: "Index the repository at a fixed commit",
		Long: "index snapshots the repository at a commit, runs the tree-sitter " +
			"parser, resolves references, and commits a commit-aware architecture " +
			"model. The argument may be a full SHA, branch, or tag; HEAD when omitted.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 && (commit == "") {
				commit = args[0]
			}
			a, err := openApp(cmd, dataDir, root)
			if err != nil {
				return err
			}
			defer a.close()
			ctx := cmd.Context()

			// Confirm the target is a git work tree so provenance is real.
			repo := &git.Repo{Path: a.root}
			if !repo.IsRepo(ctx) {
				return fmt.Errorf("%s is not a git working tree; index requires a repository", a.root)
			}

			pipe := ingest.New(a.store, a.root, a.cfg.DataDir)
			res, err := pipe.Run(ctx, ingest.Options{
				Commit:       commit,
				Ref:          ref,
				RepoID:       a.repoID,
				ParserBin:    a.cfg.ParserBin,
				HistoryDepth: depth,
				Incremental:  incremental,
				ParentCommit: parent,
			})
			if err != nil {
				return err
			}
			print(cmd, "indexed %s (%s) on %s\n", short(res.Commit), a.repoID, res.Branch)
			print(cmd, "files: %d  symbols: %d  edges: %d (unresolved %d)\n",
				res.Files, res.Symbols, res.Edges, res.Unresolved)
			print(cmd, "history commits: %d\n", res.Commits)
			if res.Unresolved > 0 {
				print(cmd, "note: %d reference(s) could not be resolved; they are kept as unknown, not invented.\n", res.Unresolved)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dataDir, "data-dir", "", "data directory")
	cmd.Flags().StringVar(&root, "root", "", "repository root")
	cmd.Flags().StringVarP(&commit, "commit", "c", "", "exact commit SHA to index")
	cmd.Flags().StringVarP(&ref, "ref", "r", "", "branch or tag to resolve and index")
	cmd.Flags().IntVarP(&depth, "depth", "d", 100, "history depth (commits walked back)")
	cmd.Flags().BoolVarP(&incremental, "incremental", "i", false, "re-index only files changed since --parent")
	cmd.Flags().StringVar(&parent, "parent", "", "previous commit when --incremental")
	return cmd
}
