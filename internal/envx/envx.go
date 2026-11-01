// Package envx holds the application configuration, loaded from the
// environment and .env files. No values in here are secrets-in-code; every
// credential is sourced from the environment or a .env file at runtime.
package envx

import (
	"fmt"
	"os"
	"strings"
)

// Config is the full runtime configuration of one product instance.
type Config struct {
	// Database is the DSN. With Local=true the driver is SQLite and the
	// value is a file path; otherwise it is a Postgres DSN.
	Database string
	// Local forces the SQLite driver (and hence local-only data storage).
	Local bool
	// BindAddr is the listen address of the service.
	BindAddr string
	// AuthKey is the API key for local single-user mode. Empty disables
	// authentication (CLI-only usage); when set, every service request must
	// carry it in the Authorization header.
	AuthKey string
	// DataDir is where the SQLite database file and git working snapshots live.
	DataDir string
	// HistoryDepth bounds git history traversal (number of commits).
	HistoryDepth int
	// ParserBin is the path to the Rust parser binary. Empty means the
	// "archaeologist-parser" binary on $PATH.
	ParserBin string
	// LogLevel is one of debug, info, warn, error.
	LogLevel string
}

// Load reads configuration from the environment, applying defaults for
// anything unset. Values come from standard environment variables and
// from .env / .env.local files in the working directory when present.
func Load() (*Config, error) {
	loadDotEnv(".")
	loadDotEnv("./config")

	c := &Config{
		BindAddr:     envOr("ARCH_BIND", ":8080"),
		DataDir:      envOr("ARCH_DATA_DIR", "./data"),
		HistoryDepth: 100,
		ParserBin:    envOr("ARCH_PARSER_BIN", "archaeologist-parser"),
		LogLevel:     envOr("ARCH_LOG_LEVEL", "info"),
		AuthKey:      os.Getenv("ARCH_AUTH_KEY"),
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn != "" {
		c.Database = dsn
	} else if p := os.Getenv("ARCH_POSTGRES_DSN"); p != "" {
		c.Database = p
	} else {
		// Local default: SQLite file under the data directory.
		c.Local = true
		c.Database = fmt.Sprintf("%s/archaeologist.sqlite", c.DataDir)
	}
	if os.Getenv("ARCH_LOCAL_ONLY") == "true" {
		c.Local = true
		if c.Database == "" {
			c.Database = fmt.Sprintf("%s/archaeologist.sqlite", c.DataDir)
		}
	}
	if c.Database == "" {
		return nil, fmt.Errorf("no database configured: set DATABASE_URL or use local SQLite")
	}
	return c, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// loadDotEnv reads key=value pairs from a .env-style file into the process
// environment. Existing environment variables win; lines are trimmed and
// blank/# lines are ignored. A missing file is not an error.
func loadDotEnv(dir string) {
	data, err := os.ReadFile(dir + "/.env")
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		if _, exists := os.LookupEnv(k); !exists {
			os.Setenv(k, v)
		}
	}
}
