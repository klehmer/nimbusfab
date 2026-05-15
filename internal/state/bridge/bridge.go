// Package bridge reconciles OpenTofu state (read via `tofu show -json`) with
// the inventory. Used by the engine's drift-detection path and by Import.
package bridge

import (
	"context"

	"github.com/kratus8990/nimbusfab/pkg/engine"
)

// Bridge is the entry point.
type Bridge interface {
	// DetectDrift reads current tofu state for a deployment and compares each
	// primitive to the inventory's last-known IR snapshot.
	DetectDrift(ctx context.Context, deploymentID string) (*engine.DriftReport, error)
}
