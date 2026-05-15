// Package composition expands user-defined Compositions into built-in
// components at validation time. Expansion is snapshot-per-deployment: edits
// to a Composition do not live-update existing deployments — users must
// re-plan.
package composition

import (
	"context"

	"github.com/klehmer/nimbusfab/pkg/ir"
)

// Engine expands user-defined Compositions referenced by Components.
type Engine interface {
	// Expand walks project.Components and replaces every Component whose Type
	// matches a registered Composition Kind with the expanded resources from
	// that Composition's template, parameterized by the consuming Component's
	// Spec. After Expand returns, no Component references a Composition Kind.
	Expand(ctx context.Context, project *ir.Project) error

	// Register adds a Composition to the engine's known set.
	Register(c ir.Composition) error
}

// New returns an Engine suitable for use during validation. Implementation
// deferred to the DSL & IR subsystem spec.
func New() Engine {
	return &stubEngine{}
}

type stubEngine struct{}

func (s *stubEngine) Expand(_ context.Context, _ *ir.Project) error {
	return errStub("composition.Expand: not implemented yet")
}

func (s *stubEngine) Register(_ ir.Composition) error {
	return errStub("composition.Register: not implemented yet")
}

type errStub string

func (e errStub) Error() string { return string(e) }
