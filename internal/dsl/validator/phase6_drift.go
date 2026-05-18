package validator

import (
	"fmt"
	"time"

	"github.com/klehmer/nimbusfab/pkg/ir"
)

const driftMinimumInterval = 60 * time.Second

func phase6Drift(proj *ir.Project, rep *ir.ValidationReport) error {
	for name, stack := range proj.Stacks {
		if stack.Drift == nil || stack.Drift.Interval == "" {
			continue
		}
		d, err := time.ParseDuration(stack.Drift.Interval)
		if err != nil {
			rep.Issues = append(rep.Issues, ir.Issue{
				Code:    "ErrValidatorDriftIntervalInvalid",
				Message: fmt.Sprintf("stack %q drift.interval %q does not parse as a duration: %v", name, stack.Drift.Interval, err),
			})
			continue
		}
		if d < driftMinimumInterval {
			rep.Issues = append(rep.Issues, ir.Issue{
				Code:    "ErrValidatorDriftIntervalTooShort",
				Message: fmt.Sprintf("stack %q drift.interval %q is below the minimum 60s", name, stack.Drift.Interval),
			})
		}
	}
	return nil
}
