// Package cloud defines the per-cloud Adapter interface. Adapter is THE
// plugin contract — every in-tree and (future) out-of-tree provider
// implements it. The set of methods is also the protobuf service that the
// v2 gRPC plugin protocol will surface, so changes here are public API
// changes.
package cloud

import (
	"context"
	"time"

	"github.com/kratus8990/cloud-infra-manager/pkg/ir"
)

// Adapter is what every cloud provider implements. The engine never
// inspects an adapter's internals; all communication is through this
// interface plus structured error categories defined in package errs.
type Adapter interface {
	// Name returns the cloud short name ("aws" / "azure" / "gcp"). Must be
	// stable across versions; used as the dispatch key.
	Name() string

	// SupportedAPIVersions returns the IR APIVersions this adapter can
	// consume. The engine fails fast if a project's APIVersion is unsupported.
	SupportedAPIVersions() []string

	// Emit takes one DeploymentTarget (already merged with stack vars and any
	// composition expansion) and returns the ResourcePrimitives to write into
	// the OpenTofu workspace. Emit is pure: no network, no disk, no globals.
	Emit(ctx context.Context, target ir.DeploymentTarget, refs ResolvedRefs) ([]ir.ResourcePrimitive, error)

	// PricingKey returns the cloud-native pricing identifier for one
	// primitive. The cost estimator hands this back to PricingProvider /
	// the pricing-cache. Opaque to the engine.
	PricingKey(ctx context.Context, primitive ir.ResourcePrimitive) (map[string]any, error)

	// BillingQuery returns the parameters needed to fetch actual cost rows
	// for a credential over a time window. The cost collector calls this and
	// then dispatches to the cloud's billing API.
	BillingQuery(ctx context.Context, creds Credentials, since, until time.Time) (BillingQueryParams, error)

	// FetchBilling executes a billing query and returns normalized rows.
	// Separated from BillingQuery so the collector can mock the API call
	// while still exercising the query construction in tests.
	FetchBilling(ctx context.Context, creds Credentials, params BillingQueryParams) ([]NormalizedCostRow, error)

	// DefaultStateBackend returns the recommended state backend config for
	// this cloud when a user has not declared one. Allows zero-config CLI
	// use against a credentials-bearing local profile.
	DefaultStateBackend(ctx context.Context, target ir.DeploymentTarget) (ir.StateBackend, error)
}

// Credentials is an opaque handle that adapters resolve via the secrets
// backend. The engine fetches it by name and passes the result through.
type Credentials struct {
	Ref     string         // the user-facing name (e.g., "aws-prod")
	Payload map[string]any // adapter-specific resolved secret material
}

// ResolvedRefs is the set of cross-component output references the engine
// has already looked up by the time it calls Emit. Keyed by ComponentRef.As
// (or .Output if .As is empty).
type ResolvedRefs map[string]any

// BillingQueryParams is whatever the adapter needs to call the billing API.
// Opaque to everything except the adapter itself.
type BillingQueryParams map[string]any

// NormalizedCostRow is the canonical shape every adapter normalizes its
// billing response into. Resource IDs match the cloud-native ARNs / IDs that
// the provisioner stored alongside primitives.
type NormalizedCostRow struct {
	PeriodStart time.Time
	PeriodEnd   time.Time
	Service     string
	ResourceID  string
	Region      string
	Amount      float64
	Currency    string
	Tags        map[string]string
}
