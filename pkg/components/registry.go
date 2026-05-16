// Package components holds the registry of component types. A component type
// is a logical name ("network", "compute", "database", "storage", ...) that
// users put in YAML `type:`. The registry dispatches each type to the right
// adapter Emit() call; type-specific spec validation rules also live here.
package components

import (
	"context"

	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

// Registry knows the set of recognized component types and, for each, how to
// validate its Spec and emit primitives via the relevant cloud adapter.
type Registry interface {
	// Types returns the names of all registered component types.
	Types() []string

	// Type returns the Type descriptor for the given name; ok is false if the
	// name is unknown.
	Type(name string) (Type, bool)

	// Register adds a Type to the registry. Returns an error if the name is
	// already registered.
	Register(t Type) error
}

// Type is one component-type descriptor. Implementations are usually small:
// they hold a JSON Schema for the spec and dispatch Emit to the adapter.
type Type interface {
	// Name is the type name as users write it in YAML.
	Name() string

	// SpecSchema returns the JSON Schema for this type's spec field. The
	// validator runs this before any adapter sees the spec.
	SpecSchema() []byte

	// SupportedClouds returns the clouds this type can deploy to.
	SupportedClouds() []string

	// Outputs declares what reference targets this type publishes. Consumed
	// by the validator's ref-resolution phase and by the web app's type
	// browser. Keys are output names (e.g. "vpc_id"); values describe the
	// expected shape.
	Outputs() map[string]OutputType

	// Emit dispatches to the right cloud adapter and returns primitives. The
	// registry passes through the adapter that was looked up by target.Cloud.
	Emit(ctx context.Context, target ir.DeploymentTarget, adapter cloud.Adapter, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error)
}

// OutputType describes one declared output of a component type. The Kind is
// one of: "string", "integer", "boolean", "list<string>", "map<string,string>".
type OutputType struct {
	Kind        string
	Description string
}

// NewInMemoryRegistry returns an empty Registry suitable for testing and for
// composing in-tree types at startup. Returned implementation is not exported
// since callers depend only on the interface.
func NewInMemoryRegistry() Registry {
	return &memReg{types: map[string]Type{}}
}

type memReg struct {
	types map[string]Type
}

func (r *memReg) Types() []string {
	out := make([]string, 0, len(r.types))
	for name := range r.types {
		out = append(out, name)
	}
	return out
}

func (r *memReg) Type(name string) (Type, bool) {
	t, ok := r.types[name]
	return t, ok
}

func (r *memReg) Register(t Type) error {
	if _, exists := r.types[t.Name()]; exists {
		return errDuplicateType(t.Name())
	}
	r.types[t.Name()] = t
	return nil
}

type errDuplicateType string

func (e errDuplicateType) Error() string {
	return "components: type already registered: " + string(e)
}
