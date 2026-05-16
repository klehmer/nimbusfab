package aws_test

import (
	"context"
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/aws"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestAdapter_Emit_UnsupportedType(t *testing.T) {
	a := aws.New()
	target := ir.DeploymentTarget{
		Cloud: "aws", Region: "us-east-1",
		Spec: map[string]any{"__type": "exotic"},
	}
	_, err := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
	if err == nil {
		t.Error("Emit with unsupported type: nil err, want non-nil")
	}
}

func TestAdapter_SupportsAllFourTypes(t *testing.T) {
	a := aws.New()
	got := a.SupportedComponentTypes()
	want := map[string]bool{"network": true, "compute": true, "database": true, "storage": true}
	for _, name := range got {
		delete(want, name)
	}
	if len(want) != 0 {
		t.Errorf("missing types: %v", want)
	}
}
