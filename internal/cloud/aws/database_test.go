package aws_test

import (
	"context"
	"strings"
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/aws"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestEmitDatabase_BasicShape(t *testing.T) {
	a := aws.New()
	target := ir.DeploymentTarget{
		Cloud:  "aws",
		Region: "us-east-1",
		Spec: map[string]any{
			"__type": "database", "__component": "orders-db",
			"engine": "postgres", "size": "small",
		},
	}
	refs := cloud.ResolvedRefs{
		"vpcId":     "vpc-0abc",
		"subnetIds": []string{"subnet-1", "subnet-2"},
	}
	prims, err := a.Emit(context.Background(), target, refs)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if len(prims) != 2 {
		t.Fatalf("got %d primitives, want 2", len(prims))
	}
	var sg, db *ir.ResourcePrimitive
	for i := range prims {
		switch prims[i].TofuType {
		case "aws_db_subnet_group":
			sg = &prims[i]
		case "aws_db_instance":
			db = &prims[i]
		}
	}
	if sg == nil || db == nil {
		t.Fatalf("missing primitives: %+v", prims)
	}
	if db.Attributes["instance_class"] != "db.t3.small" {
		t.Errorf("instance_class = %v", db.Attributes["instance_class"])
	}
	if db.Attributes["engine"] != "postgres" {
		t.Errorf("engine = %v", db.Attributes["engine"])
	}
	if db.Attributes["allocated_storage"] != 100 {
		t.Errorf("allocated_storage = %v, want 100", db.Attributes["allocated_storage"])
	}
}

func TestEmitDatabase_SizeMapping(t *testing.T) {
	cases := map[string]struct {
		instanceClass string
		storage       int
	}{
		"small":  {"db.t3.small", 100},
		"medium": {"db.t3.medium", 250},
		"large":  {"db.m6i.large", 500},
		"xlarge": {"db.m6i.xlarge", 1000},
	}
	a := aws.New()
	for size, want := range cases {
		prims, err := a.Emit(context.Background(), ir.DeploymentTarget{
			Cloud: "aws", Region: "us-east-1",
			Spec: map[string]any{"__type": "database", "__component": "db", "engine": "postgres", "size": size},
		}, cloud.ResolvedRefs{"subnetIds": []string{"subnet-1"}})
		if err != nil {
			t.Errorf("%s: Emit: %v", size, err)
			continue
		}
		for _, p := range prims {
			if p.TofuType == "aws_db_instance" {
				if p.Attributes["instance_class"] != want.instanceClass {
					t.Errorf("%s: instance_class = %v, want %v", size, p.Attributes["instance_class"], want.instanceClass)
				}
				if p.Attributes["allocated_storage"] != want.storage {
					t.Errorf("%s: storage = %v, want %v", size, p.Attributes["allocated_storage"], want.storage)
				}
			}
		}
	}
}

func TestEmitDatabase_UnknownEngine(t *testing.T) {
	a := aws.New()
	_, err := a.Emit(context.Background(), ir.DeploymentTarget{
		Cloud: "aws", Region: "us-east-1",
		Spec: map[string]any{"__type": "database", "__component": "db", "engine": "oracle", "size": "small"},
	}, cloud.ResolvedRefs{"subnetIds": []string{"s"}})
	if err == nil {
		t.Error("expected error for unsupported engine")
	}
}

func TestEmitDatabase_MissingSubnetIDsRefErrors(t *testing.T) {
	a := aws.New()
	target := ir.DeploymentTarget{
		Cloud: "aws", Region: "us-east-1",
		Spec: map[string]any{"__component": "orders-db", "__type": "database",
			"engine": "postgres", "size": "small"},
	}
	_, err := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
	if err == nil {
		t.Fatal("expected Emit to fail when subnetIds ref is missing")
	}
	if !strings.Contains(err.Error(), "subnetIds") && !strings.Contains(err.Error(), "ref") {
		t.Errorf("error should mention missing ref; got: %v", err)
	}
}
