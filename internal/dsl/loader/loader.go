// Package loader reads YAML files from a Project directory and returns an
// unvalidated ir.Project tree. Validation happens in the sibling validator
// package; this one cares only about file discovery, merge order, includes,
// and YAML well-formedness.
package loader

import (
	"context"
	"errors"

	"github.com/klehmer/nimbusfab/pkg/ir"
)

// Loader is the file-system-aware front door.
type Loader interface {
	// Load reads everything under root and returns an unvalidated IR. Errors
	// here are YAML-shape or filesystem errors, not semantic violations.
	Load(ctx context.Context, root string) (*ir.Project, error)
}

// New returns the default Loader implementation. Implementation deferred to
// the DSL & IR subsystem spec.
func New() Loader { return &stub{} }

type stub struct{}

func (s *stub) Load(_ context.Context, _ string) (*ir.Project, error) {
	return nil, errors.New("loader.Load: not implemented yet")
}
