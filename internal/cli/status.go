package cli

import (
	"github.com/ensembyte/archaeologist/internal/git"
	"github.com/spf13/cobra"
)

func newInit() *cobra.Command {
	var dataDir, root string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Prepare the local index store for the current repository",
		Long: "init registers the git working tree as a repository and creates " +
			"the local database. It does not index files; run 'archaeologist index' " +
			"to produce a commit-aware snapshot.",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := openApp(cmd, dataDir, root)
			if err != nil {
				return err
			}
			defer a.close()
			print(cmd, "repository registered: %s\n", a.repoID)
			print(cmd, "database driver:        %s\n", a.cfg.Database)
			print(cmd, "database ready.\n")
			return nil
		},
	}
	cmd.Flags().StringVar(&dataDir, "data-dir", "", "data directory for the SQLite file")
	cmd.Flags().StringVar(&root, "root", "", "repository root (default: current directory)")
	return cmd
}

func newStatus() *cobra.Command {
	var dataDir, root string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the current index run and its freshness",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := openApp(cmd, dataDir, root)
			if err != nil {
				return err
			}
			defer a.close()
			ctx := cmd.Context()

			repo := &git.Repo{Path: a.root}
			head, branch, herr := repo.HeadCommit(ctx)
			run, rerr := a.store.GetCurrentRun(ctx, a.repoID)
			if rerr != nil {
				print(cmd, "repository:  %s\n", a.repoID)
				print(cmd, "no complete index run yet; run 'archaeologist index'\n")
				return nil
			}
			commitDate := run.CommitDate.Format("2006-01-02")
			stale := herr == nil && head != run.Commit
			print(cmd, "repository:    %s\n", a.repoID)
			print(cmd, "indexed at:    %s (%s)\n", short(run.Commit), commitDate)
			print(cmd, "branch:        %s\n", run.Branch)
			if herr == nil {
				print(cmd, "head:          %s (%s)\n", short(head), branch)
			} else {
				print(cmd, "head:          unknown (git unavailable)\n")
			}
			if stale {
				print(cmd, "state:         STALE — branch advanced, re-index pending\n")
			} else {
				print(cmd, "state:         current\n")
			}
			files, _ := a.store.FileCount(ctx, run.Commit)
			syms, _ := a.store.SymbolCount(ctx, run.Commit)
			edges, _ := a.store.EdgeCount(ctx, run.Commit)
			print(cmd, "files: %d  symbols: %d  edges: %d\n", files, syms, edges)
			return nil
		},
	}
	cmd.Flags().StringVar(&dataDir, "data-dir", "", "data directory")
	cmd.Flags().StringVar(&root, "root", "", "repository root")
	return cmd
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
