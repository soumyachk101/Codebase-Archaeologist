package cli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ensembyte/archaeologist/internal/store"
	"github.com/spf13/cobra"
)

// newImpact wires `archaeologist impact <file-or-symbol>`: bounded reverse
// traversal over stored edges from a changed file or symbol, with affected
// modules and unresolved references surfaced separately — never merged
// into the static set.
func newImpact() *cobra.Command {
	var (
		dataDir string
		root    string
		depth   int
		limit   int
		asJSON  bool
	)
	cmd := &cobra.Command{
		Use:   "impact <file-or-symbol>",
		Short: "Change impact: bounded reverse-dependency traversal from a file or symbol",
		Long: "impact resolves the target to indexed symbols, walks stored reverse " +
			"edges to a bounded depth, and reports likely affected modules. Static and " +
			"unresolved edges are shown separately; unresolved references are hints, " +
			"never promoted to resolved. For dynamic languages this is a bounded static " +
			"view, not exhaustive impact coverage.",
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
			if depth <= 0 {
				depth = 1
			}
			if depth > 5 {
				depth = 5
			}

			// Resolve the target: a file path gives every symbol declared in
			// it; a symbol name gives exact-name matches.
			var roots []store.Symbol
			isFile, fErr := resolveFileTarget(ctx, a.store, run.Commit, args[0])
			if isFile {
				roots, fErr = a.store.SymbolsForFile(ctx, run.Commit, args[0])
			} else {
				roots, fErr = a.store.FindSymbolsByName(ctx, run.Commit, args[0], 20)
			}
			if fErr != nil {
				return fErr
			}
			if len(roots) == 0 {
				print(cmd, "target %q is not in the index at commit %s; not found in current index, no guess.\n",
					args[0], short(run.Commit))
				return nil
			}

			// Bounded BFS over reverse edges.
			type hop struct {
				sym   store.Symbol
				depth int
			}
			seen := map[string]int{}
			for _, r := range roots {
				seen[r.ID] = 0
			}
			var queue []hop
			for _, r := range roots {
				queue = append(queue, hop{r, 0})
			}
			type hit struct {
				Via      store.Edge `json:"via"`
				At       store.Symbol `json:"at"`
				Depth    int    `json:"depth"`
			}
			var static, unresolved []hit
			for len(queue) > 0 {
				h := queue[0]
				queue = queue[1:]
				if h.depth >= depth {
					continue
				}
				edges, err := a.store.DependentsOf(ctx, run.Commit, h.sym.ID)
				if err != nil {
					return err
				}
				for _, e := range edges {
					entry := hit{Via: e, Depth: h.depth + 1}
					if e.FromID != "" {
						if from, err := a.store.SymbolAt(ctx, run.Commit, e.FromID); err == nil {
							entry.At = from
						}
					}
					if !e.Resolved {
						unresolved = append(unresolved, entry)
						continue
					}
					static = append(static, entry)
					if prev, ok := seen[e.FromID]; ok && prev <= h.depth+1 {
						continue
					}
					seen[e.FromID] = h.depth + 1
					queue = append(queue, hop{entry.At, h.depth + 1})
					if len(static)+len(unresolved) >= 2000 {
						queue = nil
						break
					}
				}
			}
			if limit > 0 && limit < len(static) {
				static = static[:limit]
			}

			// Likely affected modules come only from symbols the store holds.
			modules := map[string]bool{}
			for _, h := range static {
				if h.At.Module != "" {
					modules[h.At.Module] = true
				}
			}
			var affected []string
			for m := range modules {
				affected = append(affected, m)
			}

			type report struct {
				Commit     string `json:"commit"`
				Target     string `json:"target"`
				Roots      int    `json:"roots"`
				Static     []hit  `json:"static"`
				Unresolved []hit  `json:"unresolved"`
				Modules    []string `json:"modules"`
			}
			rep := report{
				Commit:     run.Commit,
				Target:     args[0],
				Roots:      len(roots),
				Static:     static,
				Unresolved: unresolved,
				Modules:    affected,
			}
			if asJSON {
				if rep.Static == nil {
					rep.Static = []hit{}
				}
				if rep.Unresolved == nil {
					rep.Unresolved = []hit{}
				}
				return json.NewEncoder(cmd.OutOrStdout()).Encode(rep)
			}
			print(cmd, "impact of %q at commit %s (depth %d)\n", args[0], short(run.Commit), depth)
			print(cmd, "roots: %d symbol(s)\n", len(roots))
			print(cmd, "static reverse edges: %d\n", len(rep.Static))
			for _, h := range rep.Static {
				name := h.At.Name
				if name == "" {
					name = h.Via.FromID
				}
				print(cmd, "  %-24s via %s %s:%d\n", name, h.Via.Kind, h.Via.SourceFile, h.Via.SourceLine)
			}
			print(cmd, "unresolved references (kept as unknown): %d\n", len(rep.Unresolved))
			for _, h := range rep.Unresolved {
				print(cmd, "  %q at %s:%d  [%s]\n", h.Via.TargetHint, h.Via.SourceFile, h.Via.SourceLine, h.Via.Kind)
			}
			if len(affected) > 0 {
				print(cmd, "likely affected modules: %s\n", joinAll(affected))
			}
			if len(rep.Unresolved) > 0 {
				print(cmd, "note: for dynamic languages this is a bounded static view, not exhaustive impact coverage.\n")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dataDir, "data-dir", "", "data directory")
	cmd.Flags().StringVar(&root, "root", "", "repository root")
	cmd.Flags().IntVarP(&depth, "depth", "d", 1, "reverse traversal depth (max 5)")
	cmd.Flags().IntVarP(&limit, "limit", "l", 0, "cap on static edges printed")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the report as JSON")
	return cmd
}

// resolveFileTarget reports whether the target names a file path in the
// index rather than a symbol name.
func resolveFileTarget(ctx context.Context, st *store.Store, commit, target string) (bool, error) {
	for _, r := range target {
		if r == '/' {
			return true, nil
		}
	}
	syms, err := st.FindSymbolsByName(ctx, commit, target, 1)
	if err != nil {
		return false, err
	}
	return len(syms) == 0 && looksLikePath(target), nil
}

func looksLikePath(s string) bool {
	for _, r := range s {
		if r == '.' || r == '/' {
			return true
		}
	}
	return false
}

func joinAll(items []string) string {
	if len(items) == 0 {
		return ""
	}
	out := items[0]
	for i := 1; i < len(items); i++ {
		out += ", " + items[i]
	}
	return out
}

var _ = fmt.Sprintf
