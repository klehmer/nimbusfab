package azure_test

import (
	"context"
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/azure"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestAdapter_NameAndSupport(t *testing.T) {
	a := azure.New()
	if a.Name() != "azure" {
		t.Errorf("Name = %q", a.Name())
	}
	types := a.SupportedComponentTypes()
	want := map[string]bool{"network": true, "compute": true, "database": true, "storage": true}
	for _, n := range types {
		delete(want, n)
	}
	if len(want) != 0 {
		t.Errorf("SupportedComponentTypes missing: %v", want)
	}
}

func TestAdapter_ValidateRejectsAWSStyleRegion(t *testing.T) {
	a := azure.New()
	issues := a.Validate(context.Background(), ir.DeploymentTarget{Region: "us-east-1"})
	if len(issues) == 0 {
		t.Error("expected validation issue for AWS-style region")
	}
}

func TestAdapter_ValidateAcceptsAzureRegion(t *testing.T) {
	a := azure.New()
	issues := a.Validate(context.Background(), ir.DeploymentTarget{Region: "eastus"})
	if len(issues) != 0 {
		t.Errorf("unexpected issues for valid region: %v", issues)
	}
}

func TestAdapter_DefaultStateBackend(t *testing.T) {
	a := azure.New()
	sb, err := a.DefaultStateBackend(context.Background(), ir.DeploymentTarget{Region: "eastus"})
	if err != nil {
		t.Fatalf("DefaultStateBackend: %v", err)
	}
	if sb.Kind != "azurerm" {
		t.Errorf("Kind = %q, want azurerm", sb.Kind)
	}
}

func TestAdapter_ProviderBlock_NoPlaintextSecrets(t *testing.T) {
	a := azure.New()
	pb, _ := a.ProviderBlock(context.Background(), ir.DeploymentTarget{Region: "eastus"}, cloud.Credentials{})
	azBlock, ok := pb["azurerm"].(map[string]any)
	if !ok {
		t.Fatalf("missing azurerm block: %v", pb)
	}
	if _, ok := azBlock["features"]; !ok {
		t.Errorf("missing features block: %v", azBlock)
	}
	for _, forbidden := range []string{"client_secret", "subscription_id", "tenant_id"} {
		if _, present := azBlock[forbidden]; present {
			t.Errorf("provider block leaks %s", forbidden)
		}
	}
}

func TestAdapter_Emit_UnsupportedType(t *testing.T) {
	a := azure.New()
	_, err := a.Emit(context.Background(), ir.DeploymentTarget{
		Cloud: "azure", Region: "eastus",
		Spec: map[string]any{"__type": "exotic"},
	}, cloud.ResolvedRefs{})
	if err == nil {
		t.Error("expected error for unsupported type")
	}
}
