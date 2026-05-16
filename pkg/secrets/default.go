package secrets

// DefaultBackend returns the Phase-1 default backend chain. Tries the
// EnvBackend first (cheapest, isolates per-process credentials), then
// FileBackend rooted at ~/.nimbusfab/secrets/. First non-nil resolution
// wins; if neither knows the ref, the Chain returns ErrNotFound.
func DefaultBackend() Backend {
	return NewChain(&EnvBackend{}, NewFileBackend(""))
}
