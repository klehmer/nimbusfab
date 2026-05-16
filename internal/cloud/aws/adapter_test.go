package aws_test

import (
	"context"
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/aws"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestAdapter_NameAndSupport(t *testing.T) {
	a := aws.New()
	if a.Name() != "aws" {
		t.Errorf("Name() = %q, want \"aws\"", a.Name())
	}
	if got := a.SupportedAPIVersions(); len(got) != 1 || got[0] != ir.APIVersionV1Alpha1 {
		t.Errorf("SupportedAPIVersions() = %v, want [%s]", got, ir.APIVersionV1Alpha1)
	}
	types := a.SupportedComponentTypes()
	want := map[string]bool{"network": true, "compute": true, "database": true, "storage": true}
	for _, n := range types {
		delete(want, n)
	}
	if len(want) != 0 {
		t.Errorf("SupportedComponentTypes() missing: %v", want)
	}
}

func TestAdapter_DefaultStateBackend(t *testing.T) {
	a := aws.New()
	sb, err := a.DefaultStateBackend(context.Background(), ir.DeploymentTarget{Region: "us-east-1"})
	if err != nil {
		t.Fatalf("DefaultStateBackend: %v", err)
	}
	if sb.Kind != "s3" {
		t.Errorf("default backend kind = %q, want \"s3\"", sb.Kind)
	}
	if region, _ := sb.Config["region"].(string); region != "us-east-1" {
		t.Errorf("backend region = %v", region)
	}
}

func TestAdapter_ProviderBlock(t *testing.T) {
	a := aws.New()
	pb, err := a.ProviderBlock(context.Background(), ir.DeploymentTarget{Region: "us-east-1"}, cloud.Credentials{Ref: "aws-dev"})
	if err != nil {
		t.Fatalf("ProviderBlock: %v", err)
	}
	awsBlk, ok := pb["aws"].(map[string]any)
	if !ok {
		t.Fatalf("ProviderBlock missing aws key: %v", pb)
	}
	if awsBlk["region"] != "us-east-1" {
		t.Errorf("region = %v, want us-east-1", awsBlk["region"])
	}
	if _, hasKey := awsBlk["access_key"]; hasKey {
		t.Error("provider block leaks access_key")
	}
	if _, hasSec := awsBlk["secret_key"]; hasSec {
		t.Error("provider block leaks secret_key")
	}
}

func TestAdapter_Validate_RejectsMissingRegion(t *testing.T) {
	a := aws.New()
	issues := a.Validate(context.Background(), ir.DeploymentTarget{})
	if len(issues) == 0 {
		t.Error("Validate(): no issues for missing region")
	}
	if issues[0].Code != "ErrAdapterAWSRegionMissing" {
		t.Errorf("issue Code = %q, want ErrAdapterAWSRegionMissing", issues[0].Code)
	}
}
