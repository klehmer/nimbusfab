package ir

import "fmt"

// Source identifies the location in a YAML file that produced an IR node.
// The loader attaches Sources to nodes as it parses; the validator quotes
// them in diagnostics.
type Source struct {
	File   string
	Line   int
	Column int
}

// String renders the source as "file:line:col", "file:line", "file", or
// "<unknown>" depending on which fields are set.
func (s Source) String() string {
	switch {
	case s.File == "":
		return "<unknown>"
	case s.Line == 0:
		return s.File
	case s.Column == 0:
		return fmt.Sprintf("%s:%d", s.File, s.Line)
	default:
		return fmt.Sprintf("%s:%d:%d", s.File, s.Line, s.Column)
	}
}
