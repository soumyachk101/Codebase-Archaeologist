package cli

import (
	"encoding/json"

	"github.com/ensembyte/archaeologist/internal/search"
	"github.com/spf13/cobra"
)

// newSearch wires `archaeologist search "query"`: hybrid, commit-stamped,
// cursor-paginated symbol search over the store.
func newSearch() *cobra.Command {
	var (
		dataDir string
		root    string
		kind    string
		limit   int
		cursor  string
		asJSON  bool
	)
	cmd := &cobra.Command{
		Use:   `search "query"`,
		Short: "Hybrid symbol search over the current index, with commit-stamped evidence",
		Long: "search combines exact symbol/keyword matches with weighted fuzzy " +
			"scoring against the newest complete index run. Every result carries the " +
			"commit and run it came from plus a file/line evidence reference. When the " +
			"query has no coverage at the current commit the command says so; it never " +
			"falls back to another commit.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := openApp(cmd, dataDir, root)
			if err != nil {
				return err
			}
			defer a.close()
			page, err := search.Search(cmd.Context(), a.store, search.Options{
				RepoID: a.repoID,
				Q:      args[0],
				Kind:   kind,
				Limit:  limit,
				Offset: search.OffsetFromCursor(cursor),
			})
			if err != nil {
				return err
			}
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(page)
			}
			if page.NotFound {
				print(cmd, "no results: %q has no coverage at commit %s; not found in current index, no fallback.\n",
					args[0], short(page.Commit))
				return nil
			}
			print(cmd, "%d result(s) at commit %s (run %s)\n", page.Total, short(page.Commit), page.RunID)
			for _, r := range page.Items {
				print(cmd, "  %s  [%s %s] %s:%d–%d  score %.1f\n",
					r.Name, r.Kind, r.Language, r.File, r.StartLine, r.EndLine, r.Score)
			}
			if page.NextCursor != "" {
				print(cmd, "more results: --cursor %s\n", page.NextCursor)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dataDir, "data-dir", "", "data directory")
	cmd.Flags().StringVar(&root, "root", "", "repository root")
	cmd.Flags().StringVar(&kind, "type", "", "restrict by symbol kind (function, class, method, …)")
	cmd.Flags().IntVarP(&limit, "limit", "l", 20, "results per page")
	cmd.Flags().StringVar(&cursor, "cursor", "", "offset cursor from a previous page")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the page as JSON")
	return cmd
}
