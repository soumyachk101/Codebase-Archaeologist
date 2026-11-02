package cli

import (
	"encoding/json"

	"github.com/ensembyte/archaeologist/internal/store"
	"github.com/spf13/cobra"
)

// newDependants wires `archaeologist dependants <symbol>`: the reverse
// dependencies of a symbol — every stored edge pointing at it, each with
// the edge's own source fact (file, line, kind, resolved/unresolved).
func newDependants() *cobra.Command {
	var (
		dataDir string
		root    string
		kind    string
		asJSON  bool
	)
	cmd := &cobra.Command{
		Use:   "dependants <symbol-name>",
		Short: "List the reverse dependencies (callers/importers) of a symbol",
		Long: "dependants resolves the target to a symbol at the current index commit " +
			"and prints every stored edge pointing at it. Each entry carries the edge's " +
			"source fact: where the reference was observed (file + line), its kind and " +
			"whether it resolved. Unresolved references are shown, clearly marked — they " +
			"are kept as unknown, not invented.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := openApp(cmd, dataDir, root)
			if err != nil {
				return err
			}
			defer a.close()
			ctx := cmd.Context()
			run, err := a.store.GetCurrentRun(ctx, a.repoID)
			if err == store.ErrNotFound {
				print(cmd, "no complete index run yet; run 'archaeologist index' first\n")
				return nil
			}
			if err != nil {
				return err
			}
			cands, err := a.store.FindSymbolsByName(ctx, run.Commit, args[0], 20)
			if err != nil {
				return err
			}
			if kind != "" {
				kept := cands[:0]
				for _, c := range cands {
					if c.Kind == kind {
						kept = append(kept, c)
					}
				}
				cands = kept
			}
			if len(cands) == 0 {
				print(cmd, "no symbol named %q at commit %s; not found in current index.\n", args[0], short(run.Commit))
				return nil
			}
			type row struct {
				Dependant struct {
					ID       string `json:"id"`
					Name     string `json:"name"`
					File     string `json:"file"`
					Line     int32  `json:"line"`
					Kind     string `json:"kind"`
				} `json:"dependant"`
				store.Edge
			}
			var out []row
			for _, c := range cands {
				edges, err := a.store.DependentsOf(ctx, run.Commit, c.ID)
				if err != nil {
					return err
				}
				for _, e := range edges {
					from, sErr := a.store.SymbolAt(ctx, run.Commit, e.FromID)
					if sErr != nil {
						// FromID can be a file module symbol or synthetic:
						// show the source fact without inventing a symbol.
						from = store.Symbol{ID: e.FromID, Name: e.FromID}
					}
					out = append(out, row{
						Dependant: struct {
							ID     string `json:"id"`
							Name   string `json:"name"`
							File   string `json:"file"`
							Line   int32  `json:"line"`
							Kind   string `json:"kind"`
						}{from.ID, from.Name, from.File, from.StartLine, from.Kind},
						Edge: e,
					})
				}
			}
			if asJSON {
				if out == nil {
					out = []row{}
				}
				return json.NewEncoder(cmd.OutOrStdout()).Encode(out)
			}
			print(cmd, "%d reverse edge(s) at commit %s\n", len(out), short(run.Commit))
			for _, r := range out {
				mark := "static"
				if !r.Edge.Resolved {
					mark = "UNRESOLVED"
				}
				print(cmd, "  %-36s %s at %s:%d  [%s] (%s)\n",
					r.Dependant.Name, r.Edge.Kind, r.Edge.SourceFile, r.Edge.SourceLine, r.Edge.Confidence, mark)
				if !r.Edge.Resolved {
					print(cmd, "      hint: %q (kept as unknown, not invented)\n", r.Edge.TargetHint)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dataDir, "data-dir", "", "data directory")
	cmd.Flags().StringVar(&root, "root", "", "repository root")
	cmd.Flags().StringVar(&kind, "type", "", "restrict dependants by symbol kind")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the edges as JSON")
	return cmd
}
