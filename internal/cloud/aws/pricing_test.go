package aws_test

import (
	"context"
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/aws"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestPricingKey_EC2Instance(t *testing.T) {
	a := aws.New()
	target := ir.DeploymentTarget{
		Cloud: "aws", Region: "us-east-1",
		Spec: map[string]any{"__type": "compute", "__component": "web", "size": "small"},
	}
	prims, _ := a.Emit(context.Background(), target, cloud.ResolvedRefs{"subnetId": "s-1"})
	for _, p := range prims {
		if p.TofuType == "aws_instance" {
			key, err := a.PricingKey(context.Background(), p)
			if err != nil {
				t.Fatalf("PricingKey: %v", err)
			}
			if key["service"] != "AmazonEC2" {
				t.Errorf("service = %v", key["service"])
			}
			if key["instanceType"] != "t3.small" {
				t.Errorf("instanceType = %v", key["instanceType"])
			}
			if key["region"] != "us-east-1" {
				t.Errorf("region = %v", key["region"])
			}
		}
	}
}

func TestPricingKey_RDSInstance(t *testing.T) {
	a := aws.New()
	target := ir.DeploymentTarget{
		Cloud: "aws", Region: "us-east-1",
		Spec: map[string]any{"__type": "database", "__component": "db", "engine": "postgres", "size": "medium"},
	}
	prims, _ := a.Emit(context.Background(), target, cloud.ResolvedRefs{"subnetIds": []string{"s-1"}})
	for _, p := range prims {
		if p.TofuType == "aws_db_instance" {
			key, _ := a.PricingKey(context.Background(), p)
			if key["service"] != "AmazonRDS" {
				t.Errorf("service = %v", key["service"])
			}
			if key["engineCode"] != "postgres" {
				t.Errorf("engineCode = %v", key["engineCode"])
			}
			if key["deploymentOption"] != "Single-AZ" {
				t.Errorf("deploymentOption = %v", key["deploymentOption"])
			}
		}
	}
}

func TestPricingKey_FreePrimitive(t *testing.T) {
	a := aws.New()
	key, err := a.PricingKey(context.Background(), ir.ResourcePrimitive{
		TofuType: "aws_vpc", ID: "x.aws-us-east-1.vpc",
	})
	if err != nil {
		t.Fatalf("PricingKey: %v", err)
	}
	if key != nil {
		t.Errorf("VPC should return nil pricing key, got %v", key)
	}
}
