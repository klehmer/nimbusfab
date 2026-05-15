// Package validator runs JSON-Schema and semantic checks against a loaded
// ir.Project. It also drives composition expansion (compositions are expanded
// at validation time, snapshot-per-deployment).
package validator

import (
	"context"
	"errors"

	"github.com/klehmer/nimbusfab/pkg/engine"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

// Validator turns an unvalidated *ir.Project into a ValidationReport. On
// success, the input project is mutated to reflect composition expansion
// (every Component.Type now refers to a built-in type the registry knows).
type Validator interface {
	Validate(ctx context.Context, project *ir.Project) (*engine.ValidationReport, error)
}

// New constructs the default Validator. Implementation deferred to the
// DSL & IR subsystem spec.
func New() Validator { return &stub{} }

type stub struct{}

func (s *stub) Validate(_ context.Context, _ *ir.Project) (*engine.ValidationReport, error) {
	return nil, errors.New("validator.Validate: not implemented yet")
}
