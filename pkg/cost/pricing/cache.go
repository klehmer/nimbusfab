// Package pricing defines the pricing-cache interface used by the cost
// estimator. The concrete implementation in internal/pricing/cache wraps a
// read-through cache over cloud pricing APIs with a bundled-snapshot fallback
// per release.
package pricing

import (
	"context"
	"time"
)

// Cache reads from in-memory, on-disk snapshot, or live cloud APIs (in that
// order) and surfaces which source the answer came from.
type Cache interface {
	Lookup(ctx context.Context, cloudName string, key map[string]any) (Entry, error)
	// Refresh forces a live fetch for the keys, bypassing the cache. Useful
	// for `nimbusfab cost estimate --refresh-prices`.
	Refresh(ctx context.Context, cloudName string, keys []map[string]any) error
}

// Entry is the per-key cache result.
type Entry struct {
	UnitPrice     float64
	UnitOfMeasure string
	Currency      string
	Source        string // "memory" | "snapshot" | "live"
	FetchedAt     time.Time
}
