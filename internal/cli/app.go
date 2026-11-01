package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ensembyte/archaeologist/internal/envx"
	"github.com/ensembyte/archaeologist/internal/store"
	"github.com/spf13/cobra"
)

// app is the shared state for a command invocation.
type app struct {
	store *store.Store
	cfg   *envx.Config
	// root is the absolute path of the git working tree being indexed.
	root string
	// repoID is the store identity of that working tree.
	repoID string
}

// openApp loads configuration, opens the database, migrates it, and
// registers the current working directory as a repository. dataDirOverride
// and rootOverride are empty in normal use.
func openApp(cmd *cobra.Command, dataDirOverride, rootOverride string) (*app, error) {
	cfg, err := envx.Load()
	if err != nil {
		return nil, err
	}
	if dataDirOverride != "" {
		cfg.DataDir = dataDirOverride
		if cfg.Local {
			cfg.Database = filepath.Join(cfg.DataDir, "archaeologist.sqlite")
		}
	}

	var drv store.Driver
	if cfg.Local {
		drv = store.SQLite
	} else {
		drv = store.Postgres
	}
	st, err := store.Open(drv, cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("open %s database: %w", drv, err)
	}

	root := rootOverride
	if root == "" {
		wd, _ := os.Getwd()
		root = wd
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		st.Close()
		return nil, err
	}
	id := "repo-" + abs
	created := store.Repository{ID: id, Name: filepath.Base(abs), Origin: "local", Root: abs}
	if err := st.CreateRepository(cmd.Context(), created); err != nil {
		st.Close()
		return nil, err
	}

	return &app{store: st, cfg: cfg, root: abs, repoID: id}, nil
}

// close releases the database handle.
func (a *app) close() { a.store.Close() }
