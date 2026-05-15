// Package collector polls cloud billing APIs and writes normalized rows to
// the cost_actuals table. Runs as a goroutine in server / daemon mode and as
// an on-demand call in the CLI (`mytool cost actual --refresh`).
package collector

import (
	"context"
	"time"

	"github.com/kratus8990/cloud-infra-manager/pkg/cloud"
)

// Collector exposes the two operations the engine needs.
type Collector interface {
	// PollOnce fetches actuals for the given window across all configured
	// adapters and upserts them into the inventory.
	PollOnce(ctx context.Context, in PollInput) (PollResult, error)

	// Run is the long-running loop used in server / daemon mode. It schedules
	// PollOnce on the configured interval until ctx is canceled.
	Run(ctx context.Context, schedule Schedule) error
}

// PollInput selects what to collect for a single pass.
type PollInput struct {
	OrgID    string
	Adapters map[string]cloud.Adapter   // keyed by cloud name
	Creds    map[string]cloud.Credentials // keyed by credential ref name
	Since    time.Time
	Until    time.Time
}

// PollResult summarizes a single poll pass.
type PollResult struct {
	Rows     int
	Warnings []string
}

// Schedule configures the Run loop.
type Schedule struct {
	Interval        time.Duration // default 6h for in-scope; 24h for full sweep
	JitterFraction  float64       // 0..1; defaults to 0.1
	InitialDelay    time.Duration
}
