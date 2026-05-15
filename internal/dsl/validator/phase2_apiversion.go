package validator

import (
	"github.com/klehmer/nimbusfab/pkg/ir"
)

// supportedAPIVersions lists the IR APIVersions the validator accepts. New
// versions get added here as they're introduced; old versions stay in the
// list for at least one minor release after deprecation.
var supportedAPIVersions = map[string]bool{
	ir.APIVersionV1Alpha1: true,
}

func phase2APIVersionImpl(proj *ir.Project, report *ir.ValidationReport) {
	if proj.APIVersion == "" {
		report.Issues = append(report.Issues, ir.Issue{
			Severity: ir.SeverityError,
			Code:     "ErrMissingAPIVersion",
			Message:  "project.yaml does not declare apiVersion",
			Path:     "apiVersion",
			Hint:     `add: apiVersion: ` + ir.APIVersionV1Alpha1,
		})
		return
	}
	if !supportedAPIVersions[proj.APIVersion] {
		report.Issues = append(report.Issues, ir.Issue{
			Severity: ir.SeverityError,
			Code:     "ErrUnknownAPIVersion",
			Message:  "apiVersion " + proj.APIVersion + " is not supported by this build",
			Path:     "apiVersion",
			Hint:     "supported: " + ir.APIVersionV1Alpha1,
		})
	}
}
