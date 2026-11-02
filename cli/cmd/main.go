// archaeologist — local CLI that indexes a repository at a fixed commit,
// searches it, explains features with cited evidence, and runs impact
// analysis. Data lives in a local SQLite database by default; Postgres DSN
// may be supplied for shared deployments.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ensembyte/archaeologist/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := cli.Execute(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "error: ", err)
		os.Exit(1)
	}
}
