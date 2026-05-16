package inventory

import (
	"context"
	"fmt"
	"strings"
)

// Opener is the constructor signature each backend exposes. Backend packages
// register themselves via init() so the dispatcher avoids importing them
// directly (would create an import cycle).
type Opener func(ctx context.Context, dsn string) (Repo, error)

var openers = map[string]Opener{}

// RegisterBackend wires a scheme prefix ("sqlite", "postgres") to its
// constructor. Backend packages call this from init().
func RegisterBackend(scheme string, fn Opener) {
	openers[scheme] = fn
}

// Open opens a Repo using the backend matching the DSN scheme prefix.
// Recognizes "sqlite:", "postgres:", "postgresql:" (the latter normalized
// to "postgres").
func Open(ctx context.Context, dsn string) (Repo, error) {
	scheme := schemeOf(dsn)
	fn, ok := openers[scheme]
	if !ok {
		return nil, fmt.Errorf("inventory: no backend registered for scheme %q (dsn=%s); import a backend package for its side-effect init", scheme, dsn)
	}
	return fn(ctx, dsn)
}

// schemeOf returns the lowercase scheme portion of a DSN. PostgreSQL's
// alternate "postgresql://" form normalizes to "postgres".
func schemeOf(dsn string) string {
	i := strings.Index(dsn, ":")
	if i <= 0 {
		return ""
	}
	s := strings.ToLower(dsn[:i])
	if s == "postgresql" {
		return "postgres"
	}
	return s
}
