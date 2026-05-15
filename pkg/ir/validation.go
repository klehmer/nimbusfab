package ir

import "fmt"

// Severity classifies a diagnostic.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// Issue is a single diagnostic produced by the validator.
type Issue struct {
	Severity Severity
	Code     string // stable code, e.g. "ErrSchemaRequiredField"
	Message  string // human-readable
	Path     string // dotted IR path, e.g. "components[0].targets[1].region"
	Source   Source // YAML provenance when available
	Hint     string // remediation suggestion
}

// String renders the issue in the canonical "<source>: <severity>: <code>: <message> (at <path>)" form.
func (i Issue) String() string {
	pathPart := ""
	if i.Path != "" {
		pathPart = fmt.Sprintf(" (at %s)", i.Path)
	}
	return fmt.Sprintf("%s: %s: %s: %s%s", i.Source.String(), i.Severity, i.Code, i.Message, pathPart)
}

// ValidationReport is what the validator returns. OK() reports whether any
// blocking errors were found; warnings and info do not block.
type ValidationReport struct {
	Issues []Issue
}

// OK returns true iff no Issue with severity Error is present.
func (r ValidationReport) OK() bool {
	for _, i := range r.Issues {
		if i.Severity == SeverityError {
			return false
		}
	}
	return true
}
