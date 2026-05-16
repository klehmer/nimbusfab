// Package pricing defines the pricing-cache interface used by the cost
// estimator. The Phase-1 concrete implementation here reads from embedded
// snapshot JSON files; live AWS Pricing API integration arrives in Phase 2.
package pricing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/klehmer/nimbusfab/pkg/cost/estimator"
)

// Cache reads from in-memory, on-disk snapshot, or live cloud APIs (in that
// order) and surfaces which source the answer came from.
type Cache interface {
	Lookup(ctx context.Context, cloudName string, key map[string]any) (Entry, error)
	// Refresh forces a live fetch for the keys, bypassing the cache. Phase 1
	// returns ErrNotImplementedYet; live fetching lands in Phase 2.
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

// ErrPricingMissing is returned by Lookup when no snapshot entry matches.
var ErrPricingMissing = errors.New("pricing: no snapshot entry for key")

// ErrNotImplementedYet is returned by Refresh in Phase 1.
var ErrNotImplementedYet = errors.New("pricing: not implemented yet (live fetch)")

// NewCache returns a Cache backed by the embedded snapshots. Panics on
// malformed snapshot data (build-time issue, not a runtime error).
func NewCache() Cache {
	snaps, err := LoadSnapshots()
	if err != nil {
		panic(fmt.Sprintf("pricing.NewCache: %v", err))
	}
	c := &snapshotCache{byCloud: map[string]*snapshotIndex{}}
	for cloudName, s := range snaps {
		idx := &snapshotIndex{
			Cloud:     cloudName,
			Currency:  s.Currency,
			FetchedAt: s.FetchedAt,
			Entries:   map[string]Entry{},
		}
		for _, e := range s.Entries {
			ck := CanonicalKey(e.Key)
			idx.Entries[ck] = Entry{
				UnitPrice:     e.UnitPrice,
				UnitOfMeasure: e.UnitOfMeasure,
				Currency:      s.Currency,
				Source:        "snapshot",
				FetchedAt:     s.FetchedAt,
			}
		}
		c.byCloud[cloudName] = idx
	}
	return c
}

type snapshotCache struct {
	byCloud map[string]*snapshotIndex
}

type snapshotIndex struct {
	Cloud     string
	Currency  string
	FetchedAt time.Time
	Entries   map[string]Entry
}

func (c *snapshotCache) Lookup(ctx context.Context, cloudName string, key map[string]any) (Entry, error) {
	idx, ok := c.byCloud[cloudName]
	if !ok {
		return Entry{}, fmt.Errorf("%w: no snapshot for cloud %q", ErrPricingMissing, cloudName)
	}
	ck := CanonicalKey(key)
	entry, ok := idx.Entries[ck]
	if !ok {
		return Entry{}, fmt.Errorf("%w: %s/%s", ErrPricingMissing, cloudName, ck)
	}
	return entry, nil
}

func (c *snapshotCache) Refresh(ctx context.Context, cloudName string, keys []map[string]any) error {
	return ErrNotImplementedYet
}

// AsPricingProvider adapts a Cache to the estimator's PricingProvider interface.
func AsPricingProvider(c Cache) estimator.PricingProvider {
	return &pricingProviderAdapter{cache: c}
}

type pricingProviderAdapter struct {
	cache Cache
}

func (p *pricingProviderAdapter) Price(ctx context.Context, cloudName string, key map[string]any) (estimator.UnitPrice, error) {
	entry, err := p.cache.Lookup(ctx, cloudName, key)
	if err != nil {
		return estimator.UnitPrice{}, err
	}
	return estimator.UnitPrice{
		UnitPrice:     entry.UnitPrice,
		UnitOfMeasure: entry.UnitOfMeasure,
		Currency:      entry.Currency,
		Source:        entry.Source,
	}, nil
}

// SnapshotAge returns the staleness of the bundled snapshot for cloud.
// Returns 0 if no snapshot exists for that cloud.
func SnapshotAge(c Cache, cloudName string) time.Duration {
	sc, ok := c.(*snapshotCache)
	if !ok {
		return 0
	}
	idx, ok := sc.byCloud[cloudName]
	if !ok {
		return 0
	}
	return time.Since(idx.FetchedAt)
}
