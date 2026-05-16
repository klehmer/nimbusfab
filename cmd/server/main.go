// Command nimbusfab-server runs the nimbusfab web backend. It wraps the
// same Engine library the CLI uses; UI Phase 1 mounts a read-only browser
// UI over the SQLite inventory.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/klehmer/nimbusfab/internal/inventory/sqlite"
	"github.com/klehmer/nimbusfab/internal/webapi"
	"github.com/klehmer/nimbusfab/pkg/inventory"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	addr := envDefault("NIMBUSFAB_LISTEN_ADDR", ":8080")
	dsn := envDefault("NIMBUSFAB_DB_DSN", "sqlite:./nimbusfab.db")
	orgID := envDefault("NIMBUSFAB_ORG_ID", "default")
	apiToken := os.Getenv("NIMBUSFAB_API_TOKEN")

	repo, err := openRepo(ctx, dsn)
	if err != nil {
		return fmt.Errorf("open repo (%s): %w", dsn, err)
	}

	handler, err := webapi.New(webapi.Config{
		Repo: repo, OrgID: orgID, APIToken: apiToken,
	})
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	authNote := "API token required (Bearer)"
	if apiToken == "" {
		authNote = "API unauthenticated (set NIMBUSFAB_API_TOKEN to require Bearer auth)"
	}
	errCh := make(chan error, 1)
	go func() {
		fmt.Printf("nimbusfab-server listening on %s (HTTP Phase 1; UI auth disabled; org=%s; %s)\n", addr, orgID, authNote)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

// openRepo opens the SQLite inventory. Postgres support lands in Inventory
// Phase 2.
func openRepo(ctx context.Context, dsn string) (inventory.Repo, error) {
	r, err := sqlite.Open(dsn)
	if err != nil {
		return nil, err
	}
	if err := r.Migrate(ctx); err != nil {
		_ = r.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return r, nil
}

func envDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
