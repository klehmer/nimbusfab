// Package validator runs the IR validation phases. Phase 1 turns loader
// errors into Issues; Phases 2 and 3 land in subsequent tasks; later
// phases (interpolation, composition, semantics) are deferred to the
// Phase 2 implementation plan.
package validator

import (
	"context"

	"github.com/klehmer/nimbusfab/pkg/components"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

// Validator runs the configured phases and returns a ValidationReport.
type Validator interface {
	// ValidateLoaderError converts a single loader error into a Report.
	// Phase 1 of the validation pipeline lives here because loader failures
	// abort before later phases can see the IR.
	ValidateLoaderError(ctx context.Context, err error) (*ir.ValidationReport, error)

	// Validate runs the IR through Phases 2+. Phase 1 will already have
	// been satisfied if a non-nil Project reached this point.
	Validate(ctx context.Context, proj *ir.Project) (*ir.ValidationReport, error)
}

// New returns the default Validator. The registry is consumed by Phase 4
// (per-type spec schema). Production callers should pass
// components.DefaultRegistry(); tests can inject a minimal registry.
func New(registry components.Registry) Validator {
	return &fsValidator{registry: registry}
}

type fsValidator struct {
	registry components.Registry
}

// Validate runs the configured phases. Phase 1 plan covers phases 2 and 3;
// phases 4-9 land in the Phase 2 plan and currently return immediately.
func (v *fsValidator) Validate(ctx context.Context, proj *ir.Project) (*ir.ValidationReport, error) {
	report := &ir.ValidationReport{}
	if proj == nil {
		report.Issues = append(report.Issues, ir.Issue{
			Severity: ir.SeverityError,
			Code:     "ErrLoader",
			Message:  "nil project",
		})
		return report, nil
	}
	if err := phase2APIVersion(proj, report); err != nil {
		return nil, err
	}
	if err := phase3Schema(proj, report); err != nil {
		return nil, err
	}
	if err := phase4TypeSpec(proj, v.registry, report); err != nil {
		return nil, err
	}
	if err := phase5Refs(proj, v.registry, report); err != nil {
		return nil, err
	}
	if err := phase6Drift(proj, report); err != nil {
		return nil, err
	}
	_ = ctx
	return report, nil
}

func phase2APIVersion(proj *ir.Project, report *ir.ValidationReport) error {
	phase2APIVersionImpl(proj, report)
	return nil
}
func phase3Schema(proj *ir.Project, report *ir.ValidationReport) error {
	return phase3SchemaImpl(proj, report)
}
func phase4TypeSpec(proj *ir.Project, reg components.Registry, report *ir.ValidationReport) error {
	return phase4TypeSpecImpl(proj, reg, report)
}
func phase5Refs(proj *ir.Project, reg components.Registry, report *ir.ValidationReport) error {
	return phase5RefsImpl(proj, reg, report)
}
