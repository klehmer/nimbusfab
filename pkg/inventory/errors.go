package inventory

import "errors"

// ErrInventoryRequired is returned by read paths when no inventory is
// configured. Engine surfaces wrap this into UserFacing errors.
var ErrInventoryRequired = errors.New("inventory: required for this operation but not configured")

// ErrInventoryUnavailable is returned when inventory IS configured but the
// underlying DB is unreachable. Distinguish from ErrInventoryRequired so the
// CLI can suggest different remediations.
var ErrInventoryUnavailable = errors.New("inventory: unavailable (check DSN / DB liveness)")

// ErrDeploymentNotFound is returned by Apply/Destroy/Drift when the given
// deployment ID doesn't exist for the requested org.
var ErrDeploymentNotFound = errors.New("inventory: deployment not found")

// ErrDeploymentWrongStatus is returned by Apply when the deployment is not in
// the expected lifecycle state (e.g., trying to Apply an already-applied one).
var ErrDeploymentWrongStatus = errors.New("inventory: deployment is not in the expected status")

// ErrMigrationConflict is returned by Migrate when applied versions don't
// match expectations (typically: a migration was deleted on disk).
var ErrMigrationConflict = errors.New("inventory: migration conflict")

// ErrNotImplementedYet is returned by sub-repo methods that exist in the
// interface but aren't wired in Phase 1.
var ErrNotImplementedYet = errors.New("inventory: not implemented yet")
