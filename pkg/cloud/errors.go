package cloud

import "errors"

// ErrProfileUnavailable is returned by Adapter.Profile when the adapter
// cannot construct a ResourceProfile for a given primitive (e.g., an
// IAM role that has no compute/storage/database/network shape). The
// parity engine treats this as a non-fatal warning, not an error.
var ErrProfileUnavailable = errors.New("cloud: profile unavailable for this primitive")

// ErrNotImplementedYet is returned by adapter methods stubbed during
// phased rollout. Phase 1 returns this from real adapters' Profile,
// PricingKey, BillingQuery, and FetchBilling methods.
var ErrNotImplementedYet = errors.New("cloud: not implemented yet")
