// archaeologist-service — the Codebase Archaeologist API entrypoint.
//
// Wires the pinned Chi contract (service/internal/api) to the shared store
// layer: environment-driven configuration, one database dialect, auth and
// per-repository rate limiting, structured logging that never writes
// secrets, and graceful shutdown on SIGINT/SIGTERM.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ensembyte/archaeologist/internal/envx"
	"github.com/ensembyte/archaeologist/internal/store"
	"github.com/ensembyte/archaeologist/service/internal/api"
)

func main() {
	if err := run(); err != nil {
		log.Printf("service error: %v", err)
		os.Exit(1)
	}
}

func run() error {
	// 1. Configuration: envx owns the variable names and defaults; nothing
	// in here is a hardcoded secret.
	cfg, err := envx.Load()
	if err != nil {
		return err
	}
	if cfg.BindAddr == "" {
		return errors.New("bind address is empty: set ARCH_BIND")
	}

	// 2. Data layer: SQLite for local development, Postgres in production,
	// selected by envx's driver switch. The schema is dialect-neutral and
	// applied idempotently.
	var drv store.Driver
	if cfg.Local {
		drv = store.SQLite
	} else {
		drv = store.Postgres
	}
	st, err := store.Open(drv, cfg.Database)
	if err != nil {
		return fmt.Errorf("open %s database: %w", drv, err)
	}
	defer st.Close()
	if err := st.Migrate(context.Background()); err != nil {
		return fmt.Errorf("migrate schema: %w", err)
	}

	// 3. Handler chain: per-repository token bucket fronts the public read
	// endpoints; the API key authenticates every request when configured.
	handler := api.New(st)
	handler = api.RateLimiter(0, handler) // 0 -> api.go's default of 10 req/s
	if cfg.AuthKey != "" {
		handler = api.Auth(cfg.AuthKey, handler)
	}

	log.Printf("archaeologist service: driver=%s bind=%s auth=%s data_dir=%s",
		drv, cfg.BindAddr, authState(cfg.AuthKey), cfg.DataDir)

	// 4. Serve with graceful shutdown: SIGINT/SIGTERM stop the acceptor
	// and drain in-flight requests.
	srv := &http.Server{
		Addr:              cfg.BindAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Printf("shutting down: draining in-flight requests")
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if serr := srv.Shutdown(sctx); serr != nil {
			log.Printf("graceful shutdown failed: %v", serr)
		}
	}
	log.Printf("service stopped: driver=%s", drv)
	return nil
}

// authState renders the auth state for log lines only; the key itself is
// never written to logs.
func authState(key string) string {
	if key == "" {
		return "disabled"
	}
	return "enabled"
}
