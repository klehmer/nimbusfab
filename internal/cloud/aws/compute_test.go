package aws_test

import (
	"context"
	"strings"
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/aws"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestEmitCompute_BasicShape(t *testing.T) {
	a := aws.New()
	target := ir.DeploymentTarget{
		Cloud: "aws", Region: "us-east-1",
		Spec: map[string]any{
			"__type": "compute", "__component": "web",
			"size":     "medium",
			"replicas": 2,
		},
	}
	refs := cloud.ResolvedRefs{
		"vpcId":    "vpc-0abc",
		"subnetId": "subnet-1",
	}
	prims, err := a.Emit(context.Background(), target, refs)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	var sg int
	var instances int
	for _, p := range prims {
		switch p.TofuType {
		case "aws_security_group":
			sg++
		case "aws_instance":
			instances++
		}
	}
	if sg != 1 {
		t.Errorf("security groups = %d, want 1", sg)
	}
	if instances != 2 {
		t.Errorf("instances = %d, want 2 (replicas)", instances)
	}
}

func TestEmitCompute_SizeMapping(t *testing.T) {
	cases := map[string]string{
		"small":  "t3.small",
		"medium": "t3.medium",
		"large":  "t3.large",
		"xlarge": "t3.xlarge",
	}
	a := aws.New()
	for size, want := range cases {
		prims, _ := a.Emit(context.Background(), ir.DeploymentTarget{
			Cloud: "aws", Region: "us-east-1",
			Spec: map[string]any{"__type": "compute", "__component": "x", "size": size},
		}, cloud.ResolvedRefs{"subnetId": "subnet-1", "vpcId": "vpc-1"})
		for _, p := range prims {
			if p.TofuType == "aws_instance" {
				if p.Attributes["instance_type"] != want {
					t.Errorf("%s: instance_type = %v, want %v", size, p.Attributes["instance_type"], want)
				}
			}
		}
	}
}

func TestEmitCompute_AMIPickedPerRegion(t *testing.T) {
	a := aws.New()
	for _, region := range []string{"us-east-1", "us-west-2", "eu-west-1"} {
		prims, _ := a.Emit(context.Background(), ir.DeploymentTarget{
			Cloud: "aws", Region: region,
			Spec: map[string]any{"__type": "compute", "__component": "x", "size": "small"},
		}, cloud.ResolvedRefs{"subnetId": "subnet-1", "vpcId": "vpc-1"})
		for _, p := range prims {
			if p.TofuType == "aws_instance" {
				ami, _ := p.Attributes["ami"].(string)
				if ami == "" {
					t.Errorf("%s: empty AMI", region)
				}
			}
		}
	}
}

func TestEmitCompute_MissingSubnetIDRefErrors(t *testing.T) {
	a := aws.New()
	target := ir.DeploymentTarget{
		Cloud: "aws", Region: "us-east-1",
		Spec: map[string]any{"__component": "web-app", "__type": "compute",
			"size": "small", "instanceCount": 1},
	}
	_, err := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
	if err == nil {
		t.Fatal("expected Emit to fail when subnetId/vpcId refs are missing")
	}
	if !strings.Contains(err.Error(), "subnet") && !strings.Contains(err.Error(), "vpc") && !strings.Contains(err.Error(), "ref") {
		t.Errorf("error should mention missing ref; got: %v", err)
	}
}
