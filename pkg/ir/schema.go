package ir

// GenerateJSONSchema returns the JSON Schema document describing valid Project
// YAML for the current IR APIVersion. The schema is derived from the Go types
// in this package; the YAML loader, the runtime validator, and the IDE schema
// distributed with releases all consume the same generated artifact, so they
// cannot drift.
//
// Implementation is deferred to the DSL & IR subsystem spec; this stub fixes
// the public signature.
func GenerateJSONSchema() ([]byte, error) {
	return nil, errUnimplemented
}

// errUnimplemented is intentionally unexported and shared across stub
// functions in this package. Subsystem implementations replace each stub
// independently.
var errUnimplemented = stubError("not implemented yet")

type stubError string

func (e stubError) Error() string { return string(e) }
