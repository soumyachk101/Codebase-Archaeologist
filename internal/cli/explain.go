package cli

import (
	"encoding/json"
	"fmt"

	"github.com/ensembyte/archaeologist/internal/explain"
	"github.com/spf13/cobra"
)

// newExplain wires `archaeologist explain "target"`: a structured,
// evidence-grounded answer. Every claim carries an EvidenceReference
// (file, line range, symbol, commit) drawn only from indexed facts; when
// coverage is too low the command prints an abstain with the named missing
// evidence instead of guessing.
func newExplain() *cobra.Command {
	var (
		dataDir string
		root    string
		repo    string
		file    string
		asJSON  bool
	)
	cmd := &cobra.Command{
		Use:   `explain "symbol-or-module"`,
		Short: "Explain a symbol or module with exact, commit-stamped evidence",
		Long: "explain runs the evidence-grounded answer composer against the newest " +
			"complete index run. Every factual claim is backed by an EvidenceReference " +
			"(file, line range, commit) read directly from the store; the measured facts " +
			"are never rewritten. When coverage is too low the answer is an explicit " +
			"abstain that names the missing evidence — never a guess.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := openApp(cmd, dataDir, root)
			if err != nil {
				return err
			}
			defer a.close()
			// --repo overrides the repository scope when the target is a
			// symbol name only and the user knows the repo ID.
			repoID := a.repoID
			if repo != "" {
				repoID = repo
			}
			ans, err := explain.Explain(cmd.Context(), a.store, explain.Request{
				RepoID: repoID,
				Target: args[0],
				File:   file,
			})
			if err != nil {
				return err
			}
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(ans)
			}
			if ans.Abstained {
				print(cmd, "ABSTAIN: %s\n", ans.AbstainedReason)
				return nil
			}
			print(cmd, "target:    %s\n", ans.Target)
			print(cmd, "commit:    %s  (run %s)\n", short(ans.Commit), ans.RunID)
			print(cmd, "coverage:  %s\n", ans.Coverage)
			if ans.Summary != "" {
				print(cmd, "summary:   %s (derived)\n", ans.Summary)
			}
			print(cmd, "--- claims ---\n")
			for i, c := range ans.Claims {
				print(cmd, "  %d. %s\n", i+1, c)
			}
			print(cmd, "--- evidence (%d) ---\n", len(ans.Evidence))
			for _, e := range ans.Evidence {
				print(cmd, "  %s:%d%s  [%s] %s%s\n", e.File, e.Line, lineRange(e.EndLine),
					e.Symbol, noteTag(e.Note), e.Snippet)
			}
			if len(ans.Unresolved) > 0 {
				print(cmd, "--- unresolved references (%d) — kept as unknown, not invented ---\n",
					len(ans.Unresolved))
				for _, e := range ans.Unresolved {
					print(cmd, "  %s:%d  %q\n", e.File, e.Line, e.Snippet)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dataDir, "data-dir", "", "data directory")
	cmd.Flags().StringVar(&root, "root", "", "repository root")
	cmd.Flags().StringVar(&repo, "repo", "", "repository scope (store ID) when --root is not the indexed repo")
	cmd.Flags().StringVar(&file, "file", "", "disambiguate a symbol name to one file")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the answer as JSON")
	return cmd
}

func lineRange(end int32) string {
	if end > 0 && end != 1 {
		return fmt.Sprintf("–%d", end)
	}
	return ""
}

func noteTag(n string) string {
	if n == "" {
		return ""
	}
	return " (" + n + ")"
}
