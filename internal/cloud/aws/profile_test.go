package aws_test

import (
	"context"
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/aws"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestProfile_VPC(t *testing.T) {
	a := aws.New()
	prims, _ := a.Emit(context.Background(), ir.DeploymentTarget{
		Cloud: "aws", Region: "us-east-1",
		Spec: map[string]any{"__type": "network", "__component": "web", "cidr": "10.0.0.0/16"},
	}, cloud.ResolvedRefs{})
	for _, p := range prims {
		if p.TofuType == "aws_vpc" {
			prof, err := a.Profile(context.Background(), p)
			if err != nil {
				t.Fatalf("Profile: %v", err)
			}
			if prof.Class != "network" {
				t.Errorf("Class = %v", prof.Class)
			}
			if prof.Network == nil || prof.Network.CIDR != "10.0.0.0/16" {
				t.Errorf("Network: %+v", prof.Network)
			}
		}
	}
}

func TestProfile_DBInstance(t *testing.T) {
	a := aws.New()
	prims, _ := a.Emit(context.Background(), ir.DeploymentTarget{
		Cloud: "aws", Region: "us-east-1",
		Spec: map[string]any{"__type": "database", "__component": "db", "engine": "postgres", "size": "medium"},
	}, cloud.ResolvedRefs{"subnetIds": []string{"s-1"}})
	for _, p := range prims {
		if p.TofuType == "aws_db_instance" {
			prof, err := a.Profile(context.Background(), p)
			if err != nil {
				t.Fatalf("Profile: %v", err)
			}
			if prof.Class != "database" {
				t.Errorf("Class = %v", prof.Class)
			}
			if prof.Database == nil || prof.Database.Engine != "postgres" {
				t.Errorf("Database: %+v", prof.Database)
			}
			if prof.Database.Compute.VCPU != 2 || prof.Database.Compute.MemoryGB != 4 {
				t.Errorf("Compute: %+v", prof.Database.Compute)
			}
		}
	}
}

func TestProfile_Instance(t *testing.T) {
	a := aws.New()
	prims, _ := a.Emit(context.Background(), ir.DeploymentTarget{
		Cloud: "aws", Region: "us-east-1",
		Spec: map[string]any{"__type": "compute", "__component": "web", "size": "medium"},
	}, cloud.ResolvedRefs{"subnetId": "s-1"})
	for _, p := range prims {
		if p.TofuType == "aws_instance" {
			prof, _ := a.Profile(context.Background(), p)
			if prof.Class != "compute" || prof.Compute == nil {
				t.Errorf("compute profile missing: %+v", prof)
			}
			if prof.Compute.VCPU != 2 || prof.Compute.MemoryGB != 4 {
				t.Errorf("compute = %+v", prof.Compute)
			}
		}
	}
}

func TestProfile_FreePrimitives(t *testing.T) {
	a := aws.New()
	_, err := a.Profile(context.Background(), ir.ResourcePrimitive{TofuType: "aws_subnet"})
	if err == nil {
		t.Error("expected ErrProfileUnavailable for subnet")
	}
}
