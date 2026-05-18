// Command nimbusfab-server runs the nimbusfab web backend. It wraps the
// same Engine library the CLI uses; UI Phase 1 mounts a read-only browser
// UI over the SQLite inventory and HTTP Phase 2 adds mutating endpoints +
// SSE for browser-triggered deployments with live log streaming.
package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/klehmer/nimbusfab/internal/cloud/aws"
	"github.com/klehmer/nimbusfab/internal/cloud/azure"
	"github.com/klehmer/nimbusfab/internal/cloud/gcp"
	// Backend imports are for side-effect init() registration with
	// inventory.Open's dispatcher. Adding a new backend requires only
	// adding an import line here.
	_ "github.com/klehmer/nimbusfab/internal/inventory/postgres"
	_ "github.com/klehmer/nimbusfab/internal/inventory/sqlite"
	"github.com/klehmer/nimbusfab/internal/tofu"
	"github.com/klehmer/nimbusfab/internal/webapi"
	"github.com/klehmer/nimbusfab/internal/webapi/middleware"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/drift/notify"
	"github.com/klehmer/nimbusfab/pkg/drift/scheduler"
	"github.com/klehmer/nimbusfab/pkg/engine"
	"github.com/klehmer/nimbusfab/pkg/inventory"
	"github.com/klehmer/nimbusfab/pkg/secrets"
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
	workRoot := envDefault("NIMBUSFAB_WORK_ROOT", filepath.Join(os.TempDir(), "nimbusfab-server"))
	authMode := middleware.AuthMode(envDefault("NIMBUSFAB_AUTH_MODE", "disabled"))
	sessionKey, err := resolveSessionKey(authMode)
	if err != nil {
		return fmt.Errorf("session key: %w", err)
	}

	repo, err := openRepo(ctx, dsn)
	if err != nil {
		return fmt.Errorf("open repo (%s): %w", dsn, err)
	}

	reg, err := defaultCloudRegistry()
	if err != nil {
		return fmt.Errorf("cloud registry: %w", err)
	}

	eng, err := engine.New(ctx, engine.Config{
		CloudAdapters:  reg,
		InventoryRepo:  repo,
		SecretsBackend: secrets.DefaultBackend(),
		TofuRunner:     tofu.NewExecRunner(),
		WorkRoot:       workRoot,
	})
	if err != nil {
		return fmt.Errorf("engine: %w", err)
	}

	notif := notify.FromEnv()
	if interval := parseDriftDuration(os.Getenv("NIMBUSFAB_DRIFT_INTERVAL")); interval > 0 {
		sched := scheduler.New(scheduler.Config{
			OrgID:          orgID,
			GlobalInterval: interval,
			MaxConcurrent:  4,
		}, repo, eng, notif)
		go sched.Run(ctx)
		fmt.Printf("drift scheduler active (interval=%s, notifier configured: %t)\n",
			interval, hasNotifier(notif))
	}

	handler, err := webapi.New(webapi.Config{
		Repo:       repo,
		OrgID:      orgID,
		APIToken:   apiToken,
		Engine:     eng,
		AuthMode:   authMode,
		SessionKey: sessionKey,
	})
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	authNote := "auth: " + string(authMode)
	if authMode == middleware.AuthModeDisabled {
		authNote += " — DEV ONLY; do NOT expose this port publicly"
	}
	errCh := make(chan error, 1)
	go func() {
		fmt.Printf("nimbusfab-server listening on %s (Auth Phase 1; org=%s; %s)\n", addr, orgID, authNote)
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

// openRepo dispatches by DSN scheme via inventory.Open. Both sqlite and
// postgres backends register via init(); adding a new backend is one
// import line at the top of this file.
func openRepo(ctx context.Context, dsn string) (inventory.Repo, error) {
	return inventory.Open(ctx, dsn)
}

// defaultCloudRegistry mirrors the CLI's helper: register every in-tree
// adapter (AWS, Azure, GCP). One edit per new cloud.
func defaultCloudRegistry() (cloud.Registry, error) {
	reg := cloud.NewRegistry()
	if err := reg.Register(aws.New()); err != nil {
		return nil, err
	}
	if err := reg.Register(azure.New()); err != nil {
		return nil, err
	}
	if err := reg.Register(gcp.New()); err != nil {
		return nil, err
	}
	return reg, nil
}

func envDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseDriftDuration(s string) time.Duration {
	if s == "" {
		return 0
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0
	}
	return d
}

func hasNotifier(n notify.Notifier) bool {
	_, isNop := n.(notify.NopNotifier)
	return !isNop
}

// resolveSessionKey returns the cookie-signing key. In disabled mode the
// returned key is irrelevant (middleware bypasses signature checks) but
// must still be ≥16 bytes to satisfy the sign-time guard. For local mode
// the key is read from NIMBUSFAB_SESSION_KEY (raw bytes); if unset, a
// random key is generated with a WARN log — only acceptable for
// single-instance dev (sessions invalidate on restart and across
// replicas).
func resolveSessionKey(mode middleware.AuthMode) ([]byte, error) {
	if v := os.Getenv("NIMBUSFAB_SESSION_KEY"); v != "" {
		if len(v) < 16 {
			return nil, fmt.Errorf("NIMBUSFAB_SESSION_KEY must be ≥16 bytes (got %d)", len(v))
		}
		return []byte(v), nil
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if mode == middleware.AuthModeLocal {
		fmt.Fprintln(os.Stderr, "WARN: NIMBUSFAB_SESSION_KEY unset; generated a random session key. Sessions will not survive a restart or work across replicas. Set NIMBUSFAB_SESSION_KEY to a stable secret in production.")
	}
	return key, nil
}
