package validator

import (
	"context"
	"errors"

	"github.com/klehmer/nimbusfab/internal/dsl/yamlnode"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func (v *fsValidator) ValidateLoaderError(ctx context.Context, err error) (*ir.ValidationReport, error) {
	_ = ctx
	if err == nil {
		return &ir.ValidationReport{}, nil
	}
	report := &ir.ValidationReport{}

	var ye *yamlnode.Error
	if errors.As(err, &ye) {
		report.Issues = append(report.Issues, ir.Issue{
			Severity: ir.SeverityError,
			Code:     "ErrYAMLMalformed",
			Message:  ye.Err.Error(),
			Source:   ye.Source,
		})
		return report, nil
	}

	// Anything else is captured as a generic loader error.
	report.Issues = append(report.Issues, ir.Issue{
		Severity: ir.SeverityError,
		Code:     "ErrLoader",
		Message:  err.Error(),
	})
	return report, nil
}
