package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/klehmer/nimbusfab/internal/inventory/sqlite"
	"github.com/klehmer/nimbusfab/pkg/inventory"
)

// openInventory returns a Repo per the flags: NullRepo if --no-inventory or
// dsn is empty; otherwise a SQLite Repo. Caller is responsible for Close().
func openInventory(ctx context.Context, dsn string, noInventory bool) (inventory.Repo, error) {
	if noInventory {
		return inventory.NewNullRepo(), nil
	}
	if dsn == "" {
		dsn = os.Getenv("NIMBUSFAB_INVENTORY_DSN")
	}
	if dsn == "" {
		home, _ := os.UserHomeDir()
		if home == "" {
			return inventory.NewNullRepo(), nil
		}
		dir := filepath.Join(home, ".config", "nimbusfab")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("inventory dir: %w", err)
		}
		dsn = "sqlite://" + filepath.Join(dir, "inventory.db")
	}
	repo, err := sqlite.Open(dsn)
	if err != nil {
		return nil, fmt.Errorf("inventory open: %w", err)
	}
	if err := repo.Migrate(ctx); err != nil {
		repo.Close()
		return nil, fmt.Errorf("inventory migrate: %w", err)
	}
	return repo, nil
}
