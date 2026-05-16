package components_test

import (
	"context"
	"testing"

	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/components"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

type fakeType struct {
	name string
}

func (f fakeType) Name() string                       { return f.name }
func (f fakeType) SpecSchema() []byte                 { return []byte(`{}`) }
func (f fakeType) SupportedClouds() []string          { return []string{"aws"} }
func (f fakeType) Outputs() map[string]components.OutputType {
	return map[string]components.OutputType{}
}
func (f fakeType) Emit(ctx context.Context, target ir.DeploymentTarget, adapter cloud.Adapter, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
	return nil, nil
}

func TestRegistry_RegisterAndLookup(t *testing.T) {
	r := components.NewInMemoryRegistry()
	if err := r.Register(fakeType{name: "network"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	tp, ok := r.Type("network")
	if !ok || tp.Name() != "network" {
		t.Errorf("Type(network): ok=%v tp=%v", ok, tp)
	}
	if names := r.Types(); len(names) != 1 || names[0] != "network" {
		t.Errorf("Types(): %v", names)
	}
}

func TestRegistry_DuplicateRegister(t *testing.T) {
	r := components.NewInMemoryRegistry()
	_ = r.Register(fakeType{name: "x"})
	if err := r.Register(fakeType{name: "x"}); err == nil {
		t.Error("duplicate Register: nil err, want non-nil")
	}
}
