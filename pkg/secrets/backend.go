// Package secrets defines the pluggable secrets backend interface. The engine
// never holds plaintext credentials in the inventory DB — it stores a name
// (e.g. "aws-prod") and resolves it through a Backend at runtime.
package secrets

import "context"

// Backend resolves named references to secret material.
type Backend interface {
	// Kind returns a stable short name ("env" | "file" | "vault" | "kms" |
	// "k8s" | ...). Used in audit logs and the SecretsRef.BackendKind column.
	Kind() string

	// Resolve returns the raw secret payload for the given reference. The
	// payload structure is adapter-specific: an AWS credentials ref usually
	// returns access-key / secret-key / session-token; an OIDC client ref
	// returns id+secret; a Vault path returns whatever the secret contains.
	Resolve(ctx context.Context, ref string) (map[string]any, error)
}

// Chain composes multiple backends in order; the first one that resolves a
// ref wins. Useful for "try env, then file, then Vault".
type Chain struct {
	backends []Backend
}

// NewChain constructs a chain that tries each backend in order.
func NewChain(backends ...Backend) *Chain {
	return &Chain{backends: backends}
}

// Kind returns "chain".
func (c *Chain) Kind() string { return "chain" }

// Resolve returns the first non-error, non-empty result, or the last error.
func (c *Chain) Resolve(ctx context.Context, ref string) (map[string]any, error) {
	var lastErr error
	for _, b := range c.backends {
		v, err := b.Resolve(ctx, ref)
		if err == nil && v != nil {
			return v, nil
		}
		if err != nil {
			lastErr = err
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, ErrNotFound{Ref: ref}
}

// ErrNotFound is returned when no backend in a chain knows the ref.
type ErrNotFound struct {
	Ref string
}

func (e ErrNotFound) Error() string { return "secrets: ref not found: " + e.Ref }
