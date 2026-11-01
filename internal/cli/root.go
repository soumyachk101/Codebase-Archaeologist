// Package cli wires the cobra command tree for the archaeologist CLI.
package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

// Execute runs the root command with a signal-aware context.
func Execute(ctx context.Context) error {
	root := newRoot()
	if err := root.ExecuteContext(ctx); err != nil {
		return err
	}
	return nil
}

func newRoot() *cobra.Command {
	var version string
	root := &cobra.Command{
		Use:   "archaeologist",
		Short: "Index a repository into a commit-aware, evidence-linked architecture model",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	root.Version = "0.1.0"
	root.PersistentFlags().StringVar(&version, "version", "0.1.0", "version (display)")
	_ = version

	root.AddCommand(newInit())
	root.AddCommand(newIndex())
	root.AddCommand(newSearch())
	root.AddCommand(newExplain())
	root.AddCommand(newImpact())
	root.AddCommand(newStatus())
	root.AddCommand(newDependants())
	root.AddCommand(newPurge())
	return root
}

func print(cmd *cobra.Command, s string, a ...any) {
	fmt.Fprintf(cmd.OutOrStdout(), s, a...)
}
