package cli

import (
	"github.com/ensembyte/archaeologist/internal/git"
	"github.com/spf13/cobra"
)

// newPurge wires `archaeologist purge`: delete this repository's index
// state — all runs, facts, co-change and commit records — while keeping
// the working tree. The git repository itself is never touched.
func newPurge() *cobra.Command {
	var (
		dataDir string
		root    string
		yes     bool
	)
	cmd := &cobra.Command{
		Use:   "purge",
		Short: "Delete this repository's index state (all runs and facts)",
		Long: "purge removes every index run, file, symbol, edge, co-change and " +
			"commit record for this repository from the store. The git working tree " +
			"and the repository identity are kept; the next 'index' rebuilds from " +
			"scratch. Requires --yes; it is destructive by design.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				print(cmd, "refusing to purge without --yes: this deletes every index run and fact for %s\n", "this repository")
				return cmd.Help()
			}
			a, err := openApp(cmd, dataDir, root)
			if err != nil {
				return err
			}
			defer a.close()
			ctx := cmd.Context()
			// Confirm we are inside the git tree we claim to be purging:
			// the repository ID is derived from the absolute path.
			repo := &git.Repo{Path: a.root}
			if !repo.IsRepo(ctx) {
				return cmd.Errorf("%s is not a git working tree; nothing indexed under it", a.root)
			}
			if err := a.store.DeleteRepositoryFully(ctx, a.repoID); err != nil {
				return err
			}
			print(cmd, "purged all index state for %s\n", a.repoID)
			print(cmd, "working tree at %s is unchanged; run 'archaeologist index' to rebuild.\n", a.root)
			return nil
		},
	}
	cmd.Flags().StringVar(&dataDir, "data-dir", "", "data directory")
	cmd.Flags().StringVar(&root, "root", "", "repository root")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm the destructive purge")
	return cmd
}
